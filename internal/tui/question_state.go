package tui

// question_state.go — Plan 17 §B5: pure state machine for the question footer
// panel, mirroring opencode's question.shared.ts.
//
// opencode separates state (question.shared.ts) from render
// (footer.question.tsx): a pure state machine + a render component. The
// machine supports both single-question and multi-question flows:
//
//	single: options list with up/down selection, digit shortcuts, and
//	        optional custom text input. Submitting an option immediately
//	        replies (question.shared.ts:180-188 questionPick).
//	multi:  tabbed interface where each question is a tab, plus a final
//	        "Confirm" tab that shows all answers for review. tab/shift-tab
//	        or left/right to navigate between questions
//	        (question.shared.ts:84-103, 109-116).
//
// State transitions:
//   - questionSelect → picks an option (single: submits, multi: toggles/advances)
//   - questionSave   → saves custom text input
//   - questionMove   → arrow key navigation through options
//   - questionSetTab → tab navigation between questions
//   - questionSubmit → builds the final QuestionReply with all answers
//
// Custom answers: when a question has custom=true, an extra "Type your own
// answer" option appears (question.shared.ts:70-72). Selecting it enters
// editing mode with a text field; enter saves and submits (single) or
// advances (multi).

// questionBodyState is the per-request question UI state. Owned by the Model
// (m.qBody); reset on reply success/failure and when the active pending
// question changes. The pure transition functions below return a new state
// (no mutation), making the multi-question flow testable independently of the
// render path. Mirrors QuestionBodyState (question.shared.ts:19-27).
type questionBodyState struct {
	requestID string
	tab       int        // the active tab (0..len(questions)-1 = question, len = confirm)
	answers   [][]string // per-question selected labels (may be empty for un-answered)
	custom    []string   // per-question custom-text input (the typed value)
	selected  int        // option cursor within the active tab's question
	editing   bool       // the custom-text textarea is focused
	replying  bool       // a reply/reject is in flight
	// rejecting distinguishes the in-flight action: true when the user
	// pressed esc (reject), false when the user pressed enter on the
	// confirm tab (submit). Mirrors the prior m.qRejecting; the
	// answered-card path records Skipped=true on a reject.
	rejecting bool
}

// newQuestionBodyState returns the initial state for a question request
// (createQuestionBodyState, question.shared.ts:34-44).
func newQuestionBodyState(requestID string) questionBodyState {
	return questionBodyState{
		requestID: requestID,
		tab:       0,
	}
}

// questionSync resets the state when the request id changes
// (questionSync, question.shared.ts:46-52). Used by the render path so a
// new pending question re-initialises the state even when the Model is
// reused.
func questionSync(s questionBodyState, requestID string) questionBodyState {
	if s.requestID == requestID {
		return s
	}
	return newQuestionBodyState(requestID)
}

// questionSingle reports whether the request is a single-question flow
// (questionSingle, question.shared.ts:54-56): exactly one question and not
// multiple-select.
func questionSingle(q *Question) bool {
	return q != nil && len(q.Questions) == 1 && !q.Questions[0].Multiple
}

// questionTabs returns the total number of tabs: 1 for single (no tab bar),
// len(questions)+1 for multi (the extra tab is the Confirm review)
// (questionTabs, question.shared.ts:58-60).
func questionTabs(q *Question) int {
	if q == nil {
		return 0
	}
	if questionSingle(q) {
		return 1
	}
	return len(q.Questions) + 1
}

// questionConfirm reports whether the active tab is the Confirm review tab
// (questionConfirm, question.shared.ts:62-64). Only valid in multi-question
// flows.
func questionConfirm(q *Question, s questionBodyState) bool {
	return q != nil && !questionSingle(q) && s.tab == len(q.Questions)
}

// questionInfo returns the QuestionInfo for the active tab, or nil when the
// active tab is the Confirm review (questionInfo, question.shared.ts:66-68).
func questionInfo(q *Question, s questionBodyState) *QuestionInfo {
	if q == nil || s.tab < 0 || s.tab >= len(q.Questions) {
		return nil
	}
	return &q.Questions[s.tab]
}

// questionCustom reports whether the active question supports a custom-text
// answer (questionCustom, question.shared.ts:70-72). Defaults to true when the
// custom field is absent (opencode's info?.custom !== false check).
func questionCustom(q *Question, s questionBodyState) bool {
	info := questionInfo(q, s)
	return info == nil || info.Custom
}

// questionInput returns the custom-text content for the active tab
// (questionInput, question.shared.ts:74-76).
func questionInput(s questionBodyState) string {
	if s.tab < 0 || s.tab >= len(s.custom) {
		return ""
	}
	return s.custom[s.tab]
}

// questionPicked reports whether the custom-text answer is currently selected
// (questionPicked, question.shared.ts:78-85): the custom value is non-empty AND
// it's in the active tab's answers.
func questionPicked(s questionBodyState) bool {
	v := questionInput(s)
	if v == "" {
		return false
	}
	if s.tab >= 0 && s.tab < len(s.answers) {
		for _, a := range s.answers[s.tab] {
			if a == v {
				return true
			}
		}
	}
	return false
}

