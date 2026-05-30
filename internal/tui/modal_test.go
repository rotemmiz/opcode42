package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+j":
		return tea.KeyMsg{Type: tea.KeyCtrlJ}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestPalette_OpensNavigatesSwitches(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, key("ctrl+p"))
	if m.modal != modalPalette {
		t.Fatalf("ctrl+p should open palette, got %v", m.modal)
	}
	m, _ = step(t, m, key("down")) // -> "Switch session"
	if m.modalSel != 1 {
		t.Fatalf("down should move selection to 1, got %d", m.modalSel)
	}
	m, cmd := step(t, m, key("enter")) // select "Switch session"
	if m.modal != modalSessions || cmd == nil {
		t.Fatalf("Switch session should open the sessions modal + load, modal=%v cmd=%v", m.modal, cmd != nil)
	}
}

func TestPalette_NewSessionDispatches(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, key("ctrl+p"))
	m, cmd := step(t, m, key("enter")) // sel 0 = New session
	if m.modal != modalNone || cmd == nil {
		t.Fatalf("New session should close modal + dispatch, modal=%v cmd=%v", m.modal, cmd != nil)
	}
}

func TestModal_EscCloses(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, key("ctrl+p"))
	m, _ = step(t, m, key("esc"))
	if m.modal != modalNone {
		t.Fatal("esc should close the modal")
	}
}

func TestSessionsModal_SelectOpensNewestFirst(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.sessions = []Session{{ID: "ses_a", Title: "A"}, {ID: "ses_b", Title: "B"}} // ascending
	m.modal, m.modalSel = modalSessions, 0
	m, cmd := step(t, m, key("enter"))
	if m.cfg.SessionID != "ses_b" || m.screen != ScreenSession || cmd == nil {
		t.Fatalf("first row should open the newest (ses_b): got %q screen=%v", m.cfg.SessionID, m.screen)
	}
}

func TestSessionDeleted_RemovesAndSwitches(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.sessions = []Session{{ID: "ses_a"}, {ID: "ses_b"}}
	m.cfg.SessionID, m.screen = "ses_b", ScreenSession
	m, cmd := step(t, m, sessionDeletedMsg{id: "ses_b"})
	if len(m.store.sessions) != 1 || m.store.sessions[0].ID != "ses_a" {
		t.Fatalf("ses_b not removed: %+v", m.store.sessions)
	}
	if m.cfg.SessionID != "ses_a" || cmd == nil {
		t.Fatalf("deleting the open session should switch to another: got %q", m.cfg.SessionID)
	}
}

func TestSessionDeleted_LastGoesToSplash(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.sessions = []Session{{ID: "ses_a"}}
	m.cfg.SessionID, m.screen = "ses_a", ScreenSession
	m, _ = step(t, m, sessionDeletedMsg{id: "ses_a"})
	if m.cfg.SessionID != "" || m.screen != ScreenSplash {
		t.Fatalf("deleting the last session should return to splash, got %q screen=%v", m.cfg.SessionID, m.screen)
	}
}
