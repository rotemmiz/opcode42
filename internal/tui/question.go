package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	forgeclient "github.com/rotemmiz/forge/sdk/go"
)

// U10 — question overlay. A `question.asked` SSE event yields a pending Question
// (one or more questions, each with options). The TUI blocks, stepping through
// the questions, and replies via POST /question/:id/reply {answers} — answers is
// one array of selected option labels per question (single-select → one label,
// multi-select → many). esc/r rejects via POST /question/:id/reject. The daemon's
// question.replied/rejected events clear it.

// pendingQuestion is the question request currently being answered (oldest), or
// nil when there is none.
func (m Model) pendingQuestion() *Question {
	if len(m.store.questions) == 0 {
		return nil
	}
	return &m.store.questions[0]
}

// curQuestion is the QuestionInfo for the step the user is on (nil if out of range).
func (m Model) curQuestion() *QuestionInfo {
	q := m.pendingQuestion()
	if q == nil || m.qIdx >= len(q.Questions) {
		return nil
	}
	return &q.Questions[m.qIdx]
}

// questionRepliedMsg is the result of replying/rejecting a question.
type questionRepliedMsg struct {
	id  string
	err error
}

func replyQuestionCmd(ctx context.Context, c *forgeclient.ForgeClient, id string, answers [][]string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/question/"+id+"/reply", map[string]any{"answers": answers}, nil)
		return questionRepliedMsg{id: id, err: err}
	}
}

func rejectQuestionCmd(ctx context.Context, c *forgeclient.ForgeClient, id string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/question/"+id+"/reject", nil, nil)
		return questionRepliedMsg{id: id, err: err}
	}
}

// handleQuestionKey drives the blocking overlay: ↑/↓ move, space toggles a
// multi-select option, enter confirms the step (advancing or replying), esc/r
// rejects the whole request.
func (m Model) handleQuestionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	q := m.pendingQuestion()
	info := m.curQuestion()
	if q == nil || info == nil || m.qReplying {
		return m, nil
	}
	m = m.ensureChecked(len(info.Options))

	switch msg.String() {
	case "up", "k":
		if m.qSel > 0 {
			m.qSel--
		}
		return m, nil
	case "down", "j":
		if m.qSel < len(info.Options)-1 {
			m.qSel++
		}
		return m, nil
	case " ":
		if info.Multiple && m.qSel < len(m.qChecked) {
			m.qChecked[m.qSel] = !m.qChecked[m.qSel]
		}
		return m, nil
	case "r", "esc":
		m.qReplying = true
		return m, rejectQuestionCmd(m.ctx, m.client, q.ID)
	case "enter":
		if len(info.Options) == 0 {
			return m, nil // free-text-only question: can't answer here, only reject (r)
		}
		ans := m.stepAnswer(info)
		if m.qIdx+1 < len(q.Questions) { // advance: record this step durably
			m.qAnswers = append(m.qAnswers, ans)
			m.qIdx++
			m.qSel = 0
			m.qChecked = nil
			return m, nil
		}
		// Final step: build prior steps + this one WITHOUT mutating qAnswers, so a
		// failed reply can be retried without double-appending the last answer.
		m.qReplying = true
		return m, replyQuestionCmd(m.ctx, m.client, q.ID, m.finalAnswers(info))
	}
	return m, nil
}

// finalAnswers is the full answer array submitted at the last step: the durably
// recorded prior steps plus the current step's selection (qAnswers is left
// untouched so a failed reply retries cleanly).
func (m Model) finalAnswers(info *QuestionInfo) [][]string {
	return append(append([][]string{}, m.qAnswers...), m.stepAnswer(info))
}

// ensureChecked sizes the multi-select toggle slice to the current options.
func (m Model) ensureChecked(n int) Model {
	if len(m.qChecked) != n {
		m.qChecked = make([]bool, n)
	}
	return m
}

// stepAnswer is the selected option label(s) for the current question.
func (m Model) stepAnswer(info *QuestionInfo) []string {
	if info.Multiple {
		var sel []string
		for i, on := range m.qChecked {
			if on && i < len(info.Options) {
				sel = append(sel, info.Options[i].Label)
			}
		}
		return sel
	}
	if m.qSel < len(info.Options) {
		return []string{info.Options[m.qSel].Label}
	}
	return nil
}

// resetQuestion clears the per-request answer state (after a reply/reject lands).
func (m Model) resetQuestion() Model {
	m.qIdx, m.qSel, m.qChecked, m.qAnswers, m.qReplying = 0, 0, nil, nil, false
	return m
}

// questionView renders the blocking overlay for the current step.
func (m Model) questionView() string {
	q := m.pendingQuestion()
	info := m.curQuestion()
	if q == nil || info == nil {
		return ""
	}
	s := m.styles
	width := 64
	m = m.ensureChecked(len(info.Options))

	title := info.Header
	if title == "" {
		title = "Question"
	}
	var lines []string
	lines = append(lines, lipgloss.NewStyle().Foreground(s.P.Blue).Bold(true).Render(truncate(title, width-2)))
	if len(q.Questions) > 1 {
		lines = append(lines, s.Faint.Render(stepLabel(m.qIdx+1, len(q.Questions))))
	}
	lines = append(lines, s.Base.Render(truncate(info.Question, width-2)), "")

	for i, opt := range info.Options {
		mark := "  "
		if info.Multiple {
			mark = "[ ] "
			if i < len(m.qChecked) && m.qChecked[i] {
				mark = "[x] "
			}
		}
		row := mark + opt.Label
		if i == m.qSel {
			lines = append(lines, s.Selection.Width(width-2).Render(" "+row))
		} else {
			lines = append(lines, s.Base.Render("  "+row))
		}
	}
	if len(info.Options) == 0 { // free-text-only question: not answerable here
		lines = append(lines, s.Faint.Render("(free-text answers aren't supported — press r to reject)"))
	}

	hint := "↑↓ move · enter select · r reject"
	switch {
	case len(info.Options) == 0:
		hint = "r reject · esc reject"
	case info.Multiple:
		hint = "↑↓ move · space toggle · enter confirm · r reject"
	}
	if m.qReplying {
		hint = "sending…"
	}
	lines = append(lines, "", s.Faint.Render(hint))

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.P.Blue).
		Background(s.P.BgElev).
		Padding(1, 2).
		Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))

	if m.width == 0 || m.height == 0 {
		return card
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

func stepLabel(n, total int) string { return "question " + itoa(n) + " of " + itoa(total) }

func itoa(n int) string { return humanInt(n) }

// questionID is the id of a (possibly nil) question request.
func questionID(q *Question) string {
	if q == nil {
		return ""
	}
	return q.ID
}
