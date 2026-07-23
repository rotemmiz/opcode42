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
	m.revertMessageID = "msg_u1"
	m, cmd := m.redoTurn()
	if cmd == nil {
		t.Fatal("redoTurn should dispatch unrevertCmd")
	}
	if !strings.Contains(m.status, "redoing") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestH1b_RedoWithoutCheckpoint(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m, cmd := m.redoTurn()
	if cmd != nil {
		t.Fatal("redo without checkpoint should not dispatch")
	}
	if !strings.Contains(m.status, "nothing to redo") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestH1b_UndoWalksPastCheckpoint(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_u1", SessionID: "ses_1", Role: "user"},
		{ID: "msg_u2", SessionID: "ses_1", Role: "user"},
	}
	m.revertMessageID = "msg_u2"
	if got := m.undoTargetID(); got != "msg_u1" {
		t.Fatalf("undoTargetID with checkpoint msg_u2 = %q, want msg_u1", got)
	}
}

func TestH1b_LeaderU_Redo(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.revertMessageID = "msg_u1"
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
	m.scroll.Back(12)
	m = m.jumpLastUser()
	if m.scroll.Offset != 0 {
		t.Fatalf("jumpLastUser should ToTail (Offset=0), got %d", m.scroll.Offset)
	}
	if !strings.Contains(m.status, "last user") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestReplaceHistory_DropsStale(t *testing.T) {
	s := newStore()
	s.messages["ses_1"] = []Message{{ID: "old", SessionID: "ses_1", Role: "user"}}
	s.parts["old"] = []Part{{ID: "p", MessageID: "old", Type: "text", Text: "gone"}}
	s = s.replaceHistory("ses_1", []wireWithParts{
		{Info: Message{ID: "new", SessionID: "ses_1", Role: "user"}, Parts: []Part{{ID: "pn", MessageID: "new", Type: "text", Text: "kept"}}},
	})
	if len(s.messages["ses_1"]) != 1 || s.messages["ses_1"][0].ID != "new" {
		t.Fatalf("replaceHistory messages = %+v", s.messages["ses_1"])
	}
	if _, ok := s.parts["old"]; ok {
		t.Fatal("replaceHistory should drop parts for removed messages")
	}
}
