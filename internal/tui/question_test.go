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
	// move to "green"
	m, _ = step(t, m, key("down"))
	if got := m.finalAnswers(m.curQuestion()); len(got) != 1 || got[0][0] != "green" {
		t.Fatalf("answer should be [[green]], got %+v", got)
	}
	next, cmd := step(t, m, key("enter"))
	if cmd == nil || !next.qReplying {
		t.Fatal("enter on the last question should dispatch a reply")
	}
}

func TestQuestion_MultiSelectTogglesThenReplies(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Pick some", Multiple: true, Options: []QuestionOption{opt("x"), opt("y"), opt("z")}},
	}))
	// toggle x (idx0), move to z (idx2), toggle z
	m, _ = step(t, m, key(" "))
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key(" "))
	got := m.finalAnswers(m.curQuestion())
	if len(got) != 1 || len(got[0]) != 2 || got[0][0] != "x" || got[0][1] != "z" {
		t.Fatalf("multi-select answer should be [[x z]], got %+v", got)
	}
	if _, cmd := step(t, m, key("enter")); cmd == nil {
		t.Fatal("enter should reply")
	}
}

func TestQuestion_MultiQuestionStepsThenReplies(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q1", Options: []QuestionOption{opt("a1"), opt("a2")}},
		{Question: "Q2", Options: []QuestionOption{opt("b1"), opt("b2")}},
	}))
	// answer Q1 = a1 (enter advances, no reply yet)
	m, cmd := step(t, m, key("enter"))
	if cmd != nil {
		t.Fatal("enter on a non-final question should NOT reply")
	}
	if m.qIdx != 1 {
		t.Fatalf("should advance to question 2, qIdx=%d", m.qIdx)
	}
	// answer Q2 = b2
	m, _ = step(t, m, key("down"))
	got := m.finalAnswers(m.curQuestion())
	if len(got) != 2 || got[0][0] != "a1" || got[1][0] != "b2" {
		t.Fatalf("answers should be [[a1] [b2]], got %+v", got)
	}
	if _, cmd := step(t, m, key("enter")); cmd == nil {
		t.Fatal("enter on the final question should reply")
	}
}

func TestQuestion_RejectAndResolve(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{{Question: "Q", Options: []QuestionOption{opt("a")}}}))
	// r rejects (dispatches), overlay stays until resolved
	next, cmd := m.handleQuestionKey(key("r"))
	if cmd == nil || !next.(Model).qReplying {
		t.Fatal("r should dispatch a reject and mark replying")
	}
	// success clears it + resets state
	mOK, _ := step(t, next.(Model), questionRepliedMsg{id: "qst_1"})
	if mOK.pendingQuestion() != nil || mOK.qReplying || mOK.qIdx != 0 {
		t.Fatal("a resolved reject should clear the question and reset state")
	}
}

func TestQuestion_FailedReplyRetryNoDoubleAppend(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q1", Options: []QuestionOption{opt("a1"), opt("a2")}},
		{Question: "Q2", Options: []QuestionOption{opt("b1"), opt("b2")}},
	}))
	m, _ = step(t, m, key("enter")) // Q1 = a1, advance
	m, _ = step(t, m, key("down"))  // Q2 cursor → b2
	// final enter → dispatches reply built as [a1, b2], leaving qAnswers = [a1]
	m, cmd := step(t, m, key("enter"))
	if cmd == nil || !m.qReplying {
		t.Fatal("final enter should reply")
	}
	if len(m.qAnswers) != 1 { // only advanced steps are durable; the final is not appended
		t.Fatalf("durable qAnswers should hold only Q1, got %+v", m.qAnswers)
	}
	// reply FAILS → state retained, replying cleared
	m, _ = step(t, m, questionRepliedMsg{id: "qst_1", err: errTest})
	if m.pendingQuestion() == nil || m.qReplying {
		t.Fatal("a failed reply should keep the question and clear replying")
	}
	// retry enter, repeatedly: the durable qAnswers must NOT grow (no double-append),
	// so each rebuilt submission stays [a1, b2] (length 2).
	for i := 0; i < 3; i++ {
		m, _ = step(t, m, questionRepliedMsg{id: "qst_1", err: errTest})
		m, cmd = step(t, m, key("enter"))
		if cmd == nil {
			t.Fatal("retry enter should re-dispatch")
		}
		if len(m.qAnswers) != 1 || m.qIdx != 1 {
			t.Fatalf("retry %d corrupted step state: qIdx=%d answers=%+v", i, m.qIdx, m.qAnswers)
		}
	}
}

