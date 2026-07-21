package tui

// subagent_card_test.go — Plan 08e §C1-C4: in-stream subagent card + sidebar
// tasks + sessions modal subtree.
//
// Covers the plan's test list:
//  1. TestTaskCard_RendersHeaderAndMeta — a `task` tool part renders the card
//     with kind + description (header) + the meta line.
//  2. TestChildSessionID_ParsedFromTaskInput — a `task` tool with
//     metadata.sessionId records the child id; the <task id="…"> output
//     wrapper is the fallback when metadata is absent.
//  3. TestLoadChildMessages_FiresCmd — loadChildMessagesCmd calls
//     GET /session/{id}/message (exercised via a stub server).
//  4. TestSidebar_TasksSection_WhenChildrenExist — children present → TASKS
//     section renders; no children → no TASKS section.
//  5. TestSessionsModal_SubtreeToggle — pressing `t` in the sessions modal
//     flips subtree mode; children render indented under their parent.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// taskStateJSON builds a tool state JSON for a `task` tool with the given
// status, description, subagent_type, and optional metadata.sessionId.
func taskStateJSON(t *testing.T, status, description, subagentType, childID string) json.RawMessage {
	t.Helper()
	m := map[string]any{
		"status": status,
		"input": map[string]any{
			"description":   description,
			"subagent_type": subagentType,
			"prompt":        "do the thing",
		},
	}
	if childID != "" {
		m["metadata"] = map[string]any{
			"parentSessionId": "ses_parent",
			"sessionId":       childID,
		}
	}
	return rawState(t, m)
}

// taskStateWithOutput builds a task tool state with output carrying the
// <task id="…" state="…"> wrapper (the fallback child-id source). The childID
// is embedded in the output wrapper, not the metadata.
func taskStateWithOutput(t *testing.T, status, childID, output string) json.RawMessage {
	t.Helper()
	_ = childID // embedded in the output wrapper below; kept for readability
	return rawState(t, map[string]any{
		"status": status,
		"input": map[string]any{
			"description":   "do the thing",
			"subagent_type": "general",
			"prompt":        "do the thing",
		},
		"output": output,
	})
}

// ── 1. Task card render ──────────────────────────────────────────────────────

func TestTaskCard_RendersHeaderAndMeta(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_parent"})
	m.width, m.height = 100, 40
	m.screen = ScreenSession
	state := taskStateJSON(t, "running", "audit the auth flow", "general", "ses_child")
	part := Part{ID: "p_task", MessageID: "msg_1", SessionID: "ses_parent",
		Type: "tool", Tool: "task", State: state}
	row := m.toolRow(part)

	plain := stripANSI(row)
	if !strings.Contains(plain, "General Task") {
		t.Errorf("task card header missing kind 'General Task':\n%s", plain)
	}
	if !strings.Contains(plain, "audit the auth flow") {
		t.Errorf("task card header missing description:\n%s", plain)
	}
	if !strings.Contains(plain, "running…") {
		t.Errorf("task card meta missing 'running…':\n%s", plain)
	}
	if !strings.Contains(plain, "toolcall") {
		t.Errorf("task card meta missing 'toolcall' count:\n%s", plain)
	}
}

func TestTaskCard_CompletedShowsDone(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_parent"})
	m.width = 100
	state := taskStateJSON(t, "completed", "fix the bug", "coding", "ses_child")
	part := Part{ID: "p_task", Tool: "task", Type: "tool", State: state}
	row := m.toolRow(part)
	plain := stripANSI(row)
	if !strings.Contains(plain, "Coding Task") {
		t.Errorf("task card header missing kind 'Coding Task':\n%s", plain)
	}
	if !strings.Contains(plain, "done") {
		t.Errorf("completed task card meta missing 'done':\n%s", plain)
	}
}

// ── 2. Child session id parsing ──────────────────────────────────────────────

func TestChildSessionID_ParsedFromTaskInput(t *testing.T) {
	// Primary source: metadata.sessionId (opencode TaskTool sets it).
	state := taskStateJSON(t, "running", "x", "general", "ses_xyz")
	st, _ := parseToolState(state)
	if got := childSessionID(st); got != "ses_xyz" {
		t.Errorf("childSessionID from metadata = %q, want ses_xyz", got)
	}

	// Fallback: <task id="…" state="…"> wrapper in the output text.
	state2 := taskStateWithOutput(t, "completed", "ses_from_output",
		`<task id="ses_from_output" state="completed">`+"\n"+
			`<task_result>done</task_result>`+"\n"+
			`</task>`)
	st2, _ := parseToolState(state2)
	if got := childSessionID(st2); got != "ses_from_output" {
		t.Errorf("childSessionID from output wrapper = %q, want ses_from_output", got)
	}

	// No child id recoverable.
	state3 := rawState(t, map[string]any{
		"status": "running",
		"input":  map[string]any{"description": "x", "subagent_type": "general", "prompt": "x"},
	})
	st3, _ := parseToolState(state3)
	if got := childSessionID(st3); got != "" {
		t.Errorf("childSessionID with no source = %q, want empty", got)
	}
}

