package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/rotemmiz/forge/internal/auth"
)

func TestPtyShells(t *testing.T) {
	srv := httptest.NewServer(newBackedServer(t, auth.Config{}))
	defer srv.Close()
	rr, body := reqDo(t, srv.URL+"/pty/shells", http.MethodGet, "")
	if rr != http.StatusOK {
		t.Fatalf("status = %d", rr)
	}
	var shells []map[string]any
	if err := json.Unmarshal(body, &shells); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	for _, s := range shells {
		if _, ok := s["path"].(string); !ok {
			t.Errorf("shell missing path: %v", s)
		}
		if _, ok := s["acceptable"].(bool); !ok {
			t.Errorf("shell missing acceptable: %v", s)
		}
	}
}

func TestPtyGetUnknown404(t *testing.T) {
	srv := httptest.NewServer(newBackedServer(t, auth.Config{}))
	defer srv.Close()
	rr, body := reqDo(t, srv.URL+"/pty/pty_doesnotexist000000000000", http.MethodGet, t.TempDir())
	if rr != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr)
	}
	var e map[string]any
	_ = json.Unmarshal(body, &e)
	if e["_tag"] != "PtyNotFoundError" {
		t.Errorf("_tag = %v, want PtyNotFoundError", e["_tag"])
	}
}

func TestPtyCreateConnectReplay(t *testing.T) {
	srv := httptest.NewServer(newBackedServer(t, auth.Config{}))
	defer srv.Close()
	dir := t.TempDir()

	// A short-lived shell that emits a known marker then lingers so the session
	// stays alive while we attach.
	id := createPty(t, srv.URL, dir, map[string]any{
		"command": "/bin/sh",
		"args":    []string{"-c", "printf hello-pty; sleep 2"},
	})

	// Give the shell a moment to emit into the ring buffer.
	time.Sleep(250 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The WS request cannot set the x-opencode-directory header, so the instance
	// is selected via ?directory= (resolution order: ?directory → header → cwd).
	conn, resp, err := websocket.Dial(ctx, srv.URL+"/pty/"+id+"/connect?cursor=0&directory="+dir, nil)
	closeResp(resp)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	var text strings.Builder
	var gotControl bool
	for i := 0; i < 10 && !gotControl; i++ {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		switch typ {
		case websocket.MessageText:
			text.Write(data)
		case websocket.MessageBinary:
			if len(data) == 0 || data[0] != 0x00 {
				t.Fatalf("binary frame is not a control frame: %v", data)
			}
			var meta struct {
				Cursor int `json:"cursor"`
			}
			if err := json.Unmarshal(data[1:], &meta); err != nil {
				t.Fatalf("control payload: %v", err)
			}
			if meta.Cursor < len("hello-pty") {
				t.Errorf("control cursor = %d, want >= %d", meta.Cursor, len("hello-pty"))
			}
			gotControl = true
		}
	}
	if !gotControl {
		t.Fatal("never received the control frame")
	}
	if !strings.Contains(text.String(), "hello-pty") {
		t.Errorf("replay = %q, want to contain hello-pty", text.String())
	}
}

func TestPtyConnectTokenAndTicketBypassesAuth(t *testing.T) {
	// Auth ENABLED: the WS connect must work only with a minted ticket.
	srv := httptest.NewServer(newBackedServer(t, auth.Config{Username: "opencode", Password: "secret"}))
	defer srv.Close()
	dir := t.TempDir()
	id := createPtyAuthed(t, srv.URL, dir)

	// connect-token requires the x-opencode-ticket header.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/pty/"+id+"/connect-token", nil)
	req.SetBasicAuth("opencode", "secret")
	req.Header.Set("x-opencode-ticket", "1")
	req.Header.Set("x-opencode-directory", dir)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect-token status = %d, want 200", resp.StatusCode)
	}
	var tok struct {
		Ticket    string `json:"ticket"`
		ExpiresIn int    `json:"expires_in"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	if tok.Ticket == "" || tok.ExpiresIn != 60 {
		t.Fatalf("bad token response: %+v", tok)
	}

	// WS connect with the ticket bypasses Basic auth.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp2, err := websocket.Dial(ctx,
		srv.URL+"/pty/"+id+"/connect?cursor=-1&ticket="+tok.Ticket+"&directory="+dir, nil)
	closeResp(resp2)
	if err != nil {
		t.Fatalf("ticketed ws dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	// First frame with cursor=-1 is the control frame (no replay).
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	if typ != websocket.MessageBinary || data[0] != 0x00 {
		t.Errorf("expected control frame first, got type=%v data=%v", typ, data)
	}

	// A second use of the same (single-use) ticket must be rejected.
	conn2, resp3, err := websocket.Dial(ctx,
		srv.URL+"/pty/"+id+"/connect?cursor=-1&ticket="+tok.Ticket+"&directory="+dir, nil)
	closeResp(resp3)
	if err == nil {
		_ = conn2.CloseNow()
		t.Error("reused ticket should be rejected (403)")
	}
}

// closeResp closes the HTTP handshake response body from websocket.Dial, which
// is non-nil on a failed upgrade (satisfies bodyclose and frees the conn).
func closeResp(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

// --- helpers ---

func reqDo(t *testing.T, url, method, dir string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
	if dir != "" {
		req.Header.Set("x-opencode-directory", dir)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, e := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if e != nil {
			break
		}
	}
	return resp.StatusCode, body
}

func createPty(t *testing.T, base, dir string, payload map[string]any) string {
	t.Helper()
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, base+"/pty", strings.NewReader(string(b)))
	req.Header.Set("x-opencode-directory", dir)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create pty status = %d", resp.StatusCode)
	}
	var info struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&info)
	if info.ID == "" {
		t.Fatal("create pty returned no id")
	}
	return info.ID
}

func createPtyAuthed(t *testing.T, base, dir string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"command": "/bin/sh", "args": []string{"-c", "sleep 2"}})
	req, _ := http.NewRequest(http.MethodPost, base+"/pty", strings.NewReader(string(b)))
	req.SetBasicAuth("opencode", "secret")
	req.Header.Set("x-opencode-directory", dir)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var info struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&info)
	if info.ID == "" {
		t.Fatalf("create pty (authed) status %d returned no id", resp.StatusCode)
	}
	return info.ID
}
