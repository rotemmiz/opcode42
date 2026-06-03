package tui

import (
	"encoding/json"
	"testing"
)

// Tests for plan 08a parity features: shell mode, prompt history, effective
// agent, MCP status parsing, display toggles, and the palette wiring.

func TestShellMode_ToggleOnEmptyComposer(t *testing.T) {
	m := New(Config{URL: "http://x"})

	// "!" on an empty composer enters shell mode.
	m, _ = step(t, m, key("!"))
	if !m.shellMode {
		t.Fatal(`"!" on empty composer should enter shell mode`)
	}
	// esc exits shell mode.
	m, _ = step(t, m, key("esc"))
	if m.shellMode {
		t.Fatal("esc should exit shell mode")
	}
}

func TestShellMode_BangMidTextTypes(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("echo")

	m, _ = step(t, m, key("!"))
	if m.shellMode {
		t.Fatal(`"!" with a non-empty composer should type, not toggle shell mode`)
	}
}

func TestHistory_RecallWalksAndExits(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.history = []string{"first", "second", "third"}
	m.histIdx = -1

	// History recall is on ctrl+↑/↓ (plain arrows scroll the stream, and the wheel
	// arrives as plain arrows under alternate scroll mode).
	m, _ = step(t, m, key("ctrl+up")) // newest
	if got := m.input.Value(); got != "third" {
		t.Fatalf("ctrl+up #1 = %q, want third", got)
	}
	m, _ = step(t, m, key("ctrl+up"))
	if got := m.input.Value(); got != "second" {
		t.Fatalf("ctrl+up #2 = %q, want second", got)
	}
	m, _ = step(t, m, key("ctrl+down")) // back toward newest
	if got := m.input.Value(); got != "third" {
		t.Fatalf("ctrl+down = %q, want third", got)
	}
	m, _ = step(t, m, key("ctrl+down")) // past newest → live (empty) composer
	if got := m.input.Value(); got != "" || m.histIdx != -1 {
		t.Fatalf("ctrl+down past newest = %q idx=%d, want empty/-1", m.input.Value(), m.histIdx)
	}
}

func TestHistory_RecallSkippedWithDraft(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.history = []string{"old"}
	m.histIdx = -1
	m.input.SetValue("draft in progress")

	m, _ = step(t, m, key("up"))
	if m.input.Value() != "draft in progress" {
		t.Fatalf("up must not clobber a draft, got %q", m.input.Value())
	}
}

func TestPushHistory_DedupAdjacentAndCap(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m = m.pushHistory("a")
	m = m.pushHistory("a") // adjacent dup ignored
	m = m.pushHistory("b")
	if len(m.history) != 2 || m.history[0] != "a" || m.history[1] != "b" {
		t.Fatalf("history = %v, want [a b]", m.history)
	}
}

func TestEffectiveAgent(t *testing.T) {
	m := New(Config{URL: "http://x"})
	if got := m.effectiveAgent(); got != "build" {
		t.Fatalf("no agents → %q, want build", got)
	}
	m.agents = []agentItem{{name: "plan"}, {name: "build"}}
	if got := m.effectiveAgent(); got != "build" {
		t.Fatalf("prefers build → %q", got)
	}
	m.agents = []agentItem{{name: "plan"}, {name: "review"}}
	if got := m.effectiveAgent(); got != "plan" {
		t.Fatalf("no build → first agent, got %q", got)
	}
	m.agent = "review"
	if got := m.effectiveAgent(); got != "review" {
		t.Fatalf("selected agent wins, got %q", got)
	}
}

func TestMCPStatus(t *testing.T) {
	cases := map[string]string{
		`{"status":"connected"}`: "connected",
		`{"state":"error"}`:      "error",
		`{"enabled":false}`:      "disabled",
		`{"type":"local"}`:       "local",
		`{}`:                     "",
	}
	for in, want := range cases {
		if got := mcpStatus(json.RawMessage(in)); got != want {
			t.Errorf("mcpStatus(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestViewToggles_ViaLeader(t *testing.T) {
	m := New(Config{URL: "http://x"})
	// ctrl+x r toggles thinking visibility.
	m, _ = step(t, m, key("ctrl+x"))
	m, _ = step(t, m, key("r"))
	if !m.view.hideThinking {
		t.Fatal("ctrl+x r should hide thinking")
	}
	// ctrl+x o toggles tool output.
	m, _ = step(t, m, key("ctrl+x"))
	m, _ = step(t, m, key("o"))
	if !m.view.hideTools {
		t.Fatal("ctrl+x o should hide tool output")
	}
}

func TestPalette_IncludesSessionOps(t *testing.T) {
	want := map[paletteAction]bool{
		paRename: false, paFork: false, paSummarize: false, paAbort: false,
		paShare: false, paUnshare: false, paDelete: false, paMCP: false,
		paSkills: false, paHelp: false,
	}
	for _, it := range paletteItems {
		if _, ok := want[it.action]; ok {
			want[it.action] = true
		}
	}
	for a, seen := range want {
		if !seen {
			t.Errorf("palette missing action %d", a)
		}
	}
}

func TestRenameModal_OpensAndPrefills(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.cfg.SessionID = "ses_1"
	m.store.sessions = []Session{{ID: "ses_1", Title: "My session"}}
	// Open palette → select Rename via direct dispatch.
	m.modal, m.modalSel = modalPalette, indexOfAction(paRename)
	next, _ := m.modalSelect()
	m = next.(Model)
	if m.modal != modalRename {
		t.Fatalf("expected rename modal, got %v", m.modal)
	}
	if m.renameInput.Value() != "My session" {
		t.Fatalf("rename input not prefilled, got %q", m.renameInput.Value())
	}
}

func indexOfAction(a paletteAction) int {
	for i, it := range paletteItems {
		if it.action == a {
			return i
		}
	}
	return -1
}
