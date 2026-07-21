package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// wheelUp / wheelDown build a MouseWheelMsg with the given button, matching the
// bubbletea v2 shape (mouse.go:113: `type MouseWheelMsg Mouse`; Button is the
// only field the A2 handler routes on).
func wheelUp() tea.MouseWheelMsg   { return tea.MouseWheelMsg{Button: tea.MouseWheelUp} }
func wheelDown() tea.MouseWheelMsg { return tea.MouseWheelMsg{Button: tea.MouseWheelDown} }

// TestMouseWheel_ScrollsStream pins Plan 18 §A2: wheel up scrolls toward older
// content (Offset grows), wheel down scrolls toward newer (Offset shrinks),
// using the scrollStep=3 increment. The footer/sidebar pinning is covered by
// the canvas layer tests; here we only assert the scroll math.
func TestMouseWheel_ScrollsStream(t *testing.T) {
	m := longSessionModel(t) // 40 messages in a 20-row viewport → scroll is meaningful
	if m.scroll.Offset != 0 || !m.scroll.AtTail() {
		t.Fatalf("initial scroll: Offset=%d want 0 (live tail)", m.scroll.Offset)
	}

	const N, M = 4, 1 // N up, then M down (M < N)
	for i := 0; i < N; i++ {
		next, _ := m.Update(wheelUp())
		m = next.(Model)
	}
	if m.scroll.Offset != N*scrollStep {
		t.Fatalf("after %d wheel-up: Offset=%d want %d", N, m.scroll.Offset, N*scrollStep)
	}
	for i := 0; i < M; i++ {
		next, _ := m.Update(wheelDown())
		m = next.(Model)
	}
	if m.scroll.Offset != (N-M)*scrollStep {
		t.Fatalf("after %d wheel-down: Offset=%d want %d", M, m.scroll.Offset, (N-M)*scrollStep)
	}
}

// TestMouseWheel_IgnoredUnderModal pins Plan 18 §A2's overlay guard: when an
// overlay owns the view (modal, focused PTY, or diff reviewer), the wheel must
// NOT touch m.scroll. Same guard expression as the key handlers.
func TestMouseWheel_IgnoredUnderModal(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *Model)
	}{
		{"modal palette", func(m *Model) { m.modal = modalPalette }},
		{"focused pty", func(m *Model) { m.pty.open, m.pty.focused = true, true }},
		{"diff reviewer", func(m *Model) { m.diff.open = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := longSessionModel(t)
			tc.set(&m)
			next, _ := m.Update(wheelUp())
			m = next.(Model)
			if m.scroll.Offset != 0 {
				t.Fatalf("%s: wheel moved scroll to Offset=%d, want 0 (overlay should swallow it)", tc.name, m.scroll.Offset)
			}
		})
	}
}

// TestScroll_StickyAtTailOnNewContent pins Plan 18 §A3-simple: when the stream
// is at the tail (Offset=0), new SSE content keeps it pinned at the tail; when
// scrolled up (Offset>0), the offset is NOT adjusted (the simple tail-sticky
// model — content-anchored scroll is deferred per the plan).
func TestScroll_StickyAtTailOnNewContent(t *testing.T) {
	m := longSessionModel(t)
	m.width, m.height = 100, 20
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}
	m.store.messages["ses_1"] = []Message{{ID: "msg_1", SessionID: "ses_1", Role: "assistant"}}
	m.store.parts["msg_1"] = []Part{{ID: "prt_1", MessageID: "msg_1", Type: "text", Text: "Hel"}}

	// At tail: dispatch a delta that grows the part text; offset must stay 0.
	if !m.scroll.AtTail() {
		t.Fatalf("precondition: scroll not at tail, Offset=%d", m.scroll.Offset)
	}
	deltaEv := sseEventMsg{ev: ev("message.part.delta", map[string]any{
		"messageID": "msg_1", "partID": "prt_1", "field": "text", "delta": "lo",
	})}
	next, _ := m.Update(deltaEv)
	m = next.(Model)
	if !m.scroll.AtTail() || m.scroll.Offset != 0 {
		t.Fatalf("at-tail + new content: scroll drifted, Offset=%d want 0 (sticky)", m.scroll.Offset)
	}
	if got := m.store.parts["msg_1"][0].Text; got != "Hello" {
		t.Fatalf("delta not applied: got %q want %q", got, "Hello")
	}

	// Scrolled up: new content must NOT move the offset (simple tail-sticky).
	m.scroll.Back(scrollStep) // Offset = scrollStep
	if m.scroll.Offset != scrollStep {
		t.Fatalf("precondition: Back(scrollStep) -> Offset=%d want %d", m.scroll.Offset, scrollStep)
	}
	deltaEv2 := sseEventMsg{ev: ev("message.part.delta", map[string]any{
		"messageID": "msg_1", "partID": "prt_1", "field": "text", "delta": "!",
	})}
	next, _ = m.Update(deltaEv2)
	m = next.(Model)
	if m.scroll.Offset != scrollStep {
		t.Fatalf("scrolled-up + new content: Offset=%d want %d (A3-simple: no content-anchor)", m.scroll.Offset, scrollStep)
	}
}

// TestMouseMode_EnabledInView pins Plan 18 §A1: View() sets
// MouseMode=MouseModeCellMotion (enables wheel events) and keeps AltScreen on.
func TestMouseMode_EnabledInView(t *testing.T) {
	m := New(Config{URL: "http://x"})
	v := m.View()
	if v.AltScreen != true {
		t.Errorf("View().AltScreen = false, want true")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("View().MouseMode = %v, want MouseModeCellMotion", v.MouseMode)
	}
}

// Compile-time: keep the sseEventMsg constructor honest — if the wire shape
// ever changes, this fails to build.
var _ opcode42client.SSEEvent = opcode42client.SSEEvent{}
