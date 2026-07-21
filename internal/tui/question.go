package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// question.go — Plan 17 §B: the question footer panel.
//
// opencode renders questions as a footer-region panel (bottom of screen),
// NOT a centered modal (run/footer.view.tsx:787-794). The panel body carries
// the theme's surface background (BgElev, Opcode42's equivalent of opencode's
// `surface`); the outer footer container is transparent when a panel is
// active (footer.view.tsx:663) and the left accent border is removed
// (footer.view.tsx:645).
//
// Two flows (question.shared.ts):
//
//	single: one non-multiple question. Submitting an option immediately
//	        replies (questionPick).
//	multi:  multiple questions OR a multiple-select question. Each question
//	        is a tab, plus a final "Confirm" tab that shows all answers for
//	        review. tab/shift-tab or left/right to navigate between
//	        questions (question.shared.ts:84-103, 109-116).
//
// Custom text (question.shared.ts:70-72): when a question has custom=true,
// an extra "Type your own answer" option appears. Selecting it enters
// editing mode with a text field; enter saves and submits (single) or
// advances (multi).
//
// Keyboard (footer.question.tsx:167-248):
//   - up / k, down / j — move selection
//   - 1-9 digit shortcuts — directly choose option N (footer.question.tsx:217-224)
//   - return / enter — select/confirm/submit (verb changes per verb())
//   - esc — reject/dismiss
//   - tab / shift+tab — tab navigation (multi-question only)
//   - in editing mode: enter saves, esc cancels (footer.question.tsx:174-182)
//
// No `space` for multi-select toggle — opencode uses `enter` to toggle
// (footer.question.tsx:238-242). This replaces Opcode42's prior `space`
// toggle (plan 17 §B2).

// pendingQuestion is the question request currently being answered (oldest), or
// nil when there is none.
func (m Model) pendingQuestion() *Question {
	if len(m.store.questions) == 0 {
		return nil
	}
	return &m.store.questions[0]
}

// questionRepliedMsg is the result of replying/rejecting a question.
type questionRepliedMsg struct {
	id  string
	err error
}

func replyQuestionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string, answers [][]string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/question/"+id+"/reply", map[string]any{"answers": answers}, nil)
		return questionRepliedMsg{id: id, err: err}
	}
}

func rejectQuestionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/question/"+id+"/reject", nil, nil)
		return questionRepliedMsg{id: id, err: err}
	}
}

// handleQuestionKey drives the footer panel. The state machine lives in
// m.qBody (question_state.go); the key path applies pure transitions and
// dispatches the reply when the Confirm tab is submitted or a single-question
// option is picked.
func (m Model) handleQuestionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	q := m.pendingQuestion()
	if q == nil || m.qBody.replying {
		return m, nil
	}
	// Reset the state when the active pending question changed (mirrors the
	// prior resetQuestion, but driven by the request id).
	m.qBody = questionSync(m.qBody, q.ID)

	// Editing mode: the custom-text textarea owns the keys. enter saves, esc
	// cancels (footer.question.tsx:174-182). Printable chars append to the
	// custom text.
	if m.qBody.editing {
		return m.handleQuestionEditingKey(msg, q)
	}

	// Multi-question tab navigation (footer.question.tsx:184-201): tab /
	// shift+tab / left / h / right / l. Single-question flows ignore these
	// (questionSingle → 1 tab, no tab bar). The Confirm tab also supports
	// tab navigation (so the user can move back to a question to revise).
	if !questionSingle(q) {
		switch msg.String() {
		case "tab":
			tabs := questionTabs(q)
			m.qBody = questionSetTab(m.qBody, (m.qBody.tab+1)%tabs)
			return m, nil
		case "shift+tab":
			tabs := questionTabs(q)
			m.qBody = questionSetTab(m.qBody, (m.qBody.tab-1+tabs)%tabs)
			return m, nil
		case "left", "h":
			tabs := questionTabs(q)
			m.qBody = questionSetTab(m.qBody, (m.qBody.tab-1+tabs)%tabs)
			return m, nil
		case "right", "l":
			tabs := questionTabs(q)
			m.qBody = questionSetTab(m.qBody, (m.qBody.tab+1)%tabs)
			return m, nil
		}
	}

	// Confirm tab: enter submits, esc rejects (footer.question.tsx:203-215).
	if questionConfirm(q, m.qBody) {
		switch msg.String() {
		case "enter":
			answers := questionSubmit(q, m.qBody)
			m.qBody = questionSetReplying(m.qBody, true, false)
			return m, replyQuestionCmd(m.ctx, m.client, q.ID, answers)
		case "esc":
			m.qBody = questionSetReplying(m.qBody, true, true)
			return m, rejectQuestionCmd(m.ctx, m.client, q.ID)
		}
		return m, nil
	}

	// Digit shortcuts 1-9 (footer.question.tsx:217-224): directly choose
	// option N.
	if d, ok := digitKey(msg); ok {
		total := questionTotal(q, m.qBody)
		maxOpt := total
		if maxOpt > 9 {
			maxOpt = 9
		}
		if d >= 1 && d <= maxOpt {
			return m.questionChoose(q, d-1)
		}
	}

	switch msg.String() {
	case "up", "k":
		m.qBody = questionMove(m.qBody, q, -1)
		return m, nil
	case "down", "j":
		m.qBody = questionMove(m.qBody, q, +1)
		return m, nil
	case "enter":
		return m.questionSelectCurrent(q)
	case "esc":
		m.qBody = questionSetReplying(m.qBody, true, true)
		return m, rejectQuestionCmd(m.ctx, m.client, q.ID)
	}
	return m, nil
}

