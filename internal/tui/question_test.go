package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

func questionEvent(t *testing.T, id string, qs []QuestionInfo) opcode42client.SSEEvent {
	t.Helper()
	props, _ := json.Marshal(map[string]any{"id": id, "sessionID": "ses_1", "questions": qs})
	return opcode42client.SSEEvent{Type: "question.asked", Properties: props}
}

func opt(label string) QuestionOption { return QuestionOption{Label: label} }

func TestQuestion_AskedThenRepliedReduces(t *testing.T) {
	s := newStore()
	s = s.Reduce(questionEvent(t, "qst_1", []QuestionInfo{{Question: "pick", Options: []QuestionOption{opt("a")}}}))
	if len(s.questions) != 1 {
		t.Fatalf("question.asked should add a pending question: %+v", s.questions)
	}
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	s = s.Reduce(opcode42client.SSEEvent{Type: "question.rejected", Properties: props})
	if len(s.questions) != 0 {
		t.Fatalf("question.rejected should clear it, got %+v", s.questions)
	}
}

// TestQuestion_SingleSelectReplies verifies a single-select (one non-multiple
// question) replies immediately on enter (questionPick → reply). Plan 17 §B5.
func TestQuestion_SingleSelectReplies(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Header: "Color", Question: "Pick a color", Options: []QuestionOption{opt("red"), opt("green"), opt("blue")}},
	}))
	if m.pendingQuestion() == nil {
		t.Fatal("question should be pending")
	}
	view := m.renderView()
	for _, want := range []string{"Color", "Pick a color", "red", "green", "blue"} {
		if !strings.Contains(view, want) {
			t.Fatalf("overlay missing %q", want)
		}
	}
	// The default cursor is at idx 0 ("red").
	if m.qBody.selected != 0 {
		t.Fatalf("default cursor should be 0 (red), got %d", m.qBody.selected)
	}
	// down → cursor at idx 1 ("green").
	m, _ = step(t, m, key("down"))
	if m.qBody.selected != 1 {
		t.Fatalf("down should move cursor to 1 (green), got %d", m.qBody.selected)
	}
	// down again → cursor at idx 2 ("blue").
	m, _ = step(t, m, key("down"))
	if m.qBody.selected != 2 {
		t.Fatalf("two downs should move cursor to 2 (blue), got %d", m.qBody.selected)
	}
	// Single-select: enter dispatches the reply immediately with the
	// selected option's label.
	next, cmd := step(t, m, key("enter"))
	if cmd == nil || !next.qBody.replying {
		t.Fatal("enter on a single-select question should dispatch a reply immediately")
	}
}

// TestQuestion_MultiSelectTogglesThenReplies verifies a multi-select question
// toggles on enter (NOT space, plan 17 §B2), then submits from the Confirm tab.
func TestQuestion_MultiSelectTogglesThenReplies(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Pick some", Multiple: true, Options: []QuestionOption{opt("x"), opt("y"), opt("z")}},
	}))
	// A single multi-select question is NOT questionSingle (multi → tab flow).
	if questionSingle(m.pendingQuestion()) {
		t.Fatal("a multiple-select question should NOT be single (it uses the multi tab flow)")
	}
	// space is NOT a toggle anymore (plan 17 §B2).
	m, _ = step(t, m, key("space"))
	if len(m.qBody.answers) != 0 {
		t.Fatal("space should NOT toggle (plan 17 §B2 — opencode uses enter to toggle)")
	}
	// enter toggles the selected option (x at idx 0).
	m, _ = step(t, m, key("enter"))
	if got := m.qBody.answers[0]; len(got) != 1 || got[0] != "x" {
		t.Fatalf("enter should toggle x on, got %+v", got)
	}
	// move to z (idx2) and toggle
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("enter"))
	if got := m.qBody.answers[0]; len(got) != 2 || got[0] != "x" || got[1] != "z" {
		t.Fatalf("toggles should be [x z], got %+v", got)
	}
	// tab to the Confirm tab and submit.
	m, _ = step(t, m, key("tab"))
	if !questionConfirm(m.pendingQuestion(), m.qBody) {
		t.Fatalf("tab should land on the Confirm tab, got tab=%d", m.qBody.tab)
	}
	next, cmd := step(t, m, key("enter"))
	if cmd == nil || !next.qBody.replying {
		t.Fatal("enter on the Confirm tab should dispatch the reply")
	}
	got := questionSubmit(m.pendingQuestion(), next.qBody)
	if len(got) != 1 || len(got[0]) != 2 || got[0][0] != "x" || got[0][1] != "z" {
		t.Fatalf("reply answers should be [[x z]], got %+v", got)
	}
}

