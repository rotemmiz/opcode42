package tui

import (
	"strings"
	"testing"
)

func TestH1b_UndoLastTurn(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_u1", SessionID: "ses_1", Role: "user"},
		{ID: "msg_a1", SessionID: "ses_1", Role: "assistant"},
	}
	m.store.parts["msg_u1"] = []Part{{ID: "pu", MessageID: "msg_u1", Type: "text", Text: "do it"}}
	m, cmd := m.undoLastTurn()
	if cmd == nil {
		t.Fatal("undoLastTurn should dispatch revertCmd")
	}
	if !strings.Contains(m.status, "undoing") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestH1b_UndoEmpty(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, cmd := m.undoLastTurn()
	if cmd != nil {
		t.Fatal("undo with no user turns should not dispatch")
	}
	if !strings.Contains(m.status, "nothing to undo") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestH1b_Redo(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, cmd := m.redoTurn()
	if cmd == nil {
		t.Fatal("redoTurn should dispatch unrevertCmd")
	}
	if !strings.Contains(m.status, "redoing") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestH1b_LeaderU_Undo(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.store.messages["ses_1"] = []Message{{ID: "msg_u1", SessionID: "ses_1", Role: "user"}}
	m.store.parts["msg_u1"] = []Part{{ID: "pu", MessageID: "msg_u1", Type: "text", Text: "hi"}}
	m, _ = step(t, m, key("ctrl+x"))
	m, cmd := step(t, m, key("u"))
	if cmd == nil {
		t.Fatal("ctrl+x u should dispatch revert")
	}
}

func TestH1b_LeaderU_Redo(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, _ = step(t, m, key("ctrl+x"))
	m, cmd := step(t, m, key("U"))
	if cmd == nil {
		t.Fatal("ctrl+x U should dispatch unrevert")
	}
}

func TestH1b_JumpLastUser(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.width, m.height = 80, 24
	m.store.messages["ses_1"] = []Message{{ID: "msg_u1", SessionID: "ses_1", Role: "user"}}
	m = m.jumpLastUser()
	if !strings.Contains(m.status, "last user") {
		t.Fatalf("status = %q", m.status)
	}
}
