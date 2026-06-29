package opcode42client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/coder/websocket"
)

// PTY (pseudo-terminal) over WebSocket. opencode's transport (plan 06 / master
// plan "PTY WS framing"):
//   - create with POST /pty, mint a short-lived ticket with
//     POST /pty/:id/connect-token, then dial GET /pty/:id/connect?ticket=… (the
//     auth middleware skips Basic auth for a ticketed connect; directory routing
//     still rides the x-opencode-directory header).
//   - server→client: data frames are raw terminal bytes; a control frame is
//     0x00 + UTF-8 JSON {cursor} carrying the byte offset for resume.
//   - client→server: raw input bytes (no framing). Resize via PUT /pty/:id.
//
// This is hand-written because codegen can't model the persistent socket.

// PTYInfo is a pseudo-terminal (POST /pty / GET /pty response).
type PTYInfo struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Cwd     string   `json:"cwd"`
	Status  string   `json:"status"`
	PID     int      `json:"pid"`
}

// PTYCreate are the options for POST /pty (all optional; the daemon defaults the
// shell + cwd).
type PTYCreate struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Title   string            `json:"title,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// CreatePTY starts a pseudo-terminal.
func (c *Opcode42Client) CreatePTY(ctx context.Context, opts PTYCreate) (PTYInfo, error) {
	var info PTYInfo
	err := c.PostJSON(ctx, "/pty", opts, &info)
	return info, err
}

// ResizePTY sets the terminal size (PUT /pty/:id with {size:{cols,rows}}).
func (c *Opcode42Client) ResizePTY(ctx context.Context, id string, cols, rows int) error {
	body := map[string]any{"size": map[string]int{"cols": cols, "rows": rows}}
	return c.putJSON(ctx, "/pty/"+id, body)
}

// connectToken mints a short-lived WebSocket connect ticket.
func (c *Opcode42Client) connectToken(ctx context.Context, id string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/pty/"+id+"/connect-token", nil)
	if err != nil {
		return "", err
	}
	_ = c.injectHeaders(ctx, req)
	req.Header.Set("x-opencode-ticket", "1") // PTY_CONNECT_TOKEN_HEADER
	resp, err := c.rest.Do(req)
	if err != nil {
		return "", fmt.Errorf("pty connect-token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("pty connect-token: status %d", resp.StatusCode)
	}
	var tok struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.Ticket == "" {
		return "", fmt.Errorf("pty connect-token: empty ticket")
	}
	return tok.Ticket, nil
}

// PTYConn is a live PTY WebSocket. Read terminal output from Output(); the
// stream's terminal error arrives on Err(). Write sends input. Always Close.
type PTYConn struct {
	ws     *websocket.Conn
	cancel context.CancelFunc
	output chan []byte
	cursor chan int
	errc   chan error
}

// Output is the channel of raw terminal output chunks (data frames).
func (p *PTYConn) Output() <-chan []byte { return p.output }

// Cursor delivers the server's byte-offset control frames (for resume).
func (p *PTYConn) Cursor() <-chan int { return p.cursor }

// Err delivers the terminal read error (one value).
func (p *PTYConn) Err() <-chan error { return p.errc }

// ConnectPTY dials the PTY WebSocket. cursor resumes from a byte offset (0 from
// the start, -1 for live-only / no replay). The returned PTYConn owns a read
// goroutine that lives until Close — cancelling ctx aborts only the dial, not the
// stream, so callers MUST Close (and keep draining Output until then).
func (c *Opcode42Client) ConnectPTY(ctx context.Context, id string, cursor int) (*PTYConn, error) {
	ticket, err := c.connectToken(ctx, id)
	if err != nil {
		return nil, err
	}

	u := c.baseURL + "/pty/" + id + "/connect"
	q := url.Values{"ticket": {ticket}}
	if cursor != 0 {
		q.Set("cursor", strconv.Itoa(cursor))
	}
	u += "?" + q.Encode()
	// http(s) → ws(s)
	if len(u) > 5 && u[:5] == "https" {
		u = "wss" + u[5:]
	} else if len(u) > 4 && u[:4] == "http" {
		u = "ws" + u[4:]
	}

	h := http.Header{}
	if c.directory != "" {
		h.Set("X-Opencode-Directory", url.PathEscape(c.directory))
	}
	if c.auth != "" {
		h.Set("Authorization", c.auth) // harmless; the ticket is what authorizes
	}

	connCtx, cancel := context.WithCancel(context.Background())
	ws, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: h, HTTPClient: c.sse})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close() // the handshake response body is not used
	}
	if err != nil {
		cancel()
		return nil, fmt.Errorf("pty connect: %w", err)
	}
	ws.SetReadLimit(4 << 20) // 4 MiB; PTY replay buffers can be large

	p := &PTYConn{
		ws:     ws,
		cancel: cancel,
		output: make(chan []byte),
		cursor: make(chan int, 1),
		errc:   make(chan error, 1),
	}
	go p.read(connCtx)
	return p, nil
}

// read pumps frames. opencode sends terminal output as TEXT frames and the
// {cursor} control frame as a BINARY frame prefixed with 0x00 (its web client
// discriminates on the message type, not just the first byte). Match that so a
// text chunk that happens to start with NUL isn't misread as control.
func (p *PTYConn) read(ctx context.Context) {
	defer close(p.output)
	for {
		typ, data, err := p.ws.Read(ctx)
		if err != nil {
			p.errc <- err
			return
		}
		if typ == websocket.MessageBinary && len(data) > 0 && data[0] == 0x00 { // control: 0x00 + JSON {cursor}
			var meta struct {
				Cursor int `json:"cursor"`
			}
			if json.Unmarshal(data[1:], &meta) == nil {
				select {
				case p.cursor <- meta.Cursor:
				default: // keep only the latest cursor
				}
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case p.output <- data:
		}
	}
}

// Write sends input bytes to the terminal.
func (p *PTYConn) Write(ctx context.Context, b []byte) error {
	return p.ws.Write(ctx, websocket.MessageBinary, b)
}

// Close terminates the connection (idempotent, nil-safe).
func (p *PTYConn) Close() {
	if p == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	if p.ws != nil {
		_ = p.ws.Close(websocket.StatusNormalClosure, "")
	}
}

// putJSON performs an authed PUT of a JSON body (used by ResizePTY).
func (c *Opcode42Client) putJSON(ctx context.Context, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	_ = c.injectHeaders(ctx, req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body) // drain for keep-alive reuse
	if resp.StatusCode >= 300 {
		return fmt.Errorf("PUT %s: status %d", path, resp.StatusCode)
	}
	return nil
}
