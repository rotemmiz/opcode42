package tui

import (
	"strings"
	"testing"
)

// Tests for plan 08f H9 (DialogMessage + DialogForkFromTimeline).

func TestH9_MessageActions_RevertCopyFork(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.store.messages["ses_1"] = []Message{{ID: "msg_1", SessionID: "ses_1", Role: "user"}}
	m.store.parts["msg_1"] = []Part{{ID: "p1", Type: "text", Text: "hello world"}}
	m.messageActionID = "msg_1"

	// Revert
	m.modal, m.modalSel = modalMessage, 0
	_, cmd := m.modalSelect()
	if cmd == nil {
		t.Fatal("Revert should dispatch revertCmd")
	}

	// Copy
	m = New(Config{URL: "http://x", SessionID: "ses_1"})
	m.store.parts["msg_1"] = []Part{{ID: "p1", Type: "text", Text: "hello world"}}
	m.messageActionID = "msg_1"
	m.modal, m.modalSel = modalMessage, 1
	next, cmd := m.modalSelect()
	if cmd == nil {
		t.Fatal("Copy should dispatch copyClipboardCmd")
	}
	if !strings.Contains(next.(Model).status, "copied") {
		t.Fatalf("status=%q", next.(Model).status)
	}

	// Fork
	m = New(Config{URL: "http://x", SessionID: "ses_1"})
	m.store.parts["msg_1"] = []Part{{ID: "p1", Type: "text", Text: "hello world"}}
	m.messageActionID = "msg_1"
	m.modal, m.modalSel = modalMessage, 2
	_, cmd = m.modalSelect()
	if cmd == nil {
		t.Fatal("Fork should dispatch forkSessionCmd")
	}
}

func TestH9_ForkModal_FullAndAnchored(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_1", SessionID: "ses_1", Role: "user"},
		{ID: "msg_2", SessionID: "ses_1", Role: "user"},
	}
	m.store.parts["msg_1"] = []Part{{Type: "text", Text: "older"}}
	m.store.parts["msg_2"] = []Part{{Type: "text", Text: "newer"}}

	title, rows, _ := func() (string, []string, string) {
		m.modal = modalFork
		return m.modalItems()
	}()
	if title != "Fork session" {
		t.Fatalf("title=%q", title)
	}
	if len(rows) < 3 || rows[0] != "Full session" {
		t.Fatalf("rows=%v", rows)
	}
	if rows[1] != "newer" || rows[2] != "older" {
		t.Fatalf("want newest-first after Full session, got %v", rows)
	}

	// Full session
	m.modal, m.modalSel = modalFork, 0
	_, cmd := m.modalSelect()
	if cmd == nil {
		t.Fatal("Full session fork should dispatch")
	}

	// Anchored at newest (sel=1)
	m.modal, m.modalSel = modalFork, 1
	_, cmd = m.modalSelect()
	if cmd == nil {
		t.Fatal("anchored fork should dispatch")
	}
}

func TestH9_PaletteFork_OpensModal(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.modal, m.modalSel = modalPalette, indexOfAction(paFork)
	next, cmd := m.modalSelect()
	if cmd != nil {
		t.Fatal("Fork palette should open modal, not dispatch immediately")
	}
	if next.(Model).modal != modalFork {
		t.Fatalf("modal=%v, want modalFork", next.(Model).modal)
	}
}

func TestH9_ForkedMsg_RestoresPrompt(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_old"})
	m, _ = step(t, m, forkedMsg{
		session: Session{ID: "ses_new", Title: "fork"},
		prompt:  "restored prompt",
	})
	if m.cfg.SessionID != "ses_new" {
		t.Fatalf("SessionID=%q", m.cfg.SessionID)
	}
	if m.input.Value() != "restored prompt" {
		t.Fatalf("composer=%q", m.input.Value())
	}
}

func TestH9_UserPromptText(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.parts["m1"] = []Part{
		{Type: "text", Text: "hello"},
		{Type: "reasoning", Text: "secret"},
		{Type: "text", Text: " world"},
	}
	if got := m.userPromptText("m1"); got != "hello world" {
		t.Fatalf("userPromptText=%q", got)
	}
}