// handleQuestionEditingKey handles keys while the custom-text textarea is
// focused. enter saves, esc cancels (footer.question.tsx:174-182). Printable
// chars append; backspace deletes the last rune.
func (m Model) handleQuestionEditingKey(msg tea.KeyMsg, q *Question) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		next, answers := questionSave(m.qBody, q)
		m.qBody = next
		if answers != nil {
			// Single-question flow: the save emitted a reply.
			m.qBody = questionSetReplying(m.qBody, true, false)
			return m, replyQuestionCmd(m.ctx, m.client, q.ID, answers)
		}
		return m, nil
	case "esc":
		m.qBody = questionSetEditing(m.qBody, false)
		return m, nil
	case "backspace", "backspace2":
		cur := questionInput(m.qBody)
		if cur != "" {
			r := []rune(cur)
			m.qBody = questionStoreCustom(m.qBody, m.qBody.tab, string(r[:len(r)-1]))
		}
		return m, nil
	}
	k := msg.Key()
	if k.Text != "" && k.Code >= 32 {
		m.qBody = questionStoreCustom(m.qBody, m.qBody.tab, questionInput(m.qBody)+k.Text)
	}
	return m, nil
}

// questionChoose picks the option at index sel (the digit-shortcut path). For
// single-select it calls questionPick (which replies immediately for
// single-question flows); for multi-select it toggles. For the custom-text
// option it enters editing mode.
func (m Model) questionChoose(q *Question, sel int) (tea.Model, tea.Cmd) {
	m.qBody = questionSetSelected(m.qBody, sel)
	return m.questionSelectCurrent(q)
}

// questionSelectCurrent picks the option at the current cursor.
func (m Model) questionSelectCurrent(q *Question) (tea.Model, tea.Cmd) {
	next, answers := questionSelect(m.qBody, q)
	m.qBody = next
	if answers != nil {
		// Single-question flow: the select emitted a reply.
		m.qBody = questionSetReplying(m.qBody, true, false)
		return m, replyQuestionCmd(m.ctx, m.client, q.ID, answers)
	}
	return m, nil
}

// digitKey returns the digit 1-9 a key represents, or 0,false for non-digits.
// bubbletea v2 stringifies digit keys as the digit character ("1".."9").
func digitKey(msg tea.KeyMsg) (int, bool) {
	k := msg.Key()
	if k.Text == "" {
		return 0, false
	}
	r := []rune(k.Text)
	if len(r) != 1 || r[0] < '1' || r[0] > '9' {
		return 0, false
	}
	return int(r[0] - '0'), true
}