// TestQuestion_DigitShortcuts verifies the 1-9 digit shortcuts directly
// choose an option (footer.question.tsx:217-224). Single-select → immediate
// reply; multi-select → toggle.
func TestQuestion_DigitShortcuts(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Header: "Color", Question: "Pick", Options: []QuestionOption{opt("red"), opt("green"), opt("blue")}},
	}))
	// "2" → green (single-select → immediate reply).
	next, cmd := step(t, m, key("2"))
	if cmd == nil || !next.qBody.replying {
		t.Fatal("digit 2 should pick green and dispatch a reply (single-select)")
	}
	got := questionSubmit(m.pendingQuestion(), next.qBody)
	if len(got) != 1 || got[0][0] != "green" {
		t.Fatalf("digit 2 should pick green, got %+v", got)
	}
}

// TestQuestion_MultiQuestionTabsThenReplies verifies the multi-question tab
// flow: each enter advances to the next tab, the Confirm tab submits all
// answers (plan 17 §B5).
func TestQuestion_MultiQuestionTabsThenReplies(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q1", Options: []QuestionOption{opt("a1"), opt("a2")}},
		{Question: "Q2", Options: []QuestionOption{opt("b1"), opt("b2")}},
	}))
	if questionSingle(m.pendingQuestion()) {
		t.Fatal("a 2-question request should NOT be single")
	}
	// answer Q1 = a1 (enter advances to the next tab, no reply yet)
	m, cmd := step(t, m, key("enter"))
	if cmd != nil {
		t.Fatal("enter on a non-Confirm tab should NOT reply (it advances)")
	}
	if m.qBody.tab != 1 {
		t.Fatalf("should advance to tab 1, got %d", m.qBody.tab)
	}
	// answer Q2 = b2 (down then enter → advance to Confirm)
	m, _ = step(t, m, key("down"))
	m, cmd = step(t, m, key("enter"))
	if cmd != nil {
		t.Fatal("enter on Q2 should advance to the Confirm tab, not reply")
	}
	if !questionConfirm(m.pendingQuestion(), m.qBody) {
		t.Fatalf("should land on the Confirm tab, got tab=%d", m.qBody.tab)
	}
	got := questionSubmit(m.pendingQuestion(), m.qBody)
	if len(got) != 2 || got[0][0] != "a1" || got[1][0] != "b2" {
		t.Fatalf("answers should be [[a1] [b2]], got %+v", got)
	}
	// enter on Confirm dispatches the reply.
	if _, cmd := step(t, m, key("enter")); cmd == nil {
		t.Fatal("enter on the Confirm tab should dispatch the reply")
	}
}

// TestQuestion_RejectAndResolve verifies esc rejects (NOT r — plan 17 §B2),
// the overlay stays until resolved.
func TestQuestion_RejectAndResolve(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{{Question: "Q", Options: []QuestionOption{opt("a")}}}))
	// r is NOT a reject shortcut anymore (plan 17 §B2 — opencode uses esc).
	_, cmd := m.handleQuestionKey(key("r"))
	if cmd != nil {
		t.Fatal("r should NOT reject (plan 17 §B2 — opencode uses esc)")
	}
	// esc rejects (dispatches), overlay stays until resolved.
	next, cmd := m.handleQuestionKey(key("esc"))
	if cmd == nil || !next.(Model).qBody.replying {
		t.Fatal("esc should dispatch a reject and mark replying")
	}
	// success clears it + resets state
	mOK, _ := step(t, next.(Model), questionRepliedMsg{id: "qst_1"})
	if mOK.pendingQuestion() != nil || mOK.qBody.replying || mOK.qBody.tab != 0 {
		t.Fatal("a resolved reject should clear the question and reset state")
	}
}

