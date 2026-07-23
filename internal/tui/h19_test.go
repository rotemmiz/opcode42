package tui

import "testing"

func TestH19_SessionDraft_SaveRestore(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_a", Provider: "p", Model: "m"})
	m.store.sessions = []Session{{ID: "ses_a"}, {ID: "ses_b"}}
	m.input.SetValue("draft for A")
	m, _ = m.openSession("ses_b")
	if m.cfg.SessionID != "ses_b" {
		t.Fatalf("SessionID = %q", m.cfg.SessionID)
	}
	if m.input.Value() != "" {
		t.Fatalf("ses_b composer should start empty, got %q", m.input.Value())
	}
	m.input.SetValue("draft for B")
	m, _ = m.openSession("ses_a")
	if m.input.Value() != "draft for A" {
		t.Fatalf("restored A draft = %q, want %q", m.input.Value(), "draft for A")
	}
	m, _ = m.openSession("ses_b")
	if m.input.Value() != "draft for B" {
		t.Fatalf("restored B draft = %q", m.input.Value())
	}
}

func TestH19_SessionDraft_ClearsWhenEmptied(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_a"})
	m.input.SetValue("keep")
	m, _ = m.openSession("ses_b")
	m.input.SetValue("")
	m, _ = m.openSession("ses_a")
	if m.input.Value() != "keep" {
		t.Fatalf("A = %q", m.input.Value())
	}
	m, _ = m.openSession("ses_b")
	if m.input.Value() != "" {
		t.Fatalf("emptied B should restore empty, got %q", m.input.Value())
	}
}