// resetQuestion clears the per-request answer state (after a reply/reject lands
// or when the active question changes).
func (m Model) resetQuestion() Model {
	m.qBody = questionBodyState{}
	return m
}

// recordLocalAnsweredQuestion captures the finalized question + the locally
// selected labels into the store's answered-questions map (plan 08e §E4). Called
// from the questionRepliedMsg success path BEFORE the pending slice + per-
// request state are cleared, so the question text + answers are still
// available. The local path knows the specific labels (from qBody.answers);
// the SSE path (store.Reduce) records a label-less "Answered" fallback for
// replies that originated elsewhere. Deduped + upgraded by id inside
// recordAnsweredQuestion (a later local reply with labels wins over an earlier
// SSE label-less entry).
//
// Relies on the SSE-clear deferral in the sseEventMsg handler: when a local
// reply is in flight (qBody.replying), the SSE question.replied/rejected event
// for our own question is deferred until AFTER this runs, so the pending
// question is still in the store here.
func (m Model) recordLocalAnsweredQuestion(id string) Model {
	var q *Question
	for i := range m.store.questions {
		if m.store.questions[i].ID == id {
			q = &m.store.questions[i]
			break
		}
	}
	if q == nil {
		return m
	}
	var answers [][]string
	var skipped bool
	if m.qBody.rejecting {
		skipped = true
	} else {
		answers = questionSubmit(q, m.qBody)
	}
	m.store = m.store.recordAnsweredQuestion(AnsweredQuestion{
		ID:        q.ID,
		SessionID: q.SessionID,
		Skipped:   skipped,
		Answers:   answers,
		Questions: append([]QuestionInfo(nil), q.Questions...),
	})
	return m
}

// answeredQuestionCardView renders a finalized question as a collapsed in-stream
// card (plan 08e §E4): the header + question text + the selected labels (or
// "Skipped"). Stays in the scrollback so the question is visible in history,
// not just a transient modal. Blue accent matches the pending card.
func (m Model) answeredQuestionCardView(aq AnsweredQuestion) string {
	s := m.styles
	cw := m.contentWidth()
	width := 64
	if width > cw {
		width = cw
	}
	var lines []string
	for i, info := range aq.Questions {
		var state string
		if aq.Skipped {
			state = "Skipped"
		} else if i < len(aq.Answers) {
			state = strings.Join(aq.Answers[i], ", ")
		}
		if state == "" {
			state = "Answered"
		}
		header := info.Header
		if header == "" {
			header = "Question"
		}
		lines = append(lines,
			lipgloss.NewStyle().Foreground(s.P.Blue).Bold(true).Render(truncate(header, width-2)),
			s.Base.Render(truncate(info.Question, width-2)),
			s.Faint.Render("↳ "+state),
		)
		if i < len(aq.Questions)-1 {
			lines = append(lines, "")
		}
	}
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.P.Blue).
		Padding(0, 1).
		Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return card
}

// questionView renders the footer panel (plan 17 §B1): the panel body, sized
// leftW × panelH, styled with BgElev background. The panel is positioned at
// the bottom of the screen by the canvas (overlayLayers). The multi-question
// tab bar + the Confirm review tab render when the request has more than one
// question (or a multiple-select question).
func (m Model) questionView() string {
	q := m.pendingQuestion()
	if q == nil {
		return ""
	}
	// Reset the state when the active pending question changed (mirrors
	// handleQuestionKey's sync, for the render path).
	m.qBody = questionSync(m.qBody, q.ID)
	s := m.styles
	leftW := m.leftColumnWidth()
	if leftW < 1 {
		leftW = 1
	}
	innerW := leftW - 2 // Padding(0,1) → 1 col each side
	if innerW < 1 {
		innerW = 1
	}

	var lines []string
	lines = append(lines, "")

	// Multi-question tab bar (footer.question.tsx:282-315): one tab per
	// question + a final "Confirm" tab. Single-question flows have no tab
	// bar (questionSingle → 1 tab).
	if !questionSingle(q) {
		lines = append(lines, m.questionTabBar(q, innerW))
		lines = append(lines, "")
	}

	if questionConfirm(q, m.qBody) {
		lines = append(lines, m.questionConfirmReview(q, innerW)...)
	} else {
		lines = append(lines, m.questionQuestionLines(q, innerW)...)
	}

	hint := questionHint(q, m.qBody)
	lines = append(lines, "")
	lines = append(lines, s.Faint.Render(hint))

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	panel := s.Surface(s.P.BgElev).Width(leftW).Render(body)
	// Pad to the target height (plan 17 §B1): panelH = base + QUESTION_ROWS.
	// opencode's renderer reserves a fixed height regardless of content
	// (footer.ts:697-722); Opcode42 pads the body to the same height so
	// the panel is a stable footer region (not content-sized).
	if h := m.questionPanelHeight(); h > lipgloss.Height(panel) {
		panel = padVertical(panel, h, s.P.BgElev)
	}
	return panel
}