// TestQuestion_FailedReplyRetryNoDoubleAppend verifies a failed reply keeps
// the question so the user can retry, and the durable answers don't grow.
func TestQuestion_FailedReplyRetryNoDoubleAppend(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q1", Options: []QuestionOption{opt("a1"), opt("a2")}},
		{Question: "Q2", Options: []QuestionOption{opt("b1"), opt("b2")}},
	}))
	// Q1 = a1 → advance to Q2 tab
	m, _ = step(t, m, key("enter"))
	// Q2 = b2 (down) → advance to Confirm
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("enter"))
	if !questionConfirm(m.pendingQuestion(), m.qBody) {
		t.Fatal("should be on the Confirm tab")
	}
	// enter on Confirm → dispatches reply
	m, cmd := step(t, m, key("enter"))
	if cmd == nil || !m.qBody.replying {
		t.Fatal("enter on Confirm should reply")
	}
	// qBody.answers holds [a1, b2] (both durable).
	if got := m.qBody.answers; len(got) != 2 || got[0][0] != "a1" || got[1][0] != "b2" {
		t.Fatalf("answers should be [[a1] [b2]], got %+v", got)
	}
	// reply FAILS → state retained, replying cleared
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1", err: errTest})
	if m.pendingQuestion() == nil || m.qBody.replying {
		t.Fatal("a failed reply should keep the question and clear replying")
	}
	// retry enter, repeatedly: the answers must NOT grow (no double-append).
	for i := 0; i < 3; i++ {
		m, _ = step(t, m, questionRepliedMsg{id: "qst_1", err: errTest})
		m, cmd = step(t, m, key("enter"))
		if cmd == nil {
			t.Fatal("retry enter should re-dispatch")
		}
		if got := m.qBody.answers; len(got) != 2 || got[0][0] != "a1" || got[1][0] != "b2" {
			t.Fatalf("retry %d corrupted answers: %+v", i, got)
		}
	}
}

// TestQuestion_EmptyOptionsCanCustomText verifies a custom-only question
// (no options, custom=true) can be answered via the custom-text field
// (plan 17 §B5). The old behavior (free-text not supported) is gone.
func TestQuestion_EmptyOptionsCanCustomText(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Type your name", Custom: true, Options: nil},
	}))
	// The custom-text option is the only option (idx 0). enter enters
	// editing mode.
	m, _ = step(t, m, key("enter"))
	if !m.qBody.editing {
		t.Fatal("enter on the custom-text option should enter editing mode")
	}
	// Type "Alice".
	for _, r := range "Alice" {
		m, _ = step(t, m, key(string(r)))
	}
	if got := questionInput(m.qBody); got != "Alice" {
		t.Fatalf("custom text should be %q, got %q", "Alice", got)
	}
	// enter saves → single-question flow replies immediately.
	next, cmd := step(t, m, key("enter"))
	if cmd == nil || !next.qBody.replying {
		t.Fatal("enter in editing mode should save and dispatch the reply (single)")
	}
	got := questionSubmit(m.pendingQuestion(), next.qBody)
	if len(got) != 1 || got[0][0] != "Alice" {
		t.Fatalf("reply answers should be [[Alice]], got %+v", got)
	}
}

func TestQuestion_SSEClearResetsState(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q1", Options: []QuestionOption{opt("a1"), opt("a2")}},
		{Question: "Q2", Options: []QuestionOption{opt("b1")}},
	}))
	m, _ = step(t, m, key("enter")) // advance to Q2 (tab=1, answers has 1)
	// the request is cleared out-of-band (replied elsewhere)
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	m, _ = step(t, m, sseEventMsg{ev: opcode42client.SSEEvent{Type: "question.replied", Properties: props}})
	if m.pendingQuestion() != nil {
		t.Fatal("SSE replied should clear the pending question")
	}
	if m.qBody.tab != 0 || m.qBody.answers != nil {
		t.Fatalf("a cleared question should reset step state, tab=%d answers=%+v", m.qBody.tab, m.qBody.answers)
	}
}

// TestQuestion_SSERepliedRecordsAnsweredCard asserts the SSE question.replied
// event records an answered card in the store (plan 08e §E4) so the question
// stays visible in the stream history. The SSE event carries only the request
// id, so the card's Answers is empty (the local reply path fills the labels; the
// SSE path is the fallback for replies that originated elsewhere).
func TestQuestion_SSERepliedRecordsAnsweredCard(t *testing.T) {
	s := newStore()
	s = s.Reduce(questionEvent(t, "qst_1", []QuestionInfo{{Question: "pick a color", Header: "Color"}}))
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	s = s.Reduce(opcode42client.SSEEvent{Type: "question.replied", Properties: props})
	if len(s.questions) != 0 {
		t.Fatalf("question.replied should clear the pending question; got %+v", s.questions)
	}
	got := s.answeredQuestions["ses_1"]
	if len(got) != 1 || got[0].ID != "qst_1" {
		t.Fatalf("answered card not recorded; got %+v", got)
	}
	if got[0].Skipped {
		t.Fatal("question.replied should record Skipped=false")
	}
	if len(got[0].Questions) != 1 || got[0].Questions[0].Question != "pick a color" {
		t.Fatalf("answered card should capture the question text; got %+v", got[0].Questions)
	}
}

