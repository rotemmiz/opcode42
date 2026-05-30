package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestHumanInt(t *testing.T) {
	cases := map[int]string{0: "0", 42: "42", 999: "999", 1000: "1,000", 1234: "1,234", 1234567: "1,234,567"}
	for in, want := range cases {
		if got := humanInt(in); got != want {
			t.Errorf("humanInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("short string should pass through, got %q", got)
	}
	if got := truncate("hello world", 5); got != "hell…" {
		t.Errorf("truncate to 5 = %q, want %q", got, "hell…")
	}
}

func TestStatusBar_WidthAndContent(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "google", Model: "gemini-2.5-flash"})
	for _, w := range []int{60, 80, 120} {
		if got := lipgloss.Width(m.statusBarView(w)); got != w {
			t.Fatalf("status bar width %d, want %d", got, w)
		}
	}
	out := m.statusBarView(120)
	for _, want := range []string{"build", "gemini-2.5-flash", "commands"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status bar missing %q", want)
		}
	}
}

func TestSidebar_ShowsTokensAndCost(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.width, m.height = 120, 24
	ss := Session{ID: "ses_1", Title: "My Session"}
	ss.Tokens.Input, ss.Tokens.Output = 1200, 34
	ss.Cost = 0.0123
	m.store.sessions = []Session{ss}

	out := m.sidebarView()
	for _, want := range []string{"My Session", "1,234", "tokens", "$0.0123", "Forge"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar missing %q in:\n%s", want, out)
		}
	}
}

func TestSidebarVisible_Thresholds(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	m.width, m.height = 120, 24
	if !m.sidebarVisible() {
		t.Fatal("a wide session screen should show the sidebar")
	}
	m.width = 70
	if m.sidebarVisible() {
		t.Fatal("a narrow terminal should hide the sidebar")
	}
	m.width, m.sidebarHidden = 120, true
	if m.sidebarVisible() {
		t.Fatal("the hidden flag should hide the sidebar")
	}
	m.sidebarHidden, m.screen = false, ScreenSplash
	if m.sidebarVisible() {
		t.Fatal("the splash screen should not show the sidebar")
	}
}

// The leftColumnWidth constant must equal the sidebar's real rendered width, or
// the Update path (composer sizing) and the render path disagree and overlap.
func TestSidebarView_WidthMatchesConstant(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen, m.width, m.height = ScreenSession, 120, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}
	if got := lipgloss.Width(m.sidebarView()); got != sidebarWidth {
		t.Fatalf("sidebarView width %d != sidebarWidth %d", got, sidebarWidth)
	}
}

// Regression for the BLOCKING bug: when the sidebar shows, the composer must be
// sized to the LEFT column (not the full width), driven through Update so the
// textarea's own SetWidth is exercised — else the composer overlaps the sidebar.
func TestComposer_SizedToLeftColumnUnderSidebar(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 24}) // sidebar shows

	leftW := 120 - sidebarWidth
	if w := lipgloss.Width(m.composerView()); w > leftW {
		t.Fatalf("composer renders %d cols, exceeds left column %d (overlaps sidebar)", w, leftW)
	}
	if w := lipgloss.Width(m.renderSession()); w != 120 {
		t.Fatalf("session layout renders %d cols, want exactly 120", w)
	}
}

func TestRenderSession_SidebarLayoutFitsWidth(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.width, m.height = 120, 24
	m.screen = ScreenSession
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}

	out := m.renderSession()
	if w := lipgloss.Width(out); w > 120 {
		t.Fatalf("session layout renders %d cols, exceeds width 120", w)
	}
	if !strings.Contains(out, "Forge") {
		t.Fatal("the sidebar (Forge tag) should be present at this width")
	}

	// Hidden sidebar → no Forge tag, full-width stream.
	m.sidebarHidden = true
	if strings.Contains(m.renderSession(), "Forge") {
		t.Fatal("hidden sidebar should not render")
	}
}