// questionOther reports whether the cursor is on the "Type your own answer"
// option (questionOther, question.shared.ts:87-94).
func questionOther(q *Question, s questionBodyState) bool {
	info := questionInfo(q, s)
	if info == nil || !info.Custom {
		return false
	}
	return s.selected == len(info.Options)
}

// questionTotal returns the total number of options including the custom-text
// option when supported (questionTotal, question.shared.ts:96-103).
func questionTotal(q *Question, s questionBodyState) int {
	info := questionInfo(q, s)
	if info == nil {
		return 0
	}
	n := len(info.Options)
	if questionCustom(q, s) {
		n++
	}
	return n
}

// questionSetTab sets the active tab, resetting the per-tab cursor and editing
// flag (questionSetTab, question.shared.ts:109-116).
func questionSetTab(s questionBodyState, tab int) questionBodyState {
	return questionBodyState{
		requestID: s.requestID,
		tab:       tab,
		answers:   s.answers,
		custom:    s.custom,
		selected:  0,
		editing:   false,
		replying:  s.replying,
		rejecting: s.rejecting,
	}
}

// questionSetSelected sets the cursor (questionSetSelected,
// question.shared.ts:118-123).
func questionSetSelected(s questionBodyState, sel int) questionBodyState {
	return questionBodyState{
		requestID: s.requestID,
		tab:       s.tab,
		answers:   s.answers,
		custom:    s.custom,
		selected:  sel,
		editing:   s.editing,
		replying:  s.replying,
		rejecting: s.rejecting,
	}
}

// questionSetEditing toggles the custom-text textarea focus
// (questionSetEditing, question.shared.ts:125-130).
func questionSetEditing(s questionBodyState, editing bool) questionBodyState {
	return questionBodyState{
		requestID: s.requestID,
		tab:       s.tab,
		answers:   s.answers,
		custom:    s.custom,
		selected:  s.selected,
		editing:   editing,
		replying:  s.replying,
		rejecting: s.rejecting,
	}
}

// questionSetReplying marks the reply as in flight, clearing on retry
// (questionSetSubmitting, question.shared.ts:132-137).
func questionSetReplying(s questionBodyState, replying, rejecting bool) questionBodyState {
	return questionBodyState{
		requestID: s.requestID,
		tab:       s.tab,
		answers:   s.answers,
		custom:    s.custom,
		selected:  s.selected,
		editing:   s.editing,
		replying:  replying,
		rejecting: rejecting,
	}
}

// questionStoreCustom updates the custom-text content for a tab
// (questionStoreCustom, question.shared.ts:148-155).
func questionStoreCustom(s questionBodyState, tab int, text string) questionBodyState {
	custom := append([]string(nil), s.custom...)
	for len(custom) <= tab {
		custom = append(custom, "")
	}
	custom[tab] = text
	return questionBodyState{
		requestID: s.requestID,
		tab:       s.tab,
		answers:   s.answers,
		custom:    custom,
		selected:  s.selected,
		editing:   s.editing,
		replying:  s.replying,
		rejecting: s.rejecting,
	}
}

// questionStoreAnswers sets the answers for a tab
// (storeAnswers, question.shared.ts:139-146).
func questionStoreAnswers(s questionBodyState, tab int, list []string) questionBodyState {
	answers := append([][]string(nil), s.answers...)
	for len(answers) <= tab {
		answers = append(answers, nil)
	}
	answers[tab] = append([]string(nil), list...)
	return questionBodyState{
		requestID: s.requestID,
		tab:       s.tab,
		answers:   answers,
		custom:    s.custom,
		selected:  s.selected,
		editing:   s.editing,
		replying:  s.replying,
		rejecting: s.rejecting,
	}
}

// questionMove moves the cursor by dir, wrapping within the active tab's total
// (questionMove, question.shared.ts:207-217).
func questionMove(s questionBodyState, q *Question, dir int) questionBodyState {
	total := questionTotal(q, s)
	if total == 0 {
		return s
	}
	next := (s.selected + dir + total) % total
	return questionSetSelected(s, next)
}

// questionToggle toggles a label in the active tab's multi-select answers
// (questionToggle, question.shared.ts:195-205).
func questionToggle(s questionBodyState, answer string) questionBodyState {
	var list []string
	if s.tab < len(s.answers) {
		list = append([]string(nil), s.answers[s.tab]...)
	}
	idx := -1
	for i, a := range list {
		if a == answer {
			idx = i
			break
		}
	}
	if idx == -1 {
		list = append(list, answer)
	} else {
		list = append(list[:idx], list[idx+1:]...)
	}
	return questionStoreAnswers(s, s.tab, list)
}

