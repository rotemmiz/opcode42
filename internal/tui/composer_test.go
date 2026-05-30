package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubmit_RequiresModel(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"}) // no provider/model
	m.input.SetValue("hi")
	next, cmd := m.submit()
	if cmd != nil {
		t.Fatal("submit without a model should not dispatch")
	}
	if !strings.Contains(next.(Model).status, "no model") {
		t.Fatalf("status should explain the missing model: %q", next.(Model).status)
	}
}

func TestSubmit_EmptyIsNoop(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m"})
	m.input.SetValue("   ")
	if _, cmd := m.submit(); cmd != nil {
		t.Fatal("empty submit should be a no-op")
	}
}

func TestSubmit_DispatchesAndClearsInput(t *testing.T) {
	// existing session → prompt
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.input.SetValue("hello")
	next, cmd := m.submit()
	if cmd == nil {
		t.Fatal("submit with session+model should dispatch")
	}
	if next.(Model).input.Value() != "" {
		t.Fatal("input should be cleared on submit")
	}

	// no session → create-then-prompt
	m2 := New(Config{URL: "http://x", Provider: "p", Model: "m"})
	m2.input.SetValue("hello")
	if _, cmd := m2.submit(); cmd == nil {
		t.Fatal("submit without a session should create one")
	}
}

func TestConfigLoaded_ResolvesModelUnlessFlagged(t *testing.T) {
	m := New(Config{URL: "http://x"})
	next, _ := m.Update(configLoadedMsg{provider: "anthropic", model: "claude-x"})
	if mm := next.(Model); mm.model.Provider != "anthropic" || mm.model.Model != "claude-x" {
		t.Fatalf("config did not resolve model: %+v", mm.model)
	}
	// explicit flags win over /config
	m2 := New(Config{URL: "http://x", Provider: "flagp", Model: "flagm"})
	next2, _ := m2.Update(configLoadedMsg{provider: "anthropic", model: "claude-x"})
	if next2.(Model).model.Provider != "flagp" {
		t.Fatalf("flags should take precedence over /config")
	}
}

func TestPromptBody_WireShape(t *testing.T) {
	b, _ := json.Marshal(promptBody{
		Model: promptModelWire{ProviderID: "openai", ModelID: "gpt-4o"},
		Parts: []partInput{{Type: "text", Text: "hi"}},
	})
	s := string(b)
	for _, want := range []string{`"providerID":"openai"`, `"modelID":"gpt-4o"`, `"type":"text"`, `"text":"hi"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("prompt body missing %s: %s", want, s)
		}
	}
}
