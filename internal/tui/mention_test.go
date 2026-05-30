package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMentionQuery(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		match bool
	}{
		{"@", "", true},
		{"@mod", "mod", true},
		{"hey @mod", "mod", true},
		{"hey @mod ", "", false},  // space ends the mention
		{"hey @a b", "", false},   // token has inner space
		{"email a@b", "", false},  // @ not token-initial
		{"no mention", "", false}, // no @
		{"@a\nb", "", false},      // newline ends it
	}
	for _, c := range cases {
		got, ok := mentionQuery(c.in)
		if ok != c.match || (ok && got != c.want) {
			t.Errorf("mentionQuery(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.match)
		}
	}
}

func TestMention_FilesFoundOpensPopup(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("@mod")
	// simulate the search returning matches for the active query
	m, _ = step(t, m, filesFoundMsg{query: "mod", files: []string{"internal/tui/model.go", "internal/tui/modal.go"}})
	if !m.ac.open || m.ac.mode != acMention || len(m.ac.files) != 2 {
		t.Fatalf("file results should open the mention popup, got open=%v mode=%v files=%d", m.ac.open, m.ac.mode, len(m.ac.files))
	}
	// stale result (query no longer matches) is ignored
	m.input.SetValue("@xyz")
	m2, _ := step(t, m, filesFoundMsg{query: "mod", files: []string{"old"}})
	if len(m2.ac.files) == 1 && m2.ac.files[0] == "old" {
		t.Fatal("a stale file result should be dropped")
	}
}

func TestMention_AcceptInsertsPath(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("explain @mod")
	m.ac = autocomplete{open: true, mode: acMention, files: []string{"internal/tui/model.go"}, sel: 0}
	m = m.acceptMention()
	if got := m.input.Value(); got != "explain @internal/tui/model.go " {
		t.Fatalf("accept should replace @token with @path, got %q", got)
	}
	if m.ac.open {
		t.Fatal("accept should close the popup")
	}
}

func TestMention_EmptyResultClosesPopup(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.input.SetValue("@zzz")
	m, _ = step(t, m, filesFoundMsg{query: "zzz", files: nil})
	if m.ac.open {
		t.Fatal("no matches should leave the popup closed (no invisible key capture)")
	}
}

func TestLeader_DispatchesChords(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// ctrl+x then a → agents modal (+ load)
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if !m.leader {
		t.Fatal("ctrl+x should arm the leader")
	}
	next, cmd := step(t, m, key("a"))
	if next.modal != modalAgents || cmd == nil {
		t.Fatalf("ctrl+x a should open the agents modal, got modal=%v", next.modal)
	}
	if next.leader {
		t.Fatal("the leader should clear after the chord")
	}
}

func TestLeader_SidebarToggleAndUnknownKey(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 24})
	if !m.sidebarVisible() {
		t.Fatal("sidebar should be visible at this size")
	}
	// ctrl+x b toggles the sidebar off
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	m, _ = step(t, m, key("b"))
	if m.sidebarVisible() {
		t.Fatal("ctrl+x b should hide the sidebar")
	}
	// unknown chord is an inert no-op that clears the leader
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	m, _ = step(t, m, key("z"))
	if m.leader {
		t.Fatal("an unknown chord should still clear the leader")
	}
}
