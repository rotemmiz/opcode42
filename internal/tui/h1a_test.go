package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestH1a_CtrlR_OpensRename pins plan 08f H1a: ctrl+r opens the rename overlay.
func TestH1a_CtrlR_OpensRename(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.store.sessions = []Session{{ID: "ses_1", Title: "My Session"}}
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, key("ctrl+r"))
	if m.modal != modalRename {
		t.Fatalf("ctrl+r → modal %v, want modalRename", m.modal)
	}
	if m.renameInput.Value() != "My Session" {
		t.Fatalf("rename input = %q, want My Session", m.renameInput.Value())
	}
}

// TestH1a_CtrlD_TwoPressDelete pins the two-press delete confirm.
func TestH1a_CtrlD_TwoPressDelete(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, cmd := step(t, m, key("ctrl+d"))
	if !m.deleting || !strings.Contains(m.status, "ctrl+d again") {
		t.Fatalf("first ctrl+d should arm deleting; deleting=%v status=%q", m.deleting, m.status)
	}
	if cmd == nil {
		t.Fatal("first ctrl+d should schedule the auto-cancel tick")
	}
	m, cmd = step(t, m, key("ctrl+d"))
	if m.deleting {
		t.Fatal("second ctrl+d should clear the deleting flag")
	}
	if cmd == nil {
		t.Fatal("second ctrl+d should dispatch deleteSessionCmd")
	}
}

// TestH1a_CtrlD_ForwardsWhenComposerHasText pins that ctrl+d with text does
// forward-delete in the textarea, not session delete.
func TestH1a_CtrlD_ForwardsWhenComposerHasText(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.input.SetValue("hello")
	m.input.SetCursor(0) // at start → ctrl+d deletes 'h'
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, key("ctrl+d"))
	if m.deleting {
		t.Fatal("ctrl+d with composer text must not arm session delete")
	}
}

// TestH1a_LeaderC_Compacts pins ctrl+x c → summarize (compact).
func TestH1a_LeaderC_Compacts(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.model = promptModel{Provider: "mock", Model: "mock"}
	m, _ = step(t, m, key("ctrl+x"))
	m, cmd := step(t, m, key("c"))
	if cmd == nil {
		t.Fatal("ctrl+x c should dispatch summarizeSessionCmd")
	}
	if !strings.Contains(m.status, "summarizing") {
		t.Fatalf("status = %q, want summarizing…", m.status)
	}
}

// TestH1a_LeaderK_Connect pins connect moved to ctrl+x k.
func TestH1a_LeaderK_Connect(t *testing.T) {
	m := New(Config{URL: "http://x", NoDiscover: true})
	m, _ = step(t, m, key("ctrl+x"))
	m, _ = step(t, m, key("k"))
	if m.modal != modalConnect {
		t.Fatalf("ctrl+x k → modal %v, want modalConnect", m.modal)
	}
}

// TestH1a_SlashShareExportCopy pins the new slash builtins.
func TestH1a_SlashShareExportCopy(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_u", SessionID: "ses_1", Role: "user"},
		{ID: "msg_a", SessionID: "ses_1", Role: "assistant"},
	}
	m.store.parts["msg_u"] = []Part{{ID: "pu", MessageID: "msg_u", Type: "text", Text: "hello"}}
	m.store.parts["msg_a"] = []Part{{ID: "pa", MessageID: "msg_a", Type: "text", Text: "world"}}

	txt := m.formatTranscript()
	if !strings.Contains(txt, "hello") || !strings.Contains(txt, "world") {
		t.Fatalf("formatTranscript missing turns: %q", txt)
	}

	for _, name := range []string{"/share", "/unshare", "/compact", "/export", "/copy"} {
		found := false
		for _, it := range builtinCommands {
			if it.name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("builtinCommands missing %s", name)
		}
	}

	m.input.SetValue("/copy")
	m.ac = autocomplete{open: true, mode: acSlash, items: filterSlash("copy", nil), sel: 0}
	next, cmd := m.acceptSlash()
	m = next.(Model)
	if cmd == nil {
		t.Fatal("/copy should dispatch copyClipboardCmd")
	}
	if !strings.Contains(m.status, "copied transcript") {
		t.Fatalf("status = %q", m.status)
	}
}