// TestQuestion_SSERejectedRecordsSkippedCard asserts the SSE question.rejected
// event records an answered card with Skipped=true (plan 08e §E4).
func TestQuestion_SSERejectedRecordsSkippedCard(t *testing.T) {
	s := newStore()
	s = s.Reduce(questionEvent(t, "qst_1", []QuestionInfo{{Question: "Q"}}))
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	s = s.Reduce(opcode42client.SSEEvent{Type: "question.rejected", Properties: props})
	got := s.answeredQuestions["ses_1"]
	if len(got) != 1 || !got[0].Skipped {
		t.Fatalf("question.rejected should record a skipped card; got %+v", got)
	}
}

// TestQuestionRepliedMsg_RecordsAnsweredCardWithLabels asserts the local reply
// path (questionRepliedMsg success) records an answered card carrying the
// specific selected labels from the per-request answer state (plan 08e §E4).
// The local path knows the labels; the SSE path records a label-less "Answered"
// fallback. Deduped by id so the SSE event arriving afterwards doesn't add a
// duplicate.
func TestQuestionRepliedMsg_RecordsAnsweredCardWithLabels(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Header: "Color", Question: "Pick a color", Options: []QuestionOption{opt("red"), opt("green"), opt("blue")}},
	}))
	// Move to "green" and reply (single → immediate reply on enter).
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("enter"))
	// qBody.replying is true; the reply cmd hasn't run. Drive the success msg.
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1"})
	if m.pendingQuestion() != nil {
		t.Fatal("the pending question should be cleared on reply success")
	}
	got := m.store.answeredQuestions["ses_1"]
	if len(got) != 1 {
		t.Fatalf("one answered card expected; got %+v", got)
	}
	if got[0].Skipped {
		t.Fatal("reply should record Skipped=false")
	}
	if len(got[0].Answers) != 1 || len(got[0].Answers[0]) != 1 || got[0].Answers[0][0] != "green" {
		t.Fatalf("answer labels should be [[green]]; got %+v", got[0].Answers)
	}
}

// TestQuestionRepliedMsg_DedupsWithSSEEvent asserts that when the local reply
// path records an answered card and the SSE question.replied event then
// arrives, the answered-questions slice keeps a single entry (deduped by id).
func TestQuestionRepliedMsg_DedupsWithSSEEvent(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q", Options: []QuestionOption{opt("a"), opt("b")}},
	}))
	m, _ = step(t, m, key("enter")) // reply with [[a]] (single → immediate)
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1"})
	// The SSE event arrives after the local reply.
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	m, _ = step(t, m, sseEventMsg{ev: opcode42client.SSEEvent{Type: "question.replied", Properties: props}})
	got := m.store.answeredQuestions["ses_1"]
	if len(got) != 1 {
		t.Fatalf("dedup should keep one answered card; got %+v", got)
	}
	if len(got[0].Answers) != 1 || got[0].Answers[0][0] != "a" {
		t.Fatalf("the local labels should be retained, not overwritten by the SSE fallback; got %+v", got[0].Answers)
	}
}

// TestQuestionRepliedMsg_LocalRejectRecordsSkipped asserts the local reject
// path (esc → rejectQuestionCmd → questionRepliedMsg success) records an
// answered card with Skipped=true, NOT the highlighted-but-not-submitted
// labels (plan 08e §E4). The qBody.rejecting flag distinguishes the in-flight
// action so recordLocalAnsweredQuestion records Skipped for a reject.
func TestQuestionRepliedMsg_LocalRejectRecordsSkipped(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q", Options: []QuestionOption{opt("a"), opt("b")}},
	}))
	// Move to "b" (highlighting it) but reject — the highlight must NOT be
	// recorded as the answer.
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("esc")) // reject (qBody.replying=true, qBody.rejecting=true)
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1"})
	got := m.store.answeredQuestions["ses_1"]
	if len(got) != 1 {
		t.Fatalf("one answered card expected; got %+v", got)
	}
	if !got[0].Skipped {
		t.Fatal("a local reject should record Skipped=true")
	}
	if len(got[0].Answers) != 0 {
		t.Fatalf("a local reject should not record answers; got %+v", got[0].Answers)
	}
}