// questionTabBar renders the multi-question tab headers + the final Confirm
// tab (footer.question.tsx:282-315). The active tab carries the highlight bg;
// answered tabs carry the primary text color, unanswered the muted color.
func (m Model) questionTabBar(q *Question, innerW int) string {
	s := m.styles
	cells := make([]string, 0, len(q.Questions)+1)
	for i, info := range q.Questions {
		label := info.Header
		if label == "" {
			label = "Q" + humanInt(i+1)
		}
		answered := false
		if i < len(m.qBody.answers) && len(m.qBody.answers[i]) > 0 {
			answered = true
		}
		if i == m.qBody.tab {
			cells = append(cells, s.Selection.Render(" "+label+" "))
		} else if answered {
			cells = append(cells, s.Base.Render(" "+label+" "))
		} else {
			cells = append(cells, s.Faint.Render(" "+label+" "))
		}
	}
	// Confirm tab (footer.question.tsx:304-313).
	confirmTab := len(q.Questions)
	if confirmTab == m.qBody.tab {
		cells = append(cells, s.Selection.Render(" Confirm "))
	} else {
		cells = append(cells, s.Faint.Render(" Confirm "))
	}
	row := strings.Join(cells, " ")
	return s.Surface(s.P.BgElev).Width(innerW).Render(row)
}

// questionQuestionLines renders the active question's prompt + options
// (footer.question.tsx:356-506). The header is shown as a bold title (the
// multi-question tab bar shows it in the tab; the single-question flow has
// no tab bar so the title carries it).
func (m Model) questionQuestionLines(q *Question, innerW int) []string {
	s := m.styles
	info := questionInfo(q, m.qBody)
	if info == nil {
		return nil
	}
	var lines []string
	// Header title (footer.question.tsx:358-362 shows the question text;
	// Opcode42 also renders the header as a bold title for visual continuity
	// with the prior centered card and the multi-question tab bar).
	if info.Header != "" {
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(s.P.Blue).Bold(true).Render(truncate(info.Header, innerW)))
		lines = append(lines, "")
	}
	// Prompt (footer.question.tsx:358-362): the question text + a
	// "(select all that apply)" suffix for multi-select.
	prompt := info.Question
	if info.Multiple {
		prompt += " (select all that apply)"
	}
	lines = append(lines, " "+s.Base.Render(truncate(prompt, innerW)))
	lines = append(lines, "")

	// Options (footer.question.tsx:376-423): one row per option, with a
	// 1-based index prefix, the label, and a [✓] marker for multi-select
	// hits. The active row carries the highlight bg.
	for i, opt := range info.Options {
		lines = append(lines, m.questionOptionRow(q, info, i, opt, innerW))
	}

	// Custom-text option (footer.question.tsx:425-506): when the question
	// supports custom answers, an extra "Type your own answer" row appears.
	if questionCustom(q, m.qBody) {
		lines = append(lines, m.questionCustomRow(q, info, innerW))
	}

	// When editing, render the textarea below the custom row
	// (footer.question.tsx:476-503).
	if m.qBody.editing && questionOther(q, m.qBody) {
		lines = append(lines, m.questionEditingField(innerW))
	}
	return lines
}

