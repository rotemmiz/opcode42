package tui

// spinner_test.go — Plan 08c M9: tests for the gradient-scanner + animTick infra.
//
// Test coverage:
//  1. scannerFrame: pure + deterministic; ANSI-stripped text == label; frames differ.
//  2. Color lerp: endpoints correct (t=0 → start, t=1 → end), midpoint between.
//  3. animating() predicate: true when running tool, false at idle.
//  4. animTickMsg Update branch: idle → no tick cmd; animating → tick cmd returned.

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// ── scannerFrame tests ─────────────────────────────────────────────────────────

// TestScannerFrameTextStable asserts that stripping ANSI from a scannerFrame
// result always equals the original label — only colors animate, not text.
func TestScannerFrameTextStable(t *testing.T) {
	p := theme.Default()
	label := "thinking…"
	for _, frame := range []int{0, 3, 7, 10, 20} {
		got := scannerFrame(label, frame, p)
		stripped := stripANSI(got)
		if stripped != label {
			t.Errorf("frame %d: stripped text %q != label %q", frame, stripped, label)
		}
	}
}

// TestScannerFramesDifferViaColor asserts that different frame indices produce
// different per-character colors (i.e. the animation actually advances).  We
// test the trailColor function directly since lipgloss strips ANSI in test
// environments (no TTY / NO_COLOR), making the rendered string identical regardless.
func TestScannerFramesDifferViaColor(t *testing.T) {
	p := theme.Default()
	n := len([]rune("thinking…")) // 9

	// For each frame the head position is frame%n, so trailColor(0-i, p) differs.
	// headPos=0: char 0 → dist 0 (Accent); char 1 → dist -1 (FgDim)
	// headPos=3: char 3 → dist 0 (Accent); char 0 → dist -3 (FgDim)
	// These two produce different colors for char 0.
	_ = n
	colF0C0 := trailColor(0, p) // frame 0, char 0 → dist 0 (head = Accent)
	colF3C0 := trailColor(3, p) // frame 3, char 0 → dist 3 (trail pos 3)
	colF3C3 := trailColor(0, p) // frame 3, char 3 → dist 0 (head = Accent)
	colF7C7 := trailColor(0, p) // frame 7, char 7 → dist 0 (head = Accent)
	colF7C0 := trailColor(7, p) // frame 7, char 0 → dist 7 (inactive = FgDim)

	if colF0C0 != p.Accent() {
		t.Errorf("frame 0 char 0 should be Accent (head), got %s", colF0C0)
	}
	if colF3C3 != p.Accent() {
		t.Errorf("frame 3 char 3 should be Accent (head), got %s", colF3C3)
	}
	if colF7C7 != p.Accent() {
		t.Errorf("frame 7 char 7 should be Accent (head), got %s", colF7C7)
	}
	if colF3C0 == colF0C0 {
		t.Errorf("frame 0 char 0 (Accent) should differ from frame 3 char 0 (trail pos 3): both %s", colF0C0)
	}
	if colF7C0 != p.FgDim {
		t.Errorf("frame 7 char 0 should be FgDim (inactive at dist 7 >= trailLen), got %s", colF7C0)
	}
}

// TestScannerFrameDeterministic asserts that scannerFrame is deterministic:
// calling it twice with the same args produces the same result.
func TestScannerFrameDeterministic(t *testing.T) {
	p := theme.Default()
	label := "Read src/x.ts"
	for _, frame := range []int{0, 5, 13} {
		a := scannerFrame(label, frame, p)
		b := scannerFrame(label, frame, p)
		if a != b {
			t.Errorf("frame %d not deterministic: got different results on repeat calls", frame)
		}
	}
}

// TestScannerFrameEmpty asserts that an empty label produces an empty string.
func TestScannerFrameEmpty(t *testing.T) {
	p := theme.Default()
	got := scannerFrame("", 5, p)
	if got != "" {
		t.Errorf("empty label should produce empty string, got %q", got)
	}
}

// ── lerpHex tests ─────────────────────────────────────────────────────────────

// TestLerpHexEndpoints asserts t=0 → color a, t=1 → color b.
func TestLerpHexEndpoints(t *testing.T) {
	a, b := "#000000", "#ffffff"

	got0 := string(lerpHex(a, b, 0))
	if got0 != "#000000" {
		t.Errorf("lerpHex t=0: want #000000, got %s", got0)
	}

	got1 := string(lerpHex(a, b, 1))
	if got1 != "#ffffff" {
		t.Errorf("lerpHex t=1: want #ffffff, got %s", got1)
	}
}

// TestLerpHexMidpoint asserts t=0.5 between black and white → #808080.
func TestLerpHexMidpoint(t *testing.T) {
	got := string(lerpHex("#000000", "#ffffff", 0.5))
	if got != "#808080" {
		t.Errorf("lerpHex t=0.5 black→white: want #808080, got %s", got)
	}
}

// TestLerpHexClamped asserts that t outside [0,1] is clamped.
func TestLerpHexClamped(t *testing.T) {
	// t < 0 → same as t=0
	got := string(lerpHex("#ff0000", "#0000ff", -1))
	want := string(lerpHex("#ff0000", "#0000ff", 0))
	if got != want {
		t.Errorf("lerpHex t=-1 should clamp to t=0: got %s, want %s", got, want)
	}
	// t > 1 → same as t=1
	got2 := string(lerpHex("#ff0000", "#0000ff", 2))
	want2 := string(lerpHex("#ff0000", "#0000ff", 1))
	if got2 != want2 {
		t.Errorf("lerpHex t=2 should clamp to t=1: got %s, want %s", got2, want2)
	}
}

