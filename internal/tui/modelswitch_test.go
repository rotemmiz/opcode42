package tui

import "testing"

func sampleResp() providerResp {
	return providerResp{
		All: []providerWire{
			{ID: "anthropic", Name: "Anthropic", Models: map[string]modelWire{
				"claude-sonnet-4": {ID: "claude-sonnet-4", Name: "Claude Sonnet 4"},
				"claude-opus-4":   {ID: "claude-opus-4", Name: "Claude Opus 4"},
			}},
			{ID: "openai", Name: "OpenAI", Models: map[string]modelWire{
				"gpt-4o": {ID: "gpt-4o", Name: "GPT-4o"},
			}},
			{ID: "google", Name: "Google", Models: map[string]modelWire{
				"gemini-2.5-flash": {ID: "gemini-2.5-flash"},
			}},
		},
		Connected: []string{"anthropic", "openai"}, // google not connected
	}
}

func TestChoices_FiltersToConnectedAndSorts(t *testing.T) {
	got := sampleResp().choices()
	want := []string{
		"anthropic / claude-opus-4",
		"anthropic / claude-sonnet-4",
		"openai / gpt-4o",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d choices (connected only, sorted), got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i].label() != w {
			t.Fatalf("choice %d: want %q got %q", i, w, got[i].label())
		}
	}
}

func TestModelSelIndex_FindsActive(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.choices = sampleResp().choices()
	m.model = promptModel{Provider: "openai", Model: "gpt-4o"}
	if got := m.modelSelIndex(); got != 2 {
		t.Fatalf("active model should be index 2, got %d", got)
	}
	m.model = promptModel{Provider: "nope", Model: "nope"} // unknown -> 0
	if got := m.modelSelIndex(); got != 0 {
		t.Fatalf("unknown model should default to 0, got %d", got)
	}
}

func TestModelsModal_SelectSetsModel(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.choices = sampleResp().choices()
	m.modal, m.modalSel = modalModels, 1 // anthropic / claude-sonnet-4
	m, _ = step(t, m, key("enter"))
	if m.modal != modalNone {
		t.Fatal("selecting a model should close the modal")
	}
	if m.model.Provider != "anthropic" || m.model.Model != "claude-sonnet-4" {
		t.Fatalf("model not switched: %+v", m.model)
	}
}

func TestPalette_SwitchModelOpensModelsModal(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, key("ctrl+p"))
	m, _ = step(t, m, key("down")) // 1 Switch session
	m, _ = step(t, m, key("down")) // 2 Switch model
	if m.modalSel != 2 {
		t.Fatalf("expected selection 2 (Switch model), got %d", m.modalSel)
	}
	m, cmd := step(t, m, key("enter"))
	if m.modal != modalModels || cmd == nil {
		t.Fatalf("Switch model should open the models modal + load providers, modal=%v cmd=%v", m.modal, cmd != nil)
	}
}

func TestProvidersLoaded_RehighlightsActiveWhileModalOpen(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.model = promptModel{Provider: "openai", Model: "gpt-4o"}
	m.modal, m.modalSel = modalModels, 0
	m, _ = step(t, m, providersLoadedMsg{choices: sampleResp().choices()})
	if m.modalSel != 2 {
		t.Fatalf("providers load should re-highlight the active model (index 2), got %d", m.modalSel)
	}
}
