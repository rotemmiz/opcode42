package tui

import (
	"context"
	"sort"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// U11 — tasks board dock. The session's todos (GET /session/:id/todo, written by
// the agent's todowrite tool) render as a bottom strip toggled with ctrl+x t.
// There's no todo SSE event, so it's refetched on open and whenever a todowrite
// tool part streams in.

// Todo is one session task.
type Todo struct {
	Content  string `json:"content"`
	Status   string `json:"status"` // pending | in_progress | completed | cancelled
	Priority string `json:"priority"`
}

// todosLoadedMsg carries a session's todos.
type todosLoadedMsg struct {
	sessionID string
	todos     []Todo
	err       error
}

// loadTodosCmd fetches the session's todos.
func loadTodosCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if sessionID == "" {
			return todosLoadedMsg{}
		}
		var todos []Todo
		err := c.GetJSON(ctx, "/session/"+sessionID+"/todo", &todos)
		return todosLoadedMsg{sessionID: sessionID, todos: todos, err: err}
	}
}

// maxDockRows caps the visible task rows so the dock can't swallow the stream.
const maxDockRows = 6

// todoRank orders statuses: active work first, done/cancelled last.
func todoRank(status string) int {
	switch status {
	case "in_progress":
		return 0
	case "pending":
		return 1
	case "completed":
		return 2
	case "cancelled":
		return 3
	}
	return 1
}

// sortedTodos returns the todos ordered by status rank (stable within a rank).
func (m Model) sortedTodos() []Todo {
	out := append([]Todo(nil), m.todos...)
	sort.SliceStable(out, func(i, j int) bool { return todoRank(out[i].Status) < todoRank(out[j].Status) })
	return out
}

// todoGlyph is the status marker + its color.
func (m Model) todoGlyph(status string) string {
	s := m.styles
	switch status {
	case "completed":
		return lipgloss.NewStyle().Foreground(s.P.Green).Render("✓")
	case "in_progress":
		return lipgloss.NewStyle().Foreground(s.P.Amber).Render("◐")
	case "cancelled":
		return lipgloss.NewStyle().Foreground(s.P.Red).Render("✗")
	default: // pending
		return s.Faint.Render("○")
	}
}

// tasksDockView renders the bottom dock (empty string when closed).
func (m Model) tasksDockView(width int) string {
	if !m.tasksOpen {
		return ""
	}
	s := m.styles
	done := 0
	for _, td := range m.todos {
		if td.Status == "completed" {
			done++
		}
	}

	var lines []string
	lines = append(lines, s.Section.Render("Tasks")+s.Faint.Render(" "+itoa(done)+"/"+itoa(len(m.todos))))
	if len(m.todos) == 0 {
		lines = append(lines, s.Faint.Render("(no tasks)"))
	}
	todos := m.sortedTodos()
	for i, td := range todos {
		if i >= maxDockRows {
			lines = append(lines, s.Faint.Render("  … "+itoa(len(todos)-maxDockRows)+" more"))
			break
		}
		content := truncate(td.Content, width-3) // truncate plain text before styling
		var text string
		if td.Status == "completed" {
			text = lipgloss.NewStyle().Foreground(s.P.FgFaint).Strikethrough(true).Render(content)
		} else {
			text = s.Base.Render(content)
		}
		lines = append(lines, m.todoGlyph(td.Status)+" "+text)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false). // top rule only (no side columns)
		BorderForeground(s.P.BorderSoft).
		Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// isTodoWriteEvent reports whether an SSE event is a todowrite tool part update
// (the signal to refetch todos, since there's no todo event). Decodes the typed
// part rather than substring-matching the raw JSON.
func isTodoWriteEvent(ev opcode42client.SSEEvent) bool {
	if ev.Type != "message.part.updated" {
		return false
	}
	var p struct {
		Part Part `json:"part"`
	}
	return decode(ev.Properties, &p) && p.Part.Type == "tool" && p.Part.Tool == "todowrite"
}