// ── animating() predicate tests ────────────────────────────────────────────────

// makeToolPartState builds a JSON-encoded toolState for a given status.
func makeToolPartState(status string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"status": status})
	return raw
}

// newTestModel builds a minimal Model with the given session ID for animating()
// predicate tests.  It does not need a full New() since these tests only call
// animating(), not Update().  The store maps must be initialized.
// screen is set to ScreenSession so the splash-screen fast-path does not fire
// and the tests exercise the tool-status branch only.
func newTestModel(sessionID string) Model {
	return Model{
		cfg:    Config{SessionID: sessionID},
		store:  newStore(),
		screen: ScreenSession,
	}
}

// TestAnimatingFalseAtIdle asserts animating() is false with no parts.
func TestAnimatingFalseAtIdle(t *testing.T) {
	m := newTestModel("s1")
	if m.animating() {
		t.Error("animating() should be false with no messages")
	}
}

// TestAnimatingFalseNoSession asserts animating() is false with no session ID.
func TestAnimatingFalseNoSession(t *testing.T) {
	m := newTestModel("")
	if m.animating() {
		t.Error("animating() should be false with no session ID")
	}
}

// TestAnimatingTrueRunningTool asserts animating() is true when a tool part
// has status "running".
func TestAnimatingTrueRunningTool(t *testing.T) {
	m := newTestModel("s1")
	m.store.messages["s1"] = []Message{
		{ID: "msg1", SessionID: "s1", Role: "assistant"},
	}
	m.store.parts["msg1"] = []Part{
		{ID: "p1", MessageID: "msg1", SessionID: "s1", Type: "tool",
			State: makeToolPartState("running")},
	}
	if !m.animating() {
		t.Error("animating() should be true when a tool part is running")
	}
}

// TestAnimatingTruePendingTool asserts animating() is true for pending status.
func TestAnimatingTruePendingTool(t *testing.T) {
	m := newTestModel("s1")
	m.store.messages["s1"] = []Message{
		{ID: "msg1", SessionID: "s1", Role: "assistant"},
	}
	m.store.parts["msg1"] = []Part{
		{ID: "p1", MessageID: "msg1", SessionID: "s1", Type: "tool",
			State: makeToolPartState("pending")},
	}
	if !m.animating() {
		t.Error("animating() should be true when a tool part is pending")
	}
}

// TestAnimatingFalseCompletedTools asserts animating() is false when all tools
// are completed.
func TestAnimatingFalseCompletedTools(t *testing.T) {
	m := newTestModel("s1")
	m.store.messages["s1"] = []Message{
		{ID: "msg1", SessionID: "s1", Role: "assistant"},
	}
	m.store.parts["msg1"] = []Part{
		{ID: "p1", MessageID: "msg1", SessionID: "s1", Type: "tool",
			State: makeToolPartState("completed")},
		{ID: "p2", MessageID: "msg1", SessionID: "s1", Type: "tool",
			State: makeToolPartState("error")},
	}
	if m.animating() {
		t.Error("animating() should be false when all tools are completed/error")
	}
}

// ── animTickMsg Update branch tests ───────────────────────────────────────────

// isTick returns true if cmd is a time-based tick command (non-nil).
// We detect it by running the cmd in a test harness and checking the result type.
func isTick(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	if cmd == nil {
		return false
	}
	// tea.Tick returns a Cmd that produces a tea.Msg when called.  We cannot
	// easily introspect its type without running it, so we treat non-nil cmd
	// returned from animating == true cases as "the tick was scheduled."
	// The semantics contract is: idle → nil cmd, animating → non-nil cmd.
	return cmd != nil
}

// TestAnimTickIdleNoReschedule asserts that an animTickMsg at idle (ScreenSession,
// no running tools) returns a nil cmd (no reschedule).  We use New() to properly
// initialize the textarea and other Model fields so resizeComposer does not panic,
// then move to ScreenSession so the logo-shimmer fast-path does not fire.
func TestAnimTickIdleNoReschedule(t *testing.T) {
	m := New(Config{URL: "http://127.0.0.1:4096", SessionID: "s1"})
	// Switch to session screen: the splash logo-shimmer path keeps animating() true
	// even with no tools, so we must be on a session screen to test the idle case.
	m.screen = ScreenSession
	// No running tools → animating() == false → tick should not reschedule.
	result, cmd := m.Update(animTickMsg{})
	_ = result
	if cmd != nil {
		t.Error("animTickMsg at idle (ScreenSession, no tools) should return nil cmd (no reschedule)")
	}
}

// TestAnimTickAnimatingReschedules asserts that an animTickMsg while animating
// returns a non-nil tick cmd and increments animFrame.
func TestAnimTickAnimatingReschedules(t *testing.T) {
	m := New(Config{URL: "http://127.0.0.1:4096", SessionID: "s1"})
	m.screen = ScreenSession // session screen; animating() is driven by tool status
	m.store.messages["s1"] = []Message{
		{ID: "msg1", SessionID: "s1", Role: "assistant"},
	}
	m.store.parts["msg1"] = []Part{
		{ID: "p1", MessageID: "msg1", SessionID: "s1", Type: "tool",
			State: makeToolPartState("running")},
	}
	initialFrame := m.animFrame
	result, cmd := m.Update(animTickMsg{})
	nm := result.(Model)
	if nm.animFrame != initialFrame+1 {
		t.Errorf("animFrame should increment: got %d, want %d", nm.animFrame, initialFrame+1)
	}
	if !isTick(t, cmd) {
		t.Error("animTickMsg while animating should return a non-nil tick cmd (reschedule)")
	}
}
