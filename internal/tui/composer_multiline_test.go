package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The composer (blue accent bar + textarea) must never render wider than the
// terminal — the left border renders outside lipgloss Width, so an off-by-one
// there wraps and corrupts the footer.
func TestComposer_FitsTerminalWidth(t *testing.T) {
	for _, w := range []int{20, 40, 80, 100, 200} {
		m := New(Config{URL: "http://x"})
		m, _ = step(t, m, tea.WindowSizeMsg{Width: w, Height: 24})
		m, _ = step(t, m, key("h"))
		if got := lipgloss.Width(m.composerView()); got > w {
			t.Fatalf("composer renders %d cols, exceeds terminal width %d", got, w)
		}
	}
}

// The composer must be a real multi-line editor: ctrl+j, ctrl+enter and
// alt+enter (and shift+enter where the terminal supports it) insert a newline,
// plain enter submits, and the box auto-grows with content then collapses on
// submit. Single-line was the bug. ctrl+enter / alt+enter mirror opencode's
// input_newline keybind (tui/config/keybind.ts:163).
func TestComposer_CtrlJNewline_EnterSubmits(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}) // give it a width
	m, _ = step(t, m, key("a"))
	m, _ = step(t, m, key("ctrl+j"))
	m, _ = step(t, m, key("b"))

	if v := m.input.Value(); v != "a\nb" {
		t.Fatalf("ctrl+j should insert a newline (want %q), got %q", "a\nb", v)
	}
	if h := m.input.Height(); h < 2 {
		t.Fatalf("composer should auto-grow to >=2 rows for 2 lines, got %d", h)
	}

	next, cmd := step(t, m, key("enter"))
	if cmd == nil {
		t.Fatal("enter should submit the multi-line prompt")
	}
	if next.input.Value() != "" {
		t.Fatalf("submit should clear the composer, got %q", next.input.Value())
	}
	if next.input.Height() != 1 {
		t.Fatalf("composer should collapse to 1 row after submit, got %d", next.input.Height())
	}

	// ctrl+enter and alt+enter must also insert newlines (plan 17 §G3).
	for _, k := range []string{"ctrl+enter", "alt+enter"} {
		m2 := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
		m2, _ = step(t, m2, tea.WindowSizeMsg{Width: 80, Height: 24})
		m2, _ = step(t, m2, key("a"))
		m2, _ = step(t, m2, key(k))
		m2, _ = step(t, m2, key("b"))
		if v := m2.input.Value(); v != "a\nb" {
			t.Fatalf("%s should insert a newline (want %q), got %q", k, "a\nb", v)
		}
		if h := m2.input.Height(); h < 2 {
			t.Fatalf("%s: composer should auto-grow to >=2 rows, got %d", k, h)
		}
	}
}

func TestComposer_GrowsOnWrap(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 40, Height: 24}) // composer ~38 cols
	for _, r := range strings.Repeat("x", 120) {                // one long line, no newline
		m, _ = step(t, m, key(string(r)))
	}
	if h := m.input.Height(); h < 3 {
		t.Fatalf("a long wrapped line (no newline) should grow the box to >=3 rows, got %d", h)
	}
}

// visualRows is the wrap-height estimator the composer uses; pin its arithmetic.
func TestVisualRows(t *testing.T) {
	cases := []struct {
		text string
		cols int
		want int
	}{
		{"", 10, 1},
		{"abc", 10, 1},
		{"a\nb\nc", 10, 3},
		{strings.Repeat("x", 25), 10, 3}, // ceil(25/10)
		{"x\n" + strings.Repeat("y", 20), 10, 3},
		{"hello\n", 10, 2}, // trailing newline = an empty next row
	}
	for _, c := range cases {
		if got := visualRows(c.text, c.cols); got != c.want {
			t.Errorf("visualRows(%q, %d) = %d, want %d", c.text, c.cols, got, c.want)
		}
	}
}

func TestComposer_AutoGrowClampsAtMax(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 40})
	for i := 0; i < maxComposerRows+5; i++ { // far more lines than the cap
		m, _ = step(t, m, key("x"))
		m, _ = step(t, m, key("ctrl+j"))
	}
	if h := m.input.Height(); h != maxComposerRows {
		t.Fatalf("composer height should clamp at maxComposerRows=%d, got %d", maxComposerRows, h)
	}
}