// questionOptionRow renders one option row (footer.question.tsx:380-414).
func (m Model) questionOptionRow(_ *Question, info *QuestionInfo, i int, opt QuestionOption, innerW int) string {
	s := m.styles
	hit := false
	if m.qBody.tab < len(m.qBody.answers) {
		for _, a := range m.qBody.answers[m.qBody.tab] {
			if a == opt.Label {
				hit = true
				break
			}
		}
	}
	idx := humanInt(i + 1)
	var row string
	if info.Multiple {
		mark := "[ ] "
		if hit {
			mark = "[✓] "
		}
		row = idx + ". " + mark + opt.Label
	} else {
		row = idx + ". " + opt.Label
		if hit {
			row += " ✓"
		}
	}
	if i == m.qBody.selected {
		return " " + s.Selection.Width(innerW).Render(truncate(row, innerW))
	}
	return " " + s.Base.Render(truncate(row, innerW))
}

// questionCustomRow renders the "Type your own answer" row
// (footer.question.tsx:425-463).
func (m Model) questionCustomRow(q *Question, info *QuestionInfo, innerW int) string {
	s := m.styles
	idx := humanInt(len(info.Options) + 1)
	picked := questionPicked(m.qBody)
	var row string
	if info.Multiple {
		mark := "[ ] "
		if picked {
			mark = "[✓] "
		}
		row = idx + ". " + mark + "Type your own answer"
	} else {
		row = idx + ". Type your own answer"
		if picked {
			row += " ✓"
		}
	}
	sel := questionOther(q, m.qBody)
	if sel {
		return " " + s.Selection.Width(innerW).Render(truncate(row, innerW))
	}
	return " " + s.Base.Render(truncate(row, innerW))
}

// questionEditingField renders the custom-text textarea
// (footer.question.tsx:476-503). Opcode42 uses a single-line input rendered
// as a BgPanel-filled row with the current text (or a placeholder).
func (m Model) questionEditingField(innerW int) string {
	s := m.styles
	text := questionInput(m.qBody)
	style := s.Surface(s.P.BgPanel).Width(innerW)
	if text == "" {
		return " " + style.Foreground(s.P.FgGhost).Render("Type your own answer")
	}
	return " " + style.Foreground(s.P.Fg).Render(truncate(text, innerW))
}

// questionConfirmReview renders the Confirm tab's review summary
// (footer.question.tsx:319-354): one row per question showing the selected
// labels (or "(not answered)").
func (m Model) questionConfirmReview(q *Question, _ int) []string {
	s := m.styles
	var lines []string
	lines = append(lines, " "+s.Base.Render("Review"))
	for i, info := range q.Questions {
		var value string
		if i < len(m.qBody.answers) {
			value = strings.Join(m.qBody.answers[i], ", ")
		}
		answered := value != ""
		var row string
		header := info.Header
		if header == "" {
			header = "Q" + humanInt(i+1)
		}
		if answered {
			row = s.Faint.Render(header+": ") + s.Base.Render(value)
		} else {
			row = s.Faint.Render(header+": ") + lipgloss.NewStyle().Foreground(s.P.Red).Render("(not answered)")
		}
		lines = append(lines, " "+row)
	}
	return lines
}

// questionPanelHeight returns the panel height for the question footer panel
// (plan 17 §B1): base + QUESTION_ROWS, where base is the non-textarea chrome
// height (the status bar's rendered height). Mirrors opencode's applyHeight
// (footer.ts:697-722) with QUESTION_ROWS=14.
func (m Model) questionPanelHeight() int {
	base := lipgloss.Height(m.statusBarView(m.leftColumnWidth()))
	if base < 1 {
		base = 1
	}
	return base + 14
}

// questionID is the id of a (possibly nil) question request.
func questionID(q *Question) string {
	if q == nil {
		return ""
	}
	return q.ID
}

// itoa formats n with thousands separators (1234 → "1,234"), matching the
// prior question.go helper kept for tasks.go's "N/M" + "… N more" lines.
func itoa(n int) string { return humanInt(n) }
