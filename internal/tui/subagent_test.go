package tui

import (
	"strings"
	"testing"
)

// withSubagents builds a model with a parent session and two sub-agent children.
func withSubagents() Model {
	m := New(Config{URL: "http://x"})
	m.store.sessions = []Session{
		{ID: "ses_parent", Title: "Build the thing"},
		{ID: "ses_child1", ParentID: "ses_parent", Title: "@review subagent: check"},
		{ID: "ses_child2", ParentID: "ses_parent", Title: "@plan subagent: design"},
	}
	m.cfg.SessionID = "ses_parent"
	m.screen = ScreenSession
	return m
}

func TestChildrenOf(t *testing.T) {
	m := withSubagents()
	kids := m.childrenOf("ses_parent")
	if len(kids) != 2 || kids[0].ID != "ses_child1" || kids[1].ID != "ses_child2" {
		t.Fatalf("childrenOf(parent) = %+v, want the two children in order", kids)
	}
	if got := m.childrenOf("ses_child1"); len(got) != 0 {
		t.Fatalf("a child has no children, got %+v", got)
	}
	if got := m.childrenOf(""); got != nil {
		t.Fatalf("childrenOf(empty) should be nil, got %+v", got)
	}
}

func TestSubagentLabel(t *testing.T) {
	cases := map[string]string{
		"@review subagent: check this": "Review",
		"@plan subagent":               "Plan",
		"some other title":             "Subagent",
		"":                             "Subagent",
	}
	for title, want := range cases {
		if got := subagentLabel(Session{Title: title}); got != want {
			t.Errorf("subagentLabel(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestEnterFirstChild(t *testing.T) {
	m := withSubagents()
	next, cmd := m.enterFirstChild()
	nm := next.(Model)
	if nm.cfg.SessionID != "ses_child1" {
		t.Fatalf("enterFirstChild → %q, want ses_child1", nm.cfg.SessionID)
	}
	if cmd == nil {
		t.Fatal("entering a child should load its stream")
	}
	// A childless session is a no-op (with a status hint).
	m2 := New(Config{URL: "http://x"})
	m2.cfg.SessionID = "ses_solo"
	m2.store.sessions = []Session{{ID: "ses_solo"}}
	next2, _ := m2.enterFirstChild()
	if next2.(Model).cfg.SessionID != "ses_solo" {
		t.Fatal("enterFirstChild with no children must not navigate")
	}
}

func TestGotoParent(t *testing.T) {
	m := withSubagents()
	m.cfg.SessionID = "ses_child2"
	next, cmd := m.gotoParent()
	if next.(Model).cfg.SessionID != "ses_parent" || cmd == nil {
		t.Fatalf("gotoParent from child → %q (cmd=%v), want ses_parent", next.(Model).cfg.SessionID, cmd != nil)
	}
	// From a top-level session, parent nav is a no-op.
	m.cfg.SessionID = "ses_parent"
	next2, _ := m.gotoParent()
	if next2.(Model).cfg.SessionID != "ses_parent" {
		t.Fatal("gotoParent from a root session must not navigate")
	}
}

func TestCycleSibling(t *testing.T) {
	m := withSubagents()
	m.cfg.SessionID = "ses_child1"
	// next → child2
	next, _ := m.cycleSibling(+1)
	if got := next.(Model).cfg.SessionID; got != "ses_child2" {
		t.Fatalf("cycle next → %q, want ses_child2", got)
	}
	// next again wraps → child1
	m.cfg.SessionID = "ses_child2"
	next, _ = m.cycleSibling(+1)
	if got := next.(Model).cfg.SessionID; got != "ses_child1" {
		t.Fatalf("cycle next wrap → %q, want ses_child1", got)
	}
	// prev from child1 wraps → child2
	m.cfg.SessionID = "ses_child1"
	next, _ = m.cycleSibling(-1)
	if got := next.(Model).cfg.SessionID; got != "ses_child2" {
		t.Fatalf("cycle prev wrap → %q, want ses_child2", got)
	}
}

func TestSubagentFooter_ChildAndParent(t *testing.T) {
	m := withSubagents()
	m.width, m.height = 100, 30

	// Parent view: an invitation to descend.
	pf := m.subagentFooterView(80)
	if !strings.Contains(pf, "2 sub-agents") || !strings.Contains(pf, "enter") {
		t.Fatalf("parent footer = %q, want '2 sub-agents … enter'", pf)
	}

	// Child view: label + position among siblings + parent nav.
	m.cfg.SessionID = "ses_child1"
	cf := m.subagentFooterView(80)
	if !strings.Contains(cf, "Review") || !strings.Contains(cf, "(1 of 2)") || !strings.Contains(cf, "parent") {
		t.Fatalf("child footer = %q, want 'Review (1 of 2) … parent'", cf)
	}

	// A plain session with no parent and no children: no footer.
	m.cfg.SessionID = "ses_parent"
	m.store.sessions = []Session{{ID: "ses_parent", Title: "solo"}}
	if got := m.subagentFooterView(80); got != "" {
		t.Fatalf("childless root footer = %q, want empty", got)
	}
}

func TestSubagentNav_ViaLeader(t *testing.T) {
	m := withSubagents()
	// ctrl+x ↓ descends into the first child.
	m, _ = step(t, m, key("ctrl+x"))
	m, _ = step(t, m, key("down"))
	if m.cfg.SessionID != "ses_child1" {
		t.Fatalf("ctrl+x ↓ → %q, want ses_child1", m.cfg.SessionID)
	}
	// ctrl+x ] cycles to the next sibling.
	m, _ = step(t, m, key("ctrl+x"))
	m, _ = step(t, m, key("]"))
	if m.cfg.SessionID != "ses_child2" {
		t.Fatalf("ctrl+x ] → %q, want ses_child2", m.cfg.SessionID)
	}
	// ctrl+x ↑ returns to the parent.
	m, _ = step(t, m, key("ctrl+x"))
	m, _ = step(t, m, key("up"))
	if m.cfg.SessionID != "ses_parent" {
		t.Fatalf("ctrl+x ↑ → %q, want ses_parent", m.cfg.SessionID)
	}
}
