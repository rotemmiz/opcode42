package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestThemeModal_SelectChangesStyles(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if m.themeName != "forge-dark" {
		t.Fatalf("default theme should be forge-dark, got %q", m.themeName)
	}
	darkBlue := m.styles.P.Blue

	m.modal, m.modalSel = modalThemes, 2 // monochrome
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.themeName != "monochrome" {
		t.Fatalf("theme should switch to monochrome, got %q", nm.themeName)
	}
	if nm.styles.P.Blue == darkBlue {
		t.Fatal("the live styles palette should change when the theme changes")
	}
	if nm.modal != modalNone {
		t.Fatal("selecting a theme should close the modal")
	}
}

func TestThemeSwitch_RestylesComposer(t *testing.T) {
	m := New(Config{URL: "http://x"})
	darkText := m.input.FocusedStyle.Text.GetForeground()
	if darkText == nil {
		t.Fatal("composer text color should be set from the theme at startup, not terminal-default")
	}

	m.modal, m.modalSel = modalThemes, 1 // forge-light
	next, _ := m.modalSelect()
	nm := next.(Model)
	if nm.input.FocusedStyle.Text.GetForeground() == darkText {
		t.Fatal("composer text color should follow the theme switch")
	}
}

func TestAgentsLoaded_DropsStaleSelection(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.agent = "plan"
	// reconnect to a daemon that no longer offers "plan"
	m, _ = step(t, m, agentsLoadedMsg{items: []agentItem{{name: "build", mode: "primary"}}})
	if m.agent != "" {
		t.Fatalf("a no-longer-present agent should be cleared, got %q", m.agent)
	}
	// a still-present selection survives
	m.agent = "build"
	m, _ = step(t, m, agentsLoadedMsg{items: []agentItem{{name: "build"}}})
	if m.agent != "build" {
		t.Fatal("a still-present agent should be kept")
	}
}

func TestAgentModal_SelectSetsAgent(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.agents = []agentItem{{name: "build", mode: "primary"}, {name: "plan", mode: "primary"}}
	m.modal, m.modalSel = modalAgents, 1
	next, _ := m.modalSelect()
	if nm := next.(Model); nm.agent != "plan" || nm.modal != modalNone {
		t.Fatalf("selecting should set agent=plan and close, got agent=%q modal=%v", nm.agent, nm.modal)
	}
}

func TestAgentAndThemeSelIndex(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.agents = []agentItem{{name: "build"}, {name: "plan"}}
	m.agent = "plan"
	if got := m.agentSelIndex(); got != 1 {
		t.Fatalf("agentSelIndex = %d, want 1", got)
	}
	m.themeName = "monochrome"
	if got := m.themeSelIndex(); got != 2 {
		t.Fatalf("themeSelIndex = %d, want 2", got)
	}
}

func TestPalette_SwitchAgentOpensAgentsModal(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = step(t, m, key("ctrl+p"))
	for paletteItems[m.modalSel].action != paSwitchAgent { // walk down to "Switch agent"
		m, _ = step(t, m, key("down"))
	}
	next, cmd := step(t, m, key("enter"))
	if next.modal != modalAgents || cmd == nil {
		t.Fatalf("Switch agent should open the agents modal + load, modal=%v cmd=%v", next.modal, cmd != nil)
	}
}

func TestSlash_AgentsAndThemesBuiltins(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("/agents")
	m, _ = m.refreshAutocomplete()
	next, cmd := m.acceptSlash()
	if next.(Model).modal != modalAgents || cmd == nil {
		t.Fatal("/agents should open the agents modal + load")
	}

	m2 := New(Config{URL: "http://x"})
	m2.input.SetValue("/themes")
	m2, _ = m2.refreshAutocomplete()
	if next2, _ := m2.acceptSlash(); next2.(Model).modal != modalThemes {
		t.Fatal("/themes should open the themes modal")
	}
}

func TestPromptBody_IncludesAgentWhenSet(t *testing.T) {
	b, _ := json.Marshal(promptBody{
		Model: promptModelWire{ProviderID: "p", ModelID: "m"},
		Agent: "plan",
		Parts: []partInput{{Type: "text", Text: "hi"}},
	})
	if !strings.Contains(string(b), `"agent":"plan"`) {
		t.Fatalf("prompt body should carry the agent: %s", b)
	}
	b2, _ := json.Marshal(promptBody{Model: promptModelWire{ProviderID: "p", ModelID: "m"}})
	if strings.Contains(string(b2), "agent") {
		t.Fatalf("agent should be omitted when empty: %s", b2)
	}
}