// questionPick picks an answer for the active tab: for single-question flows
// the reply is emitted immediately; for multi-question flows the tab advances
// (questionPick, question.shared.ts:157-193). The returned reply is nil for
// multi-question flows (the reply is sent from the Confirm tab).
func questionPick(s questionBodyState, q *Question, answer string, isCustom bool) (questionBodyState, [][]string) {
	answers := append([][]string(nil), s.answers...)
	for len(answers) <= s.tab {
		answers = append(answers, nil)
	}
	answers[s.tab] = []string{answer}
	next := questionBodyState{
		requestID: s.requestID,
		tab:       s.tab,
		answers:   answers,
		custom:    s.custom,
		selected:  s.selected,
		editing:   false,
		replying:  s.replying,
		rejecting: s.rejecting,
	}
	if isCustom {
		custom := append([]string(nil), s.custom...)
		for len(custom) <= s.tab {
			custom = append(custom, "")
		}
		custom[s.tab] = answer
		next.custom = custom
	}
	if questionSingle(q) {
		return next, [][]string{{answer}}
	}
	// Multi-question: advance to the next tab (wrapping past Confirm so the
	// user lands on the next question, then Confirm).
	tabs := questionTabs(q)
	next = questionSetTab(next, (s.tab+1)%tabs)
	return next, nil
}

// questionSelect picks the option at the current cursor (questionSelect,
// question.shared.ts:219-256). For single-select it calls questionPick (which
// replies immediately for single-question flows); for multi-select it toggles;
// for the custom-text option it enters editing mode.
func questionSelect(s questionBodyState, q *Question) (questionBodyState, [][]string) {
	info := questionInfo(q, s)
	if info == nil {
		return s, nil
	}
	if questionOther(q, s) {
		if !info.Multiple {
			return questionSetEditing(s, true), nil
		}
		if v := questionInput(s); v != "" && questionPicked(s) {
			return questionToggle(s, v), nil
		}
		return questionSetEditing(s, true), nil
	}
	if s.selected < 0 || s.selected >= len(info.Options) {
		return s, nil
	}
	if info.Multiple {
		return questionToggle(s, info.Options[s.selected].Label), nil
	}
	return questionPick(s, q, info.Options[s.selected].Label, false)
}

// questionSave saves the custom-text input (questionSave,
// question.shared.ts:258-306). For single-select it calls questionPick with
// the custom value (replying immediately for single-question flows); for
// multi-select it adds/removes the value from the answers and exits editing
// mode. An empty save cancels the custom selection.
func questionSave(s questionBodyState, q *Question) (questionBodyState, [][]string) {
	info := questionInfo(q, s)
	if info == nil {
		return s, nil
	}
	value := ""
	if s.tab < len(s.custom) {
		value = s.custom[s.tab]
	}
	value = trimSpace(value)
	prev := ""
	if s.tab < len(s.custom) {
		prev = s.custom[s.tab]
	}
	if value == "" {
		if prev == "" {
			return questionSetEditing(s, false), nil
		}
		next := questionStoreCustom(s, s.tab, "")
		var list []string
		if s.tab < len(s.answers) {
			list = append([]string(nil), s.answers[s.tab]...)
		}
		filtered := list[:0]
		for _, a := range list {
			if a != prev {
				filtered = append(filtered, a)
			}
		}
		next = questionStoreAnswers(next, s.tab, filtered)
		return questionSetEditing(next, false), nil
	}
	if info.Multiple {
		var answers []string
		if s.tab < len(s.answers) {
			answers = append([]string(nil), s.answers[s.tab]...)
		}
		if prev != "" {
			for i, a := range answers {
				if a == prev {
					answers = append(answers[:i], answers[i+1:]...)
					break
				}
			}
		}
		found := false
		for _, a := range answers {
			if a == value {
				found = true
				break
			}
		}
		if !found {
			answers = append(answers, value)
		}
		next := questionStoreCustom(s, s.tab, value)
		next = questionStoreAnswers(next, s.tab, answers)
		return questionSetEditing(next, false), nil
	}
	return questionPick(s, q, value, true)
}

// questionSubmit builds the final reply with all answers, sent from the
// Confirm tab (questionSubmit, question.shared.ts:308-313).
func questionSubmit(q *Question, s questionBodyState) [][]string {
	if q == nil {
		return nil
	}
	out := make([][]string, len(q.Questions))
	for i := range q.Questions {
		if i < len(s.answers) {
			out[i] = append([]string(nil), s.answers[i]...)
		} else {
			out[i] = []string{}
		}
	}
	return out
}

// questionHint returns the bottom hint line (questionHint,
// question.shared.ts:321-339).
func questionHint(q *Question, s questionBodyState) string {
	if s.replying {
		return "Waiting for question event..."
	}
	if questionConfirm(q, s) {
		return "enter submit   esc dismiss"
	}
	if s.editing {
		return "enter save   esc cancel"
	}
	info := questionInfo(q, s)
	if questionSingle(q) {
		verb := "submit"
		if info != nil && info.Multiple {
			verb = "toggle"
		}
		return "↑↓ select   enter " + verb + "   esc dismiss"
	}
	verb := "confirm"
	if info != nil && info.Multiple {
		verb = "toggle"
	}
	return "⇆ tab   ↑↓ select   enter " + verb + "   esc dismiss"
}

// trimSpace is a strings.TrimSpace wrapper kept local so the state machine
// has no imports (pure).
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
