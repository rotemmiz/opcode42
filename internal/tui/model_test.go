package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestNew_BuildsClient(t *testing.T) {
	m := New(Config{URL: "http://127.0.0.1:4096", Directory: "/tmp"})
	if m.client == nil {
		t.Fatal("client not built")
	}
	if m.conn != Connecting {
		t.Fatalf("initial conn = %v, want Connecting", m.conn)
	}
}

func TestUpdate_ConnectionLifecycle(t *testing.T) {
	m := New(Config{URL: "http://127.0.0.1:4096"})

	// health passed
	m, cmd := step(t, m, connectedMsg{})
	if m.conn != Connected || m.status != "connected" || cmd == nil {
		t.Fatalf("after connected: conn=%v status=%q cmd=%v", m.conn, m.status, cmd != nil)
	}

	// stream open failed → reconnecting, backoff scheduled
	m, cmd = step(t, m, streamOpenedMsg{err: errors.New("boom")})
	if m.conn != Reconnecting || m.attempt != 1 || cmd == nil {
		t.Fatalf("after open err: conn=%v attempt=%d", m.conn, m.attempt)
	}

	// backoff elapsed → reopen command issued
	if _, cmd = step(t, m, reconnectMsg{}); cmd == nil {
		t.Fatal("reconnect should issue an open command")
	}

	// stream open ok → connected, listening, backoff reset
	m, cmd = step(t, m, streamOpenedMsg{stream: &opcode42client.EventStream{}})
	if m.conn != Connected || m.stream == nil || cmd == nil {
		t.Fatalf("after open ok: conn=%v stream=%v", m.conn, m.stream != nil)
	}
	if m.attempt != 0 {
		t.Fatalf("attempt should reset to 0 on successful reopen, got %d", m.attempt)
	}

	// an event increments the counter and keeps listening
	m, cmd = step(t, m, sseEventMsg{ev: opcode42client.SSEEvent{Type: "message.updated"}})
	if m.eventCount != 1 || !strings.Contains(m.status, "1 events") || cmd == nil {
		t.Fatalf("after event: count=%d status=%q", m.eventCount, m.status)
	}

	// stream closed → reconnecting again
	m, _ = step(t, m, sseClosedMsg{})
	if m.conn != Reconnecting || m.stream != nil {
		t.Fatalf("after close: conn=%v stream=%v", m.conn, m.stream != nil)
	}

	// terminal error
	m, _ = step(t, m, connErrMsg{err: errors.New("nope")})
	if m.conn != ConnError || m.err == nil {
		t.Fatalf("after conn err: conn=%v err=%v", m.conn, m.err)
	}
}

func TestUpdate_WindowSizeAndQuit(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.width != 80 || m.height != 24 {
		t.Fatalf("size = %dx%d", m.width, m.height)
	}
	// q quits (stream is nil, so no Close panic).
	_, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
}

func TestViewSplash_RendersWordmark(t *testing.T) {
	m := New(Config{URL: "http://127.0.0.1:4096"})
	if !strings.Contains(m.View(), "opcode42") {
		t.Fatalf("splash missing wordmark: %q", m.View())
	}
}
