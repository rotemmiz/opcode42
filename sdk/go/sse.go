package opcode42client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SSEEvent is one server-sent event in the opencode wire shape
// ({ id, type, properties }). Properties is left raw so callers decode it per
// type. The codegen path cannot model the persistent stream, so this and the
// stream below are hand-written (plan 06).
type SSEEvent struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
	// Directory/Project/Workspace are set only on the /global/event stream (the
	// global envelope's routing origin).
	Directory string `json:"-"`
	Project   string `json:"-"`
	Workspace string `json:"-"`
}

// EventStream is a live SSE subscription. Read from Events; Errc delivers the
// terminal error (stream close / network). Always call Close.
type EventStream struct {
	events chan SSEEvent
	errc   chan error
	cancel context.CancelFunc
	body   interface{ Close() error }
}

// Events returns the receive channel of decoded events.
func (s *EventStream) Events() <-chan SSEEvent { return s.events }

// Err returns the channel that delivers the stream's terminal error (one value).
func (s *EventStream) Err() <-chan error { return s.errc }

// Close terminates the stream and releases the connection. Safe to call on a
// zero-value stream and idempotent (cancel + body.Close are idempotent).
func (s *EventStream) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.body != nil {
		_ = s.body.Close()
	}
}

// Events subscribes to the instance event stream (GET /event) for the client's
// directory. Reconnect/backoff is the caller's responsibility (e.g. plan 08's
// TUI owns the reconnect loop).
func (c *Opcode42Client) Events(ctx context.Context) (*EventStream, error) {
	return c.stream(ctx, "/event", false)
}

// GlobalEvents subscribes to the process-global stream (GET /global/event),
// unwrapping the global envelope ({ payload, directory }) into SSEEvents.
func (c *Opcode42Client) GlobalEvents(ctx context.Context) (*EventStream, error) {
	return c.stream(ctx, "/global/event", true)
}

func (c *Opcode42Client) stream(ctx context.Context, path string, wrapped bool) (*EventStream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	_ = c.injectHeaders(streamCtx, req)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.sse.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("sse %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("sse %s: status %d", path, resp.StatusCode)
	}

	s := &EventStream{events: make(chan SSEEvent), errc: make(chan error, 1), cancel: cancel, body: resp.Body}
	go s.consume(streamCtx, resp.Body, wrapped)
	return s, nil
}

// globalEnvelope is the /global/event wrapper shape (bus.GlobalEvent).
type globalEnvelope struct {
	Payload   SSEEvent `json:"payload"`
	Directory string   `json:"directory"`
	Project   string   `json:"project"`
	Workspace string   `json:"workspace"`
}

// consume parses the SSE body, emitting one SSEEvent per "data:" block.
func (s *EventStream) consume(ctx context.Context, body interface{ Read([]byte) (int, error) }, wrapped bool) {
	defer close(s.events)
	reader := bufio.NewReader(body)
	var data strings.Builder
	flush := func() bool {
		if data.Len() == 0 {
			return true
		}
		raw := data.String()
		data.Reset()
		ev, ok := decodeEvent(raw, wrapped)
		if !ok {
			return true // tolerate keep-alives / non-JSON
		}
		select {
		case <-ctx.Done():
			return false
		case s.events <- ev:
			return true
		}
	}

	for {
		line, err := reader.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(trimmed, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			// SSE: strip exactly one optional leading space after the colon.
			data.WriteString(strings.TrimPrefix(trimmed[len("data:"):], " "))
		case trimmed == "": // blank line terminates an event
			if !flush() {
				return
			}
		}
		if err != nil {
			_ = flush()
			s.errc <- err
			return
		}
	}
}

func decodeEvent(raw string, wrapped bool) (SSEEvent, bool) {
	if wrapped {
		var env globalEnvelope
		if json.Unmarshal([]byte(raw), &env) != nil {
			return SSEEvent{}, false
		}
		ev := env.Payload
		ev.Directory, ev.Project, ev.Workspace = env.Directory, env.Project, env.Workspace
		return ev, true
	}
	var ev SSEEvent
	if json.Unmarshal([]byte(raw), &ev) != nil {
		return SSEEvent{}, false
	}
	return ev, true
}