// ── 3. loadChildMessagesCmd ──────────────────────────────────────────────────

func TestLoadChildMessages_FiresCmd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/session/ses_child/message"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"info":{"id":"m1","sessionID":"ses_child","role":"user"},"parts":[{"id":"p1","type":"text","text":"hi"}]},
			{"info":{"id":"m2","sessionID":"ses_child","role":"assistant"},"parts":[{"id":"p2","type":"text","text":"done"}]}
		]`))
	}))
	defer srv.Close()

	m := New(Config{URL: srv.URL})
	cmd := loadChildMessagesCmd(m.ctx, m.client, "ses_child")
	if cmd == nil {
		t.Fatal("loadChildMessagesCmd returned nil")
	}
	msg := cmd()
	loaded, ok := msg.(childMessagesLoadedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want childMessagesLoadedMsg", msg)
	}
	if loaded.err != nil {
		t.Fatalf("load error: %v", loaded.err)
	}
	if loaded.childID != "ses_child" {
		t.Errorf("childID = %q, want ses_child", loaded.childID)
	}
	if len(loaded.items) != 2 {
		t.Errorf("items = %d, want 2", len(loaded.items))
	}
}

// TestLoadChildMessages_IngestsIntoStore verifies the model handler ingests
// the child messages into the store keyed by the child id, so taskTranscript
// can render them.
func TestLoadChildMessages_IngestsIntoStore(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_parent"})
	m.store.sessions = []Session{
		{ID: "ses_parent", Title: "parent"},
		{ID: "ses_child", ParentID: "ses_parent", Title: "@general subagent"},
	}
	items := []wireWithParts{
		{Info: Message{ID: "m1", SessionID: "ses_child", Role: "user"}, Parts: []Part{{ID: "p1", Type: "text", Text: "hi"}}},
		{Info: Message{ID: "m2", SessionID: "ses_child", Role: "assistant"}, Parts: []Part{{ID: "p2", Type: "text", Text: "done"}}},
	}
	next, _ := step(t, m, childMessagesLoadedMsg{childID: "ses_child", items: items})
	if got := len(next.store.messages["ses_child"]); got != 2 {
		t.Fatalf("child messages ingested = %d, want 2", got)
	}
	if got := len(next.store.parts["m1"]); got != 1 {
		t.Errorf("child parts ingested = %d, want 1", got)
	}
}

// ── 4. Sidebar TASKS section ─────────────────────────────────────────────────

func TestSidebar_TasksSection_WhenChildrenExist(t *testing.T) {
	m := withSubagents()
	m.width, m.height = 120, 30
	m.screen = ScreenSession
	out := m.sidebarView()
	plain := stripANSI(out)
	if !strings.Contains(plain, "TASKS") {
		t.Errorf("sidebar with children should render TASKS section:\n%s", plain)
	}
	if !strings.Contains(plain, "Review") {
		t.Errorf("sidebar TASKS missing child label 'Review':\n%s", plain)
	}
	if !strings.Contains(plain, "Plan") {
		t.Errorf("sidebar TASKS missing child label 'Plan':\n%s", plain)
	}

	// No children → no TASKS section.
	m2 := New(Config{URL: "http://x", SessionID: "ses_solo"})
	m2.width, m2.height = 120, 30
	m2.screen = ScreenSession
	m2.store.sessions = []Session{{ID: "ses_solo", Title: "solo"}}
	out2 := m2.sidebarView()
	plain2 := stripANSI(out2)
	if strings.Contains(plain2, "TASKS") {
		t.Errorf("sidebar with no children should NOT render TASKS section:\n%s", plain2)
	}
}

// TestSidebar_TasksSection_StatusGlyphs — a running child shows the spinner
// glyph, a completed child shows ✓. The status is derived from the child
// session's tool parts (childStatus).
func TestSidebar_TasksSection_StatusGlyphs(t *testing.T) {
	m := withSubagents()
	m.width, m.height = 120, 30
	m.screen = ScreenSession
	// Seed ses_child1 with a completed assistant turn → ✓.
	m.store.messages["ses_child1"] = []Message{{ID: "cm1", SessionID: "ses_child1", Role: "assistant"}}
	// Seed ses_child2 with a running tool part → spinner.
	m.store.messages["ses_child2"] = []Message{{ID: "cm2", SessionID: "ses_child2", Role: "assistant"}}
	m.store.parts["cm2"] = []Part{{ID: "cp2", Type: "tool", Tool: "bash",
		State: rawState(t, map[string]any{"status": "running", "input": map[string]any{"command": "ls"}})}}

	plain := stripANSI(m.sidebarView())
	if !strings.Contains(plain, "✓") {
		t.Errorf("completed child should show ✓ in TASKS:\n%s", plain)
	}
	if !strings.ContainsAny(plain, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Errorf("running child should show a braille spinner in TASKS:\n%s", plain)
	}
}

// ── 5. Sessions modal subtree toggle ─────────────────────────────────────────

func TestSessionsModal_SubtreeToggle(t *testing.T) {
	m := withSubagents()
	m.width, m.height = 100, 30
	m.screen = ScreenSession
	m.modal = modalSessions
	m.modalSel = 0

	// Flat mode (default): all sessions listed newest-first, no indentation.
	_, rows, _ := m.modalItems()
	if len(rows) != 3 {
		t.Fatalf("flat sessions list = %d rows, want 3 (parent + 2 children):\n%s",
			len(rows), strings.Join(rows, "\n"))
	}
	for _, r := range rows {
		plain := stripANSI(r)
		if strings.HasPrefix(plain, "└─") || strings.HasPrefix(plain, "├─") {
			t.Errorf("flat mode should not indent rows, got %q", plain)
		}
	}

	// Press `t` → subtree mode.
	next, _ := step(t, m, key("t"))
	if !next.view.sessionsSubtree {
		t.Fatal("pressing t should enable subtree mode")
	}
	if !strings.Contains(next.status, "subtree") {
		t.Errorf("status after t = %q, want 'subtree'", next.status)
	}

	// Subtree rows: parent + 2 children indented (same count, grouped).
	_, rows2, _ := next.modalItems()
	if len(rows2) != 3 {
		t.Fatalf("subtree rows = %d, want 3 (parent + 2 children):\n%s",
			len(rows2), strings.Join(rows2, "\n"))
	}
	hasIndented := false
	for _, r := range rows2[1:] {
		plain := stripANSI(r)
		if strings.HasPrefix(plain, "└─") || strings.HasPrefix(plain, "├─") {
			hasIndented = true
		}
	}
	if !hasIndented {
		t.Errorf("subtree children should be indented with └─/├─:\n%s", strings.Join(rows2, "\n"))
	}

	// Press t again → back to flat.
	next2, _ := step(t, next, key("t"))
	if next2.view.sessionsSubtree {
		t.Fatal("pressing t again should disable subtree mode")
	}
}

// TestSessionsModal_SubtreeSelectOpensChild verifies that selecting an
// indented child row in subtree mode opens that child session (not the parent
// at the same flat index).
func TestSessionsModal_SubtreeSelectOpensChild(t *testing.T) {
	m := withSubagents()
	m.width, m.height = 100, 30
	m.screen = ScreenSession
	m.modal = modalSessions
	m.view.sessionsSubtree = true
	// Rows: [0]=parent, [1]=child1, [2]=child2. Select child2 (index 2).
	m.modalSel = 2
	next, _ := step(t, m, key("enter"))
	if next.cfg.SessionID != "ses_child2" {
		t.Fatalf("subtree select at index 2 → %q, want ses_child2", next.cfg.SessionID)
	}
}

// TestSessionSubtreeIDs_Order verifies the id ordering matches the row
// ordering so modalSelect resolves correctly.
func TestSessionSubtreeIDs_Order(t *testing.T) {
	m := withSubagents()
	ids := m.sessionSubtreeIDs()
	// orderedSessions is newest-first; withSubagents inserts parent first so
	// the parent is the newest by id ordering. The parent's children follow.
	if len(ids) != 3 {
		t.Fatalf("subtree ids = %v, want 3 ids", ids)
	}
	if ids[0] != "ses_parent" {
		t.Errorf("subtree ids[0] = %q, want ses_parent", ids[0])
	}
	if ids[1] != "ses_child1" || ids[2] != "ses_child2" {
		t.Errorf("subtree children = %v, want [ses_child1, ses_child2]", ids[1:])
	}
}

// ── Leader key: ctrl+x > descends into last task child ───────────────────────

func TestLeaderKey_DescendIntoLastTaskChild(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_parent"})
	m.width, m.height = 100, 40
	m.screen = ScreenSession
	m.store.sessions = []Session{
		{ID: "ses_parent", Title: "parent"},
		{ID: "ses_child", ParentID: "ses_parent", Title: "@general subagent"},
	}
	state := taskStateJSON(t, "completed", "do work", "general", "ses_child")
	m.store.messages["ses_parent"] = []Message{
		{ID: "msg_1", SessionID: "ses_parent", Role: "assistant"},
	}
	m.store.parts["msg_1"] = []Part{
		{ID: "p_task", MessageID: "msg_1", SessionID: "ses_parent", Type: "tool", Tool: "task", State: state},
	}

	m, _ = step(t, m, key("ctrl+x"))
	next, cmd := step(t, m, key(">"))
	if next.cfg.SessionID != "ses_child" {
		t.Fatalf("ctrl+x > → %q, want ses_child", next.cfg.SessionID)
	}
	if cmd == nil {
		t.Fatal("ctrl+x > should load the child session's stream")
	}
}

// TestLeaderKey_DescendFallbackNoChildID verifies that ctrl+x > falls back to
// enterFirstChild when the task part has no recoverable child id (no
// metadata, no <task id> wrapper).
func TestLeaderKey_DescendFallbackNoChildID(t *testing.T) {
	m := withSubagents() // has ses_parent + 2 children, no task parts
	m.width, m.height = 100, 40
	m.screen = ScreenSession
	m, _ = step(t, m, key("ctrl+x"))
	next, _ := step(t, m, key(">"))
	if next.cfg.SessionID != "ses_child1" {
		t.Fatalf("ctrl+x > fallback → %q, want ses_child1 (first child)", next.cfg.SessionID)
	}
}

// ── Leader key: ctrl+x v expands task card + fires load ──────────────────────

func TestLeaderKey_V_ExpandsTaskCardAndLoadsChild(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session/ses_child/message" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"info":{"id":"m1","sessionID":"ses_child","role":"assistant"},"parts":[{"id":"p1","type":"text","text":"child response"}]}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	m := New(Config{URL: srv.URL, SessionID: "ses_parent"})
	m.width, m.height = 100, 40
	m.screen = ScreenSession
	m.store.sessions = []Session{
		{ID: "ses_parent", Title: "parent"},
		{ID: "ses_child", ParentID: "ses_parent", Title: "@general subagent"},
	}
	state := taskStateJSON(t, "completed", "do work", "general", "ses_child")
	m.store.messages["ses_parent"] = []Message{{ID: "msg_1", SessionID: "ses_parent", Role: "assistant"}}
	m.store.parts["msg_1"] = []Part{
		{ID: "p_task", MessageID: "msg_1", SessionID: "ses_parent", Type: "tool", Tool: "task", State: state},
	}

	// ctrl+x v on the task card (expanded by default → collapses, no cmd).
	// Toggle again (collapsed → expanded) to fire the load.
	m, _ = step(t, m, key("ctrl+x"))
	next, _ := step(t, m, key("v"))
	if !next.view.isToolCollapsed("p_task") {
		t.Fatal("first ctrl+x v should collapse the expanded task card")
	}
	next, _ = step(t, next, key("ctrl+x"))
	next, cmd := step(t, next, key("v"))
	if cmd == nil {
		t.Fatal("ctrl+x v expanding a task card should fire loadChildMessagesCmd")
	}
	msg := cmd()
	if _, ok := msg.(childMessagesLoadedMsg); !ok {
		t.Fatalf("cmd produced %T, want childMessagesLoadedMsg", msg)
	}
}

// TestMaybeLoadTaskChildMessages_Idempotent verifies the helper doesn't
// re-fire when the child messages are already loaded.
func TestMaybeLoadTaskChildMessages_Idempotent(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_parent"})
	m.screen = ScreenSession
	state := taskStateJSON(t, "completed", "x", "general", "ses_child")
	m.store.messages["ses_parent"] = []Message{{ID: "msg_1", SessionID: "ses_parent", Role: "assistant"}}
	m.store.parts["msg_1"] = []Part{{ID: "p_task", MessageID: "msg_1", Type: "tool", Tool: "task", State: state}}
	// Pre-load the child messages.
	m.store.messages["ses_child"] = []Message{{ID: "cm", SessionID: "ses_child", Role: "assistant"}}

	if cmd := m.maybeLoadTaskChildMessages("p_task"); cmd != nil {
		t.Errorf("maybeLoadTaskChildMessages should return nil when child already loaded, got %v", cmd)
	}
	// Non-task part → nil.
	m.store.parts["msg_1"] = append(m.store.parts["msg_1"], Part{ID: "p_bash", Type: "tool", Tool: "bash"})
	if cmd := m.maybeLoadTaskChildMessages("p_bash"); cmd != nil {
		t.Errorf("maybeLoadTaskChildMessages on a non-task part should return nil, got %v", cmd)
	}
}