func TestQuestion_EmptyOptionsCannotAnswer(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Type your name", Custom: true, Options: nil},
	}))
	// enter is a no-op (no selectable options); only reject works
	next, cmd := m.handleQuestionKey(key("enter"))
	if cmd != nil || next.(Model).qReplying {
		t.Fatal("enter on a free-text-only question must not reply")
	}
	_, cmd = m.handleQuestionKey(key("r"))
	if cmd == nil {
		t.Fatal("r should reject a free-text-only question")
	}
}

func TestQuestion_SSEClearResetsState(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q1", Options: []QuestionOption{opt("a1"), opt("a2")}},
		{Question: "Q2", Options: []QuestionOption{opt("b1")}},
	}))
	m, _ = step(t, m, key("enter")) // advance to Q2 (qIdx=1, qAnswers has 1)
	// the request is cleared out-of-band (replied elsewhere)
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	m, _ = step(t, m, sseEventMsg{ev: opcode42client.SSEEvent{Type: "question.replied", Properties: props}})
	if m.pendingQuestion() != nil {
		t.Fatal("SSE replied should clear the pending question")
	}
	if m.qIdx != 0 || m.qAnswers != nil {
		t.Fatalf("a cleared question should reset step state, qIdx=%d answers=%+v", m.qIdx, m.qAnswers)
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
	// Move to "green" and reply.
	m, _ = step(t, m, key("down"))
	m, _ = step(t, m, key("enter"))
	// qReplying is true; the reply cmd hasn't run. Drive the success msg.
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
	m, _ = step(t, m, key("enter")) // reply with [[a]]
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
// path (r/esc → rejectQuestionCmd → questionRepliedMsg success) records an
// answered card with Skipped=true, NOT the highlighted-but-not-submitted
// labels (plan 08e §E4). The qRejecting flag distinguishes the in-flight
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
	m, _ = step(t, m, key("r")) // reject (qReplying=true, qRejecting=true)
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

// TestQuestionRepliedMsg_SSEArrivesFirst_UpgradesWithLabels asserts the
// out-of-order edge case: if the SSE question.replied event arrives BEFORE the
// local questionRepliedMsg, the SSE event is deferred (not applied) so the
// local reply path can still record the locally-selected labels. The deferred
// SSE event is then applied by questionRepliedMsg (a no-op dedup-wise). This
// guards the plan 08e §E4 design: the SSE-clear-during-qReplying is deferred
// to avoid losing the labels.
func TestQuestionRepliedMsg_SSEArrivesFirst_UpgradesWithLabels(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Question: "Q", Options: []QuestionOption{opt("a"), opt("b")}},
	}))
	m, _ = step(t, m, key("enter")) // reply with [[a]] (qReplying=true, cmd dispatched)
	// The SSE event arrives before the HTTP response (out of order). It is
	// deferred (not applied) because qReplying is true; the pending question
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

// TestRenderSession_ShowsPendingQuestionCard asserts the in-stream pending
// question card renders in the session body (plan 08e §E4). The card is behind
// the blocking overlay while the overlay is up; this test calls renderSession
// directly (which doesn't draw the overlay) so the card is visible.
func TestRenderSession_ShowsPendingQuestionCard(t *testing.T) {
	m := seededSessionModel(t)
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Header: "Color", Question: "Pick a color", Options: []QuestionOption{opt("red"), opt("green")}},
	}))
	plain := stripANSI(m.renderSession())
	for _, want := range []string{"Color", "Pick a color", "enter to answer"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("pending question card missing %q in stream:\n%s", want, plain)
		}
	}
}

// TestRenderSession_ShowsAnsweredQuestionCard asserts a finalized question
// renders as a collapsed in-stream card with the selected labels (plan 08e §E4).
func TestRenderSession_ShowsAnsweredQuestionCard(t *testing.T) {
	m := seededSessionModel(t)
	m.store = m.store.Reduce(questionEvent(t, "qst_1", []QuestionInfo{
		{Header: "Color", Question: "Pick a color", Options: []QuestionOption{opt("red"), opt("green"), opt("blue")}},
	}))
	// Reply with "green" via the local path.
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
