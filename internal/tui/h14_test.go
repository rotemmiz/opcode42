package tui

import (
	"testing"
)

func TestH14_MostRecentParentSession(t *testing.T) {
	sessions := []Session{
		{ID: "child", ParentID: "parent"},
		{ID: "parent", ParentID: ""},
		{ID: "older", ParentID: ""},
	}
	if got := mostRecentParentSessionID(sessions); got != "parent" {
		t.Fatalf("mostRecentParent = %q, want parent (first non-child)", got)
	}
	if got := mostRecentParentSessionID(nil); got != "" {
		t.Fatalf("empty list = %q", got)
	}
}

func TestH14_Continue_SelectsParent(t *testing.T) {
	m := New(Config{URL: "http://x", Continue: true, Provider: "p", Model: "m"})
	m, _ = step(t, m, sessionsLoadedMsg{sessions: []Session{
		{ID: "child", ParentID: "parent"},
		{ID: "parent"},
	}})
	if m.cfg.SessionID != "parent" {
		t.Fatalf("SessionID = %q, want parent", m.cfg.SessionID)
	}
	if m.screen != ScreenSession {
		t.Fatalf("screen = %v, want Session", m.screen)
	}
}

func TestH14_Fork_DispatchesForkCmd(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1", Fork: true, Provider: "p", Model: "m"})
	nm, cmd := step(t, m, sessionsLoadedMsg{sessions: []Session{{ID: "ses_1"}}})
	if cmd == nil {
		t.Fatal("fork startup should dispatch forkSessionCmd")
	}
	if !nm.startupForkDone {
		t.Fatal("startupForkDone should be set after first fork dispatch")
	}
	_, cmd2 := step(t, nm, sessionsLoadedMsg{sessions: []Session{{ID: "ses_1"}}})
	if cmd2 == nil {
		t.Fatal("second load after forkDone should still load messages")
	}
	// Confirm we didn't try to fork again: forkDone stays true and SessionID unchanged.
	if !nm.startupForkDone || nm.cfg.SessionID != "ses_1" {
		t.Fatalf("forkDone/session = %v %q", nm.startupForkDone, nm.cfg.SessionID)
	}
}

func TestH14_Prompt_PrefillsComposer(t *testing.T) {
	m := New(Config{URL: "http://x", Prompt: "hello world", Provider: "p", Model: "m"})
	if m.input.Value() != "hello world" {
		t.Fatalf("composer = %q", m.input.Value())
	}
	if !m.startupPromptArmed {
		t.Fatal("startupPromptArmed should be set")
	}
}

func TestH14_Prompt_AutoSubmitsWhenReady(t *testing.T) {
	m := New(Config{URL: "http://x", Prompt: "go", Provider: "p", Model: "m"})
	m.cfg.SessionID = "ses_1"
	next, cmd, ok := m.maybeSubmitStartupPrompt()
	if !ok || cmd == nil {
		t.Fatal("should auto-submit when model+prompt ready")
	}
	nm := next.(Model)
	if nm.startupPromptArmed {
		t.Fatal("armed flag should clear after submit")
	}
	if nm.input.Value() != "" {
		t.Fatalf("composer should clear on submit, got %q", nm.input.Value())
	}
}

func TestH14_Prompt_WaitsForModel(t *testing.T) {
	m := New(Config{URL: "http://x", Prompt: "go"})
	_, cmd, ok := m.maybeSubmitStartupPrompt()
	if ok || cmd != nil {
		t.Fatal("should not submit without a model")
	}
	if !m.startupPromptArmed {
		t.Fatal("should stay armed")
	}
}

func TestH14_Agent_SetsOnNew(t *testing.T) {
	m := New(Config{URL: "http://x", Agent: "plan"})
	if m.agent != "plan" {
		t.Fatalf("agent = %q, want plan", m.agent)
	}
}

func TestH14_ApplyStartup_ContinueAndFork(t *testing.T) {
	m := New(Config{URL: "http://x", Continue: true, Fork: true})
	m, cmd := m.applyStartupSessionArgs([]Session{
		{ID: "child", ParentID: "p"},
		{ID: "p"},
	})
	if m.cfg.SessionID != "p" {
		t.Fatalf("SessionID = %q, want p", m.cfg.SessionID)
	}
	if cmd == nil || !m.startupForkDone {
		t.Fatal("expected fork cmd + forkDone")
	}
}
