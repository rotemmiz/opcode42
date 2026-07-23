package tui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// reconcileServer is a stand-in daemon serving GET /permission + GET /question
// with canned lists, and recording the paths hit (in order) for assertions.
type reconcileServer struct {
	srv       *httptest.Server
	mu        sync.Mutex
	paths     []string
	permErr   int // 0 = serve perms, else status code
	qErr      int
	perms     []Permission
	questions []Question
}

func newReconcileServer() *reconcileServer {
	r := &reconcileServer{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.paths = append(r.paths, req.URL.Path)
		r.mu.Unlock()
		switch req.URL.Path {
		case "/permission":
			if r.permErr != 0 {
				w.WriteHeader(r.permErr)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(r.perms)
		case "/question":
			if r.qErr != 0 {
				w.WriteHeader(r.qErr)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(r.questions)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return r
}

func (r *reconcileServer) close() { r.srv.Close() }

func (r *reconcileServer) client() *opcode42client.Opcode42Client {
	c, err := opcode42client.New(r.srv.URL, opcode42client.Options{HTTPClient: r.srv.Client()})
	if err != nil {
		panic(err)
	}
	return c
}

func (r *reconcileServer) hit(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.paths {
		if p == path {
			return true
		}
	}
	return false
}

// TestReconcilePending_ReplaceOnSuccess fires reconcilePendingCmd against a
// server holding a fresh list and asserts the store's permissions/questions are
// REPLACED (stale entries dropped, new entries adopted) — not merged.
func TestReconcilePending_ReplaceOnSuccess(t *testing.T) {
	rs := newReconcileServer()
	defer rs.close()
	rs.perms = []Permission{{ID: "per_new", SessionID: "ses_1", Permission: "bash"}}
	rs.questions = []Question{{ID: "qst_new", SessionID: "ses_1", Questions: []QuestionInfo{{Question: "fresh"}}}}

	m := New(Config{URL: rs.srv.URL})
	m.client = rs.client()
	// Seed the store with stale entries that must be replaced away.
	m.store.permissions = []Permission{{ID: "per_stale", SessionID: "ses_1"}}
	m.store.questions = []Question{{ID: "qst_stale", SessionID: "ses_1"}}

	cmd := reconcilePendingCmd(m.ctx, m.client)
	msgs := collectMsgs(t, cmd)

	var gotPerm, gotQ bool
	for _, msg := range msgs {
		switch v := msg.(type) {
		case permissionsReconciledMsg:
			if v.err != nil {
				t.Fatalf("perm reconcile err: %v", v.err)
			}
			m, _ = step(t, m, v)
			gotPerm = true
		case questionsReconciledMsg:
			if v.err != nil {
				t.Fatalf("q reconcile err: %v", v.err)
			}
			m, _ = step(t, m, v)
			gotQ = true
		}
	}
	if !gotPerm || !gotQ {
		t.Fatalf("reconcile did not produce both msgs; perm=%v q=%v", gotPerm, gotQ)
	}
	if len(m.store.permissions) != 1 || m.store.permissions[0].ID != "per_new" {
		t.Fatalf("permissions not replaced: %+v", m.store.permissions)
	}
	if len(m.store.questions) != 1 || m.store.questions[0].ID != "qst_new" {
		t.Fatalf("questions not replaced: %+v", m.store.questions)
	}
	if !rs.hit("/permission") || !rs.hit("/question") {
		t.Fatalf("expected GET /permission + /question; got %v", rs.paths)
	}
}

// TestReconcilePending_ErrorLeavesStoreUnchanged asserts a fetch failure does
// not wipe the store — a flaky GET must not blank the UI (matches Android's
// runCatching-per-call: a 500 on one endpoint leaves the other's state intact).
func TestReconcilePending_ErrorLeavesStoreUnchanged(t *testing.T) {
	rs := newReconcileServer()
	defer rs.close()
	rs.permErr = http.StatusInternalServerError
	rs.qErr = http.StatusInternalServerError

	m := New(Config{URL: rs.srv.URL})
	m.client = rs.client()
	m.store.permissions = []Permission{{ID: "per_keep", SessionID: "ses_1"}}
	m.store.questions = []Question{{ID: "qst_keep", SessionID: "ses_1"}}

	cmd := reconcilePendingCmd(m.ctx, m.client)
	for _, msg := range collectMsgs(t, cmd) {
		switch v := msg.(type) {
		case permissionsReconciledMsg:
			if v.err == nil {
				t.Fatal("perm reconcile should have errored")
			}
			m, _ = step(t, m, v)
		case questionsReconciledMsg:
			if v.err == nil {
				t.Fatal("q reconcile should have errored")
			}
			m, _ = step(t, m, v)
		}
	}
	if len(m.store.permissions) != 1 || m.store.permissions[0].ID != "per_keep" {
		t.Fatalf("permissions should be unchanged on error: %+v", m.store.permissions)
	}
	if len(m.store.questions) != 1 || m.store.questions[0].ID != "qst_keep" {
		t.Fatalf("questions should be unchanged on error: %+v", m.store.questions)
	}
}

// TestStreamOpenedMsg_TriggersReconcile sends a successful streamOpenedMsg and
// asserts the returned batch fires reconcilePendingCmd (the server sees both
// GET /permission and GET /question). This is the reconnect path (plan 16 Bug 3).
func TestStreamOpenedMsg_TriggersReconcile(t *testing.T) {
	rs := newReconcileServer()
	defer rs.close()

	m := New(Config{URL: rs.srv.URL})
	m.client = rs.client()

	// A successful streamOpenedMsg must return a batch that includes both
	// listenCmd (keep pumping events) and reconcilePendingCmd (re-fetch state).
	stream := &opcode42client.EventStream{}
	next, cmd := step(t, m, streamOpenedMsg{stream: stream})
	if cmd == nil {
		t.Fatal("streamOpenedMsg should return a batch")
	}
	// collectMsgs runs the batch in a goroutine; the reconcile leaves hit the
	// server quickly, the listen leaf blocks on the empty stream (no events)
	// and is left running — stream.Close() below releases it.
	for _, msg := range collectMsgs(t, cmd) {
		if _, ok := msg.(permissionsReconciledMsg); ok {
			next, _ = step(t, next, msg)
		}
		if _, ok := msg.(questionsReconciledMsg); ok {
			next, _ = step(t, next, msg)
		}
	}
	if !rs.hit("/permission") || !rs.hit("/question") {
		t.Fatalf("reconnect did not fire GET /permission + /question; paths=%v", rs.paths)
	}
	if next.conn != Connected {
		t.Fatalf("conn = %v, want Connected", next.conn)
	}
	stream.Close()
}

// TestSseEventMsg_SessionIdleTriggersReconcile asserts a session.status SSE
// event with status.type == "idle" for the open session fires reconcile. A busy
// transition or an idle for a different session must NOT fire it.
func TestSseEventMsg_SessionIdleTriggersReconcile(t *testing.T) {
	rs := newReconcileServer()
	defer rs.close()

	m := New(Config{URL: rs.srv.URL})
	m.client = rs.client()
	m.cfg.SessionID = "ses_open"
	// sseEventMsg's handler re-issues listenCmd(m.stream), so the model needs a
	// live stream for the returned batch to not panic. An empty EventStream
	// blocks listenCmd forever (no events) — collectMsgs leaves it running.
	m.stream = &opcode42client.EventStream{}
	defer m.stream.Close()

	cases := []struct {
		name    string
		ev      opcode42client.SSEEvent
		wantHit bool
	}{
		{
			name:    "idle for open session fires reconcile",
			ev:      idleStatusEvent("ses_open"),
			wantHit: true,
		},
		{
			name:    "busy for open session does not fire",
			ev:      statusEvent("ses_open", "busy"),
			wantHit: false,
		},
		{
			name:    "idle for a different session does not fire",
			ev:      idleStatusEvent("ses_other"),
			wantHit: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rs.mu.Lock()
			rs.paths = nil
			rs.mu.Unlock()
			_, cmd := step(t, m, sseEventMsg{ev: c.ev})
			collectMsgs(t, cmd)
			got := rs.hit("/permission") && rs.hit("/question")
			if got != c.wantHit {
				t.Fatalf("reconcile hit=%v, want %v (paths=%v)", got, c.wantHit, rs.paths)
			}
		})
	}
}

// TestPermissionReplied_404Swallowed sends a permissionRepliedMsg carrying a
// 404 and asserts the status line is NOT set to the error string (plan 16 Bug 1).
// The optimistic clear already removed the entry from the UI; the 404 just
// confirms the daemon no longer has it.
func TestPermissionReplied_404Swallowed(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.permissions = []Permission{{ID: "per_gone", SessionID: "ses_1"}}
	m.permState = permSetReplying(m.permState, true)

	notFound := errors.New("POST /permission/per_gone/reply: status 404")
	next, _ := step(t, m, permissionRepliedMsg{id: "per_gone", err: notFound})

	if strings.Contains(next.status, "404") || strings.Contains(next.status, "failed") {
		t.Fatalf("404 should be swallowed silently; status=%q", next.status)
	}
	if next.permState.replying {
		t.Fatal("permState.replying should be cleared after a 404")
	}
	if len(next.store.permissions) != 0 {
		t.Fatalf("the stale permission should be cleared on 404; got %+v", next.store.permissions)
	}
}

// TestQuestionReplied_404Swallowed is the question-side counterpart: a 404 on
// reply/reject is swallowed silently and the question is cleared.
func TestQuestionReplied_404Swallowed(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.questions = []Question{{ID: "qst_gone", SessionID: "ses_1", Questions: []QuestionInfo{{Question: "q"}}}}
	m.qBody = questionSetReplying(m.qBody, true, false)

	notFound := errors.New("POST /question/qst_gone/reply: status 404")
	next, _ := step(t, m, questionRepliedMsg{id: "qst_gone", err: notFound})

	if strings.Contains(next.status, "404") || strings.Contains(next.status, "failed") {
		t.Fatalf("404 should be swallowed silently; status=%q", next.status)
	}
	if next.qBody.replying {
		t.Fatal("qBody.replying should be cleared after a 404")
	}
	if len(next.store.questions) != 0 {
		t.Fatalf("the stale question should be cleared on 404; got %+v", next.store.questions)
	}
	if next.qBody.tab != 0 || next.qBody.answers != nil {
		t.Fatalf("question state should be reset on 404; tab=%d answers=%+v", next.qBody.tab, next.qBody.answers)
	}
}

// TestPermissionReplied_Non404StillSurfaces asserts a non-404 error still keeps
// the request and surfaces the status — only 404 is swallowed.
func TestPermissionReplied_Non404StillSurfaces(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.store.permissions = []Permission{{ID: "per_x", SessionID: "ses_1"}}
	m.permState = permSetReplying(m.permState, true)

	serverErr := errors.New("POST /permission/per_x/reply: status 500")
	next, _ := step(t, m, permissionRepliedMsg{id: "per_x", err: serverErr})

	if !strings.Contains(next.status, "500") || !strings.Contains(next.status, "failed") {
		t.Fatalf("non-404 should surface in status; got %q", next.status)
	}
	if next.permState.replying {
		t.Fatal("permState.replying should be cleared so the user can retry")
	}
	if len(next.store.permissions) != 1 || next.store.permissions[0].ID != "per_x" {
		t.Fatalf("non-404 should KEEP the request for retry; got %+v", next.store.permissions)
	}
}

// TestQuestionsReconciled_ResetsActiveQuestionState asserts that when a
// reconcile replaces the store and the active question disappears, the
// per-request answer state (qBody.tab/answers/replying) is reset so the overlay
// closes cleanly instead of pointing at a vanished request.
func TestQuestionsReconciled_ResetsActiveQuestionState(t *testing.T) {
	m := openSes(New(Config{URL: "http://x"}), "ses_1")
	m.store.questions = []Question{{ID: "qst_active", SessionID: "ses_1", Questions: []QuestionInfo{{Question: "q"}}}}
	m.qBody = questionBodyState{tab: 1, answers: [][]string{{"a1"}}, replying: true}

	// Reconcile returns an empty list — the active question is gone.
	next, _ := step(t, m, questionsReconciledMsg{questions: nil})

	if len(next.store.questions) != 0 {
		t.Fatalf("questions should be replaced to empty; got %+v", next.store.questions)
	}
	if next.qBody.tab != 0 || next.qBody.answers != nil || next.qBody.replying {
		t.Fatalf("active-question state should be reset; tab=%d answers=%+v replying=%v",
			next.qBody.tab, next.qBody.answers, next.qBody.replying)
	}
}

// --- helpers ---

// collectMsgs runs a (possibly batched) tea.Cmd in a goroutine and gathers the
// leaf messages that arrive within the timeout. Blocking leaves (e.g.
// listenCmd on an empty stream) never produce a message and are left running;
// the caller closes the stream / cancels the context to release them. Used for
// reconcile assertions where the interesting leaves (the two GETs) complete
// quickly. The 300ms cap is plenty for an in-process httptest round-trip and
// keeps the test gate fast.
func collectMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	out := make(chan tea.Msg, 16)
	var walk func(c tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, sub := range v {
				go walk(sub)
			}
		case nil:
		default:
			out <- v
		}
	}
	go walk(cmd)
	var got []tea.Msg
	timer := time.After(300 * time.Millisecond)
	for {
		select {
		case m := <-out:
			got = append(got, m)
		case <-timer:
			return got
		}
	}
}

func statusEvent(sessionID, statusType string) opcode42client.SSEEvent {
	props, _ := json.Marshal(map[string]any{
		"sessionID": sessionID,
		"status":    map[string]any{"type": statusType},
	})
	return opcode42client.SSEEvent{Type: "session.status", Properties: props}
}

func idleStatusEvent(sessionID string) opcode42client.SSEEvent {
	return statusEvent(sessionID, "idle")
}
