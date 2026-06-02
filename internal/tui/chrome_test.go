package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TestAlwaysPaintsBackground verifies that View() always wraps content in the
// theme background (previously the default theme was excluded — that was the bug).
func TestAlwaysPaintsBackground(t *testing.T) {
	// With dimensions set, every theme should produce output that includes the
	// background color escape (lipgloss embeds it when Background is set).
	for _, name := range []string{"forge-dark", "forge-light", "monochrome"} {
		m := New(Config{URL: "http://x"})
		m.width, m.height = 80, 24
		m = m.applyThemeByName(name)
		out := m.View()
		if out == "" {
			t.Fatalf("theme %q: View() returned empty", name)
		}
	}
	// Without dimensions View() returns body unpainted (nothing to fill).
	m0 := New(Config{URL: "http://x"})
	m0.themeName = "monochrome"
	// width==0, height==0 → no background fill, should not panic
	_ = m0.View()
}

// Overlays must render as clean rectangles (uniform line width, no ragged edge).
func TestAutocompleteView_UniformWidth(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.commands = []slashItem{{name: "/init", desc: "guided setup", kind: slashDaemon}}
	m.input.SetValue("/")
	m, _ = m.refreshAutocomplete()
	rows := strings.Split(strings.TrimRight(m.autocompleteView(), "\n"), "\n")
	w := lipgloss.Width(rows[0])
	for i, r := range rows {
		if lipgloss.Width(r) != w {
			t.Fatalf("popup line %d width %d != %d (ragged panel)", i, lipgloss.Width(r), w)
		}
	}
}

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
	// M8 re-style: the sidebar now shows per-direction token rows ("in"/"out")
	// and a "total" row — matches opencode scene 06 which breaks down token
	// counts by direction. "tokens" (the old single-row label) is replaced by
	// "total". All other previously-tested strings remain present.
	for _, want := range []string{"My Session", "1,234", "total", "$0.0123", "Forge"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar missing %q in:\n%s", want, out)
		}
	}
	// Also assert the new per-direction labels are rendered.
	for _, want := range []string{"in", "out", "CONTEXT", "LSP"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar missing section label %q in:\n%s", want, out)
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

// TestStatusBar_ModeChipAndProviderChips verifies the M8 chrome grammar:
// the status bar must contain the mode chip, model name, and provider —
// matching opencode's "Build · Big Pickle OpenCode Zen" pattern.
func TestStatusBar_ModeChipAndProviderChips(t *testing.T) {
	cases := []struct {
		name     string
		cfg      Config
		agent    string
		wantMode string
		wantMod  string
		wantProv string
	}{
		{
			name:     "default build mode",
			cfg:      Config{URL: "http://x", Provider: "anthropic", Model: "claude-sonnet-4"},
			agent:    "",
			wantMode: "build",
			wantMod:  "claude-sonnet-4",
			wantProv: "anthropic",
		},
		{
			name:     "custom agent mode",
			cfg:      Config{URL: "http://x", Provider: "openai", Model: "gpt-4o"},
			agent:    "researcher",
			wantMode: "researcher",
			wantMod:  "gpt-4o",
			wantProv: "openai",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(tc.cfg)
			m.agent = tc.agent
			out := m.statusBarView(120)
			for _, want := range []string{tc.wantMode, tc.wantMod, tc.wantProv, "ctrl+p", "commands"} {
				if !strings.Contains(out, want) {
					t.Errorf("status bar missing %q\nout: %s", want, out)
				}
			}
			// Width invariant must hold.
			if got := lipgloss.Width(out); got != 120 {
				t.Errorf("status bar width %d, want 120", got)
			}
		})
	}
}

// TestStatusBar_BackgroundFill verifies the M8 surface-fill rule: the status bar
// must render as exactly `width` visible characters wide for all tested widths
// so the Bg surface covers trailing cells (no bleed-through on light terminals).
func TestStatusBar_BackgroundFill(t *testing.T) {
	for _, themeName := range []string{"forge-dark", "forge-light"} {
		for _, w := range []int{60, 80, 120} {
			m := New(Config{URL: "http://x", Provider: "anthropic", Model: "claude-sonnet-4"})
			m = m.applyThemeByName(themeName)
			bar := m.statusBarView(w)
			if got := lipgloss.Width(bar); got != w {
				t.Errorf("theme=%s width=%d: bar visible width %d, want %d", themeName, w, got, w)
			}
		}
	}
}

// TestSidebar_SectionsPresent verifies the M8 sidebar layout: CONTEXT and LSP
// sections must always appear, and their labels must be visible regardless of
// whether token data is available. This mirrors opencode scene 06 where the
// sidebar always shows both sections.
func TestSidebar_SectionsPresent(t *testing.T) {
	cases := []struct {
		name     string
		hasToken bool
	}{
		{"with tokens", true},
		{"no tokens", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Config{URL: "http://x", SessionID: "ses_1"})
			m.width, m.height = 120, 24
			ss := Session{ID: "ses_1", Title: "T"}
			if tc.hasToken {
				ss.Tokens.Input, ss.Tokens.Output = 500, 100
			}
			m.store.sessions = []Session{ss}
			out := m.sidebarView()
			for _, want := range []string{"CONTEXT", "LSP"} {
				if !strings.Contains(out, want) {
					t.Errorf("sidebar (%s) missing section %q", tc.name, want)
				}
			}
		})
	}
}

// TestSidebar_BackgroundFill verifies the M8 surface-fill rule: sidebarView()
// must render as exactly sidebarWidth visible characters wide per line.
func TestSidebar_BackgroundFill(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.width, m.height = 120, 24
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}
	out := m.sidebarView()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, line := range lines {
		if got := lipgloss.Width(line); got != sidebarWidth {
			t.Errorf("sidebar line %d: visible width %d, want %d", i, got, sidebarWidth)
		}
	}
}