// TestQuestion_ReplyAfterFailedRejectRecordsLabels asserts that a reply
// following a FAILED reject records the labels (not Skipped). The rejecting
// flag must be cleared on the reply attempt so a prior failed reject doesn't
// taint the reply's answered-card state (plan 08e §E4).
func TestQuestion_ReplyAfterFailedRejectRecordsLabels(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q", Options: []QuestionOption{opt("a"), opt("b")}},
	}))
	// Reject attempt fails (non-404) — qBody.rejecting stays true, replying false.
	m, _ = step(t, m, key("esc"))
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1", err: errTest})
	if m.qBody.replying {
		t.Fatal("a failed reject should clear replying")
	}
	if !m.qBody.rejecting {
		t.Fatal("rejecting should be retained after a failed reject (the user may retry the reject)")
	}
	// Now the user changes their mind and replies with "a" (enter).
	m, _ = step(t, m, key("enter"))
	if m.qBody.rejecting {
		t.Fatal("the reply path must clear rejecting so the reply records labels, not Skipped")
	}
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1"})
	got := m.store.answeredQuestions["ses_1"]
	if len(got) != 1 {
		t.Fatalf("one answered card expected; got %+v", got)
	}
	if got[0].Skipped {
		t.Fatal("a reply after a failed reject should record Skipped=false (labels win)")
	}
	if len(got[0].Answers) != 1 || got[0].Answers[0][0] != "a" {
		t.Fatalf("the reply labels should be recorded; got %+v", got[0].Answers)
	}
}

// TestQuestionRepliedMsg_SSEArrivesFirst_UpgradesWithLabels asserts the
// out-of-order edge case: if the SSE question.replied event arrives BEFORE the
// local questionRepliedMsg, the SSE event is deferred (not applied) so the
// local reply path can still record the locally-selected labels. The deferred
// SSE event is then applied by questionRepliedMsg (a no-op dedup-wise). This
// guards the plan 08e §E4 design: the SSE-clear-during-replying is deferred
// to avoid losing the labels.
func TestQuestionRepliedMsg_SSEArrivesFirst_UpgradesWithLabels(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q", Options: []QuestionOption{opt("a"), opt("b")}},
	}))
	m, _ = step(t, m, key("enter")) // reply with [[a]] (replying=true, cmd dispatched)
	// The SSE event arrives before the HTTP response (out of order). It is
	// deferred (not applied) because replying is true; the pending question
	// stays in the store so recordLocalAnsweredQuestion can still find it.
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	m, _ = step(t, m, sseEventMsg{ev: opcode42client.SSEEvent{Type: "question.replied", Properties: props}})
	if m.qDeferredSSE.Type != "question.replied" {
		t.Fatalf("the SSE event should be deferred; got %+v", m.qDeferredSSE)
	}
	if m.pendingQuestion() == nil {
		t.Fatal("the pending question should still be in the store (SSE deferred)")
	}
	// The HTTP response arrives; the local path records the labels, then
	// applies the deferred SSE event (a no-op dedup-wise) + clears the question.
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1"})
	got := m.store.answeredQuestions["ses_1"]
	if len(got) != 1 {
		t.Fatalf("one answered card expected; got %+v", got)
	}
	if len(got[0].Answers) != 1 || got[0].Answers[0][0] != "a" {
		t.Fatalf("the local labels should be recorded; got %+v", got[0].Answers)
	}
	if m.qDeferredSSE.Type != "" {
		t.Fatalf("the deferred SSE event should be applied + cleared; got %+v", m.qDeferredSSE)
	}
	if m.pendingQuestion() != nil {
		t.Fatal("the pending question should be cleared after questionRepliedMsg")
	}
}

// TestRenderSession_ShowsAnsweredQuestionCard asserts a finalized question
// renders as a collapsed in-stream card with the selected labels (plan 08e §E4).
func TestRenderSession_ShowsAnsweredQuestionCard(t *testing.T) {
	m := seededSessionModel(t)
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Header: "Color", Question: "Pick a color", Options: []QuestionOption{opt("red"), opt("green"), opt("blue")}},
	}))
	// Reply with "green" via the local path (single → immediate).
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 60})
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("enter"))
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1"})
	plain := stripANSI(m.renderSession())
	for _, want := range []string{"Color", "Pick a color", "green"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("answered question card missing %q in stream:\n%s", want, plain)
		}
	}
}

// TestRenderSession_ShowsSkippedQuestionCard asserts a rejected question renders
// as a collapsed in-stream card with "Skipped" (plan 08e §E4).
func TestRenderSession_ShowsSkippedQuestionCard(t *testing.T) {
	m := seededSessionModel(t)
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Pick a color", Options: []QuestionOption{opt("red")}},
	}))
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	m.store = m.store.Reduce(opcode42client.SSEEvent{Type: "question.rejected", Properties: props})
	plain := stripANSI(m.renderSession())
	for _, want := range []string{"Pick a color", "Skipped"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("skipped question card missing %q in stream:\n%s", want, plain)
		}
	}
}
