package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	forgeclient "github.com/rotemmiz/forge/sdk/go"
)

func questionEvent(t *testing.T, id string, qs []QuestionInfo) forgeclient.SSEEvent {
	t.Helper()
	props, _ := json.Marshal(map[string]any{"id": id, "sessionID": "ses_1", "questions": qs})
	return forgeclient.SSEEvent{Type: "question.asked", Properties: props}
}

func opt(label string) QuestionOption { return QuestionOption{Label: label} }

func TestQuestion_AskedThenRepliedReduces(t *testing.T) {
	s := newStore()
	s = s.Reduce(questionEvent(t, "qst_1", []QuestionInfo{{Question: "pick", Options: []QuestionOption{opt("a")}}}))
	if len(s.questions) != 1 {
		t.Fatalf("question.asked should add a pending question: %+v", s.questions)
	}
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	s = s.Reduce(forgeclient.SSEEvent{Type: "question.rejected", Properties: props})
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
	view := m.View()
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
	m, _ = step(t, m, sseEventMsg{ev: forgeclient.SSEEvent{Type: "question.replied", Properties: props}})
	if m.pendingQuestion() != nil {
		t.Fatal("SSE replied should clear the pending question")
	}
	if m.qIdx != 0 || m.qAnswers != nil {
		t.Fatalf("a cleared question should reset step state, qIdx=%d answers=%+v", m.qIdx, m.qAnswers)
	}
}
