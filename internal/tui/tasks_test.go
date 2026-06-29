package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

func TestTasksDock_TogglesAndRenders(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}
	m.todos = []Todo{
		{Content: "write the parser", Status: "in_progress"},
		{Content: "add tests", Status: "pending"},
		{Content: "scaffold", Status: "completed"},
	}

	// closed by default
	if strings.Contains(m.renderSession(), "Tasks") {
		t.Fatal("dock should be hidden until toggled")
	}
	// ctrl+x t opens it (+ fetches)
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	m, cmd := step(t, m, key("t"))
	if !m.tasksOpen || cmd == nil {
		t.Fatal("ctrl+x t should open the dock and fetch todos")
	}
	view := m.renderSession()
	for _, want := range []string{"Tasks", "1/3", "write the parser", "add tests", "scaffold"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dock missing %q", want)
		}
	}
	// ctrl+x t again closes it
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlX})
	m, _ = step(t, m, key("t"))
	if m.tasksOpen {
		t.Fatal("ctrl+x t should toggle the dock closed")
	}
}

func TestTasksDock_SortsActiveFirst(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.todos = []Todo{
		{Content: "done", Status: "completed"},
		{Content: "now", Status: "in_progress"},
		{Content: "later", Status: "pending"},
	}
	got := m.sortedTodos()
	if got[0].Status != "in_progress" || got[1].Status != "pending" || got[2].Status != "completed" {
		t.Fatalf("todos should sort active→pending→done, got %+v", got)
	}
}

func TestTodosLoaded_OnlyAppliesToCurrentSession(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m, _ = step(t, m, todosLoadedMsg{sessionID: "ses_other", todos: []Todo{{Content: "x"}}})
	if len(m.todos) != 0 {
		t.Fatal("todos for a different session must be ignored")
	}
	m, _ = step(t, m, todosLoadedMsg{sessionID: "ses_1", todos: []Todo{{Content: "x"}}})
	if len(m.todos) != 1 {
		t.Fatal("todos for the current session should apply")
	}
}

func TestIsTodoWriteEvent(t *testing.T) {
	yes := opcode42client.SSEEvent{Type: "message.part.updated", Properties: []byte(`{"part":{"type":"tool","tool":"todowrite"}}`)}
	if !isTodoWriteEvent(yes) {
		t.Fatal("a todowrite part update should be detected")
	}
	no := opcode42client.SSEEvent{Type: "message.part.updated", Properties: []byte(`{"part":{"type":"tool","tool":"bash"}}`)}
	if isTodoWriteEvent(no) {
		t.Fatal("a non-todowrite part should not trigger a refetch")
	}
	if isTodoWriteEvent(opcode42client.SSEEvent{Type: "session.updated"}) {
		t.Fatal("a non-part event should not match")
	}
}
