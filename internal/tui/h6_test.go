package tui

import (
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestH6_WindowTitle_HomeAndSession(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if got := m.windowTitle(); got != "Opcode42" {
		t.Fatalf("splash title = %q, want Opcode42", got)
	}
	m.screen = ScreenSession
	m.cfg.SessionID = "ses_1"
	m.store.sessions = []Session{{ID: "ses_1", Title: "Fix the flaky test suite now please"}}
	got := m.windowTitle()
	if got != "OC | Fix the flaky test suite now please" {
		t.Fatalf("session title = %q", got)
	}
	m.store.sessions[0].Title = "This is a very long session title that should be truncated for the terminal"
	got = m.windowTitle()
	want := "OC | " + truncate("This is a very long session title that should be truncated for the terminal", 40)
	if got != want {
		t.Fatalf("truncated title = %q, want %q", got, want)
	}
	// Daemon auto-title must fall back to Opcode42 (opencode isDefaultTitle).
	m.store.sessions[0].Title = "New session - 2026-07-23T23:29:11.356Z"
	if got = m.windowTitle(); got != "Opcode42" {
		t.Fatalf("default auto-title = %q, want Opcode42", got)
	}
}

func TestH6_WindowTitle_Disabled(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.store.sessions = []Session{{ID: "ses_1", Title: "Hi"}}
	m.terminalTitleEnabled = false
	if got := m.windowTitle(); got != "" {
		t.Fatalf("disabled title = %q, want empty", got)
	}
	v := m.View()
	if v.WindowTitle != "" {
		t.Fatalf("View.WindowTitle = %q, want empty", v.WindowTitle)
	}
}

func TestH6_CtrlZ_Suspends(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("suspend disabled on windows")
	}
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	_, cmd := step(t, m, key("ctrl+z"))
	if cmd == nil {
		t.Fatal("ctrl+z should return tea.Suspend")
	}
	msg := cmd()
	if _, ok := msg.(tea.SuspendMsg); !ok {
		t.Fatalf("ctrl+z cmd = %T, want SuspendMsg", msg)
	}
}

func TestH6_Palette_ToggleTerminalTitle(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.terminalTitleEnabled = true
	m.modal, m.modalSel = modalPalette, 0
	for i, it := range paletteItems {
		if it.action == paTerminalTitle {
			m.modalSel = i
			break
		}
	}
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.terminalTitleEnabled {
		t.Fatal("palette toggle should disable terminal title")
	}
	if nm.modal != modalNone {
		t.Fatalf("palette should close, modal=%v", nm.modal)
	}
}

func TestH6_KV_TitleDefaultOn(t *testing.T) {
	if !kvTitleEnabled(kvData{}) {
		t.Fatal("nil TerminalTitleEnabled should default on")
	}
	off := false
	if kvTitleEnabled(kvData{TerminalTitleEnabled: &off}) {
		t.Fatal("explicit false should disable")
	}
}
