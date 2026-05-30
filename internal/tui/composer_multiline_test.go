package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// The composer must be a real multi-line editor: ctrl+j (and shift+enter where
// the terminal supports it) inserts a newline, plain enter submits, and the box
// auto-grows with content then collapses on submit. Single-line was the bug.
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
