package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestComposer_HasBgElevBackground asserts plan 17 §F1: the composer paints an
// elevated surface (BgElev, Opcode42's solid equivalent of opencode's
// semi-opaque "surface" token, theme.ts:512). lipgloss emits no ANSI escapes in
// the no-TTY test environment, so the bg token is asserted via the
// composerBackground() helper rather than by scanning the render output.
func TestComposer_HasBgElevBackground(t *testing.T) {
	for _, tn := range []string{"opcode42-dark", "opcode42-light", "monochrome"} {
		t.Run(tn, func(t *testing.T) {
			m := New(Config{URL: "http://x"})
			m = m.applyThemeByName(tn)
			m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
			if got, want := m.composerBackground(), m.styles.P.BgElev; got != want {
				t.Fatalf("theme %s: composerBackground = %q, want BgElev %q", tn, got, want)
			}
			if m.styles.P.BgElev == "" {
				t.Fatalf("theme %s: BgElev token is empty", tn)
			}
		})
	}
}

// TestStatusBar_OwnBackgroundDistinctFromComposer asserts plan 17 §F3: the
// status bar paints its own surface (BgPanel, opencode's tinted "status" lift,
// theme.ts:514-519), distinct from the composer's BgElev so the two read as
// separate chrome rows.
func TestStatusBar_OwnBackgroundDistinctFromComposer(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "anthropic", Model: "claude-sonnet-4"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if got, want := m.statusBarBackground(), m.styles.P.BgPanel; got != want {
		t.Fatalf("statusBarBackground = %q, want BgPanel %q", got, want)
	}
	if m.statusBarBackground() == m.composerBackground() {
		t.Fatalf("status bar bg (%q) should differ from composer bg (%q)",
			m.statusBarBackground(), m.composerBackground())
	}
}

// TestStatusBar_ExitMode asserts plan 17 §F2: two ctrl+c presses on an empty
// composer enter and confirm an exit guard — the first shows the "Exit" mode
// chip in red and an "press ctrl+c again to exit" hint; the second quits. Any
// other key cancels the guard. Mirrors opencode footer.ts:987-1006.
func TestStatusBar_ExitMode(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// First ctrl+c arms the guard: status bar shows the Exit chip + hint.
	armed, _ := step(t, m, key("ctrl+c"))
	if !armed.exiting {
		t.Fatal("first ctrl+c on empty composer should arm the exit guard")
	}
	bar := armed.statusBarView(80)
	if !strings.Contains(stripANSI(bar), "Exit") {
		t.Fatalf("armed status bar should show the Exit chip\nbar: %s", stripANSI(bar))
	}
	if !strings.Contains(stripANSI(bar), "press ctrl+c again to exit") {
		t.Fatalf("armed status bar should show the exit hint\nbar: %s", stripANSI(bar))
	}
	// The chip's mode color must be red (opencode footer.view.tsx:392-394).
	if got, want := armed.modeColor(armed.styles.P), armed.styles.P.Red; got != want {
		t.Fatalf("exit modeColor = %q, want Red %q", got, want)
	}

	// Second ctrl+c quits (tea.Quit is the cmd).
	_, quitCmd := step(t, armed, key("ctrl+c"))
	if quitCmd == nil {
		t.Fatal("second ctrl+c should quit (non-nil cmd)")
	}
}

// TestStatusBar_ExitMode_CancelledByKey asserts the armed exit guard is
// cancelled by any non-ctrl+c keypress (opencode footer.ts:684-692
// handleInputClear resets exit on input activity) and by the 5s exitTickMsg.
// Each sub-case uses a fresh Model because the bubbles textarea shares its
// buffer via a pointer, so typing into one Model's composer mutates another's
// input — a fresh model keeps the two cases independent.
func TestStatusBar_ExitMode_CancelledByKey(t *testing.T) {
	// Typing a character cancels the guard.
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	armed, _ := step(t, m, key("ctrl+c"))
	if !armed.exiting {
		t.Fatal("setup: first ctrl+c should arm the exit guard")
	}
	cancelled, _ := step(t, armed, key("x"))
	if cancelled.exiting {
		t.Fatal("a non-ctrl+c keypress should cancel the armed exit guard")
	}
	if strings.Contains(stripANSI(cancelled.statusBarView(80)), "Exit") {
		t.Fatal("status bar should drop the Exit chip after the guard is cancelled")
	}

	// The exitTickMsg (5s timeout) also cancels the guard.
	m2 := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m2, _ = step(t, m2, tea.WindowSizeMsg{Width: 80, Height: 24})
	armed2, _ := step(t, m2, key("ctrl+c"))
	if !armed2.exiting {
		t.Fatal("setup: first ctrl+c should arm the exit guard")
	}
	timed, _ := step(t, armed2, exitTickMsg{})
	if timed.exiting {
		t.Fatal("exitTickMsg should cancel the armed exit guard")
	}
}

// TestComposer_Placeholder asserts plan 17 §F4: the composer shows opencode's
// placeholder text — `Ask anything... "Fix a TODO in the codebase"` before a
// session is open (opencode footer.prompt.tsx:289-293), and
// `Run a command... "git status"` while in `!` shell mode
// (footer.prompt.tsx:285-286).
func TestComposer_Placeholder(t *testing.T) {
	// Splash / no session → the opencode first-prompt placeholder. Update()
	// writes composerPlaceholder() to m.input.Placeholder each frame, so the
	// textarea (the empty composer) shows it.
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	ph := m.composerPlaceholder()
	if !strings.Contains(ph, `Ask anything...`) {
		t.Fatalf("splash placeholder should be opencode's \"Ask anything...\" text, got %q", ph)
	}
	if !strings.Contains(ph, "Fix a TODO") {
		t.Fatalf("splash placeholder should quote a TODO example, got %q", ph)
	}
	if m.input.Placeholder != ph {
		t.Fatalf("Update should set m.input.Placeholder to %q, got %q", ph, m.input.Placeholder)
	}

	// Shell mode → the run-a-command placeholder.
	m2 := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m2, _ = step(t, m2, tea.WindowSizeMsg{Width: 80, Height: 24})
	m2, _ = step(t, m2, key("!")) // enter shell mode
	if !m2.shellMode {
		t.Fatal("setup: `!` on an empty composer should enter shell mode")
	}
	ph2 := m2.composerPlaceholder()
	if !strings.Contains(ph2, "Run a command") {
		t.Fatalf("shell-mode placeholder should be \"Run a command...\", got %q", ph2)
	}
	if !strings.Contains(ph2, "git status") {
		t.Fatalf("shell-mode placeholder should quote a git status example, got %q", ph2)
	}
	if m2.input.Placeholder != ph2 {
		t.Fatalf("Update should set m.input.Placeholder to %q in shell mode, got %q", ph2, m2.input.Placeholder)
	}
}