func TestComposer_PlaceholderFollowsScreen(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}) // still splash
	if got := m.input.Placeholder; !strings.Contains(got, "Ask anything") {
		t.Fatalf("splash placeholder should invite a first prompt, got %q", got)
	}
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if got := m.input.Placeholder; !strings.Contains(got, "Reply") {
		t.Fatalf("session placeholder should be the reply hint, got %q", got)
	}
}

// TestComposer_BracketedPasteInsertsText verifies plan 17 §G1: a tea.PasteMsg
// (what bubbletea v2 emits for bracketed paste, \x1b[200~…\x1b[201~) must insert
// its content at the cursor and auto-grow the composer. Before §G1 the message
// was dropped by Model.Update (no case tea.PasteMsg), so cmd+v did nothing.
func TestComposer_BracketedPasteInsertsText(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, tea.PasteMsg{Content: "hello\nworld"})

	if v := m.input.Value(); v != "hello\nworld" {
		t.Fatalf("PasteMsg should insert its content, got %q", v)
	}
	if h := m.input.Height(); h < 2 {
		t.Fatalf("pasting 2 lines should grow the composer to >=2 rows, got %d", h)
	}

	// Pasting into the middle of existing text inserts at the cursor, not at
	// the end — the textarea's PasteMsg handler uses insertRunesFromUserInput.
	m2 := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m2, _ = step(t, m2, tea.WindowSizeMsg{Width: 80, Height: 24})
	m2, _ = step(t, m2, key("a"))
	m2, _ = step(t, m2, key("b"))
	m2, _ = step(t, m2, tea.PasteMsg{Content: "XY"})
	if v := m2.input.Value(); v != "abXY" {
		t.Fatalf("paste should insert at cursor (after 'ab'), got %q", v)
	}
}

// TestComposer_ReadlineShortcuts verifies plan 17 §G2: the bubbles v2 default
// KeyMap already binds ctrl+w (delete word back), ctrl+u (delete to line start),
// ctrl+k (delete to line end), ctrl+a/ctrl+e (line start/end), alt+f/alt+b
// (word forward/back). Opcode42 intentionally does NOT override the KeyMap —
// overriding would risk dropping bindings opencode has. This test pins the two
// most-used shortcuts so a future override regression is caught.
func TestComposer_ReadlineShortcuts(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "hello world" {
		m, _ = step(t, m, key(string(r)))
	}
	if v := m.input.Value(); v != "hello world" {
		t.Fatalf("setup: typing should produce %q, got %q", "hello world", v)
	}

	// ctrl+w deletes the last word ("world"), leaving "hello " (trailing space).
	m, _ = step(t, m, key("ctrl+w"))
	if v := m.input.Value(); v != "hello " {
		t.Fatalf("ctrl+w should delete the last word, want %q got %q", "hello ", v)
	}

	// ctrl+u deletes from cursor to the start of the line → empty input.
	m, _ = step(t, m, key("ctrl+u"))
	if v := m.input.Value(); v != "" {
		t.Fatalf("ctrl+u should clear to line start, got %q", v)
	}
}

// TestCtrlC_ClearsInputWhenNonEmpty verifies plan 17 §G4: ctrl+c is
// context-dependent (opencode prompt-input.tsx:806 + app.tsx:963-966) — with
// text in the composer it clears the input; with an empty composer it quits.
// Before §G4 ctrl+c unconditionally quit, diverging from opencode.
func TestCtrlC_ClearsInputWhenNonEmpty(t *testing.T) {
	// With text → clears, does not quit.
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, key("h"))
	m, _ = step(t, m, key("i"))
	next, cmd := step(t, m, key("ctrl+c"))
	if cmd != nil {
		t.Fatalf("ctrl+c with text should clear, not quit (cmd should be nil), got %v", cmd)
	}
	if v := next.input.Value(); v != "" {
		t.Fatalf("ctrl+c should clear the composer, got %q", v)
	}

	// With empty composer → quits (tea.Quit is the cmd).
	m2 := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m2, _ = step(t, m2, tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd2 := step(t, m2, key("ctrl+c"))
	if cmd2 == nil {
		t.Fatal("ctrl+c with empty composer should quit (non-nil cmd)")
	}
}
