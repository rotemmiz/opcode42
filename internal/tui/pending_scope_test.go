package tui

import (
	"encoding/json"
	"testing"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// TestPendingScope_ParentAggregatesChildren pins plan 08f H18 / opencode
// routes/session/index.tsx:207-236: when viewing a parent, permissions and
// questions from the parent and its direct children surface in FIFO order;
// unrelated sessions are ignored; viewing a child suppresses all overlays.
func TestPendingScope_ParentAggregatesChildren(t *testing.T) {
	m := withSubagents() // ses_parent + ses_child1 + ses_child2

	m.store.permissions = []Permission{
		{ID: "p_unrelated", SessionID: "ses_other", Permission: "bash"},
		{ID: "p_child", SessionID: "ses_child1", Permission: "edit"},
		{ID: "p_parent", SessionID: "ses_parent", Permission: "bash"},
	}
	m.store.questions = []Question{
		{ID: "q_unrelated", SessionID: "ses_other"},
		{ID: "q_child", SessionID: "ses_child2"},
		{ID: "q_parent", SessionID: "ses_parent"},
	}

	// Parent view: oldest in-scope permission is the child's (FIFO among
	// scoped entries — p_unrelated is skipped).
	if got := m.pendingPermission(); got == nil || got.ID != "p_child" {
		t.Fatalf("parent pendingPermission = %+v, want p_child", got)
	}
	if got := m.pendingQuestion(); got == nil || got.ID != "q_child" {
		t.Fatalf("parent pendingQuestion = %+v, want q_child", got)
	}

	// Child view: no aggregation — overlays are answered from the parent.
	m.cfg.SessionID = "ses_child1"
	if got := m.pendingPermission(); got != nil {
		t.Fatalf("child view pendingPermission = %+v, want nil", got)
	}
	if got := m.pendingQuestion(); got != nil {
		t.Fatalf("child view pendingQuestion = %+v, want nil", got)
	}

	// Splash / no open session / not on session screen: nothing pending.
	m.cfg.SessionID = ""
	if got := m.pendingPermission(); got != nil {
		t.Fatalf("splash pendingPermission = %+v, want nil", got)
	}
	m.cfg.SessionID = "ses_parent"
	m.screen = ScreenSplash
	if got := m.pendingPermission(); got != nil {
		t.Fatalf("ScreenSplash pendingPermission = %+v, want nil", got)
	}
}

// TestPendingScope_RootWithoutChildrenStillSeesOwn verifies a lone root
// session still surfaces its own pending prompts (scope = {self}).
func TestPendingScope_RootWithoutChildrenStillSeesOwn(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.store.permissions = []Permission{
		{ID: "p_other", SessionID: "ses_x", Permission: "bash"},
		{ID: "p_own", SessionID: "ses_1", Permission: "edit"},
	}
	if got := m.pendingPermission(); got == nil || got.ID != "p_own" {
		t.Fatalf("root pendingPermission = %+v, want p_own", got)
	}
}

// TestPendingScopeIDs_FlatOneLevel verifies grandchildren are NOT in scope
// (opencode aggregates one level only).
func TestPendingScopeIDs_FlatOneLevel(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_parent")
	m.store.sessions = []Session{
		{ID: "ses_parent"},
		{ID: "ses_child", ParentID: "ses_parent"},
		{ID: "ses_grand", ParentID: "ses_child"},
	}
	scope := m.pendingScopeIDs()
	if !scope["ses_parent"] || !scope["ses_child"] {
		t.Fatalf("scope = %v, want parent+child", scope)
	}
	if scope["ses_grand"] {
		t.Fatalf("scope must not include grandchild: %v", scope)
	}
}

// TestPendingScopeIDs_UnloadedSessionIsEmpty verifies we do not guess
// parent-vs-child when the open session is not yet in store.sessions.
func TestPendingScopeIDs_UnloadedSessionIsEmpty(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_maybe_child"})
	m.screen = ScreenSession
	if scope := m.pendingScopeIDs(); scope != nil {
		t.Fatalf("unloaded session scope = %v, want nil", scope)
	}
}

// TestQuestionSSE_DeferUsesRequestIDNotScope pins that an in-flight reply
// still defers question.replied after navigating out of scope (child view),
// so recordLocalAnsweredQuestion can run before the store clears.
func TestQuestionSSE_DeferUsesRequestIDNotScope(t *testing.T) {
	m := withSubagents()
	m.store.questions = []Question{{
		ID: "qst_1", SessionID: "ses_parent",
		Questions: []QuestionInfo{{Question: "Q", Options: []QuestionOption{opt("a")}}},
	}}
	m.qBody = questionBodyState{requestID: "qst_1", answers: [][]string{{"a"}}, replying: true}
	// Navigate into a child while the reply is in flight — pendingQuestion
	// becomes nil (H18), but the SSE defer must still fire.
	m.cfg.SessionID = "ses_child1"
	props, _ := json.Marshal(map[string]any{"requestID": "qst_1"})
	m, _ = step(t, m, sseEventMsg{ev: opcode42client.SSEEvent{
		Type: "question.replied", Properties: props,
	}})
	if m.qDeferredSSE.Type != "question.replied" {
		t.Fatalf("expected deferred SSE while replying out of scope; got type=%q", m.qDeferredSSE.Type)
	}
	if len(m.store.questions) != 1 {
		t.Fatalf("deferred SSE must not clear the store yet; got %+v", m.store.questions)
	}
}
