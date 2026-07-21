package tui

// subagent_count_test.go — plan 17 §E (Workstream E): the two-count
// subagent footer (active vs recent) + the childStatus == "" gap fix (E2a:
// derive status from the parent's task tool parts, wire-compatible with
// opencode's taskStatus).
//
// Acceptance tests from the plan:
//   - TestSubagentFooter_ShowsActiveVsRecent — 3 running + 14 completed →
//     "3 active"; 0 running + 17 completed → "17 recent"; 0 children → "".
//   - TestSubagentFooter_ChildStatusEmpty_NoUndercount — before any child
//     messages load, a running task tool part in the parent still drives the
//     active count (the E2 lazy-load gap).
//   - TestSubagentFooter_HiddenWhenNoChildren — the parent footer hides
//     entirely when there are no children.
//   - TestTaskChildStatusFromParent_MatchesOpencodeTaskStatus — the
//     parent-derived status mirrors opencode's taskStatus for each state
//     (running/completed/error/cancelled).
//   - TestSidebar_CancelledGlyph — an errored+interrupted task part shows
//     ○ (cancelled), not ✗ (error) — the E3 cancelled/error distinction.

import (
	"strings"
	"testing"
)

// seedParentWithTaskChildren builds a model with a parent session and n
// children, where each child is referenced by a task tool part in the parent's
// message stream with the given status. This is the shape opencode's TaskTool
// produces: the parent's task part carries metadata.sessionId pointing at the
// spawned child, and state.status reflects the task's run state. The children
// themselves have NO messages loaded (mirroring the pre-expand state), so the
// only status source is the parent's task parts — exercising E2a.
func seedParentWithTaskChildren(t *testing.T, n int, status string) Model {
	t.Helper()
	m := New(Config{URL: "http://x", SessionID: "ses_parent"})
	m.width, m.height = 120, 30
	m.screen = ScreenSession
	sessions := []Session{{ID: "ses_parent", Title: "Build the thing"}}
	msgs := []Message{{ID: "msg_parent", SessionID: "ses_parent", Role: "assistant"}}
	var parts []Part
	for i := 0; i < n; i++ {
		childID := "ses_child_" + string(rune('a'+i))
		sessions = append(sessions, Session{
			ID:       childID,
			ParentID: "ses_parent",
			Title:    "@general subagent: task " + string(rune('a'+i)),
		})
		parts = append(parts, Part{
			ID:        "p_task_" + string(rune('a'+i)),
			MessageID: "msg_parent",
			SessionID: "ses_parent",
			Type:      "tool",
			Tool:      "task",
			State:     taskStateJSON(t, status, "do work", "general", childID),
		})
	}
	m.store.sessions = sessions
	m.store.messages["ses_parent"] = msgs
	m.store.parts["msg_parent"] = parts
	return m
}

func TestSubagentFooter_ShowsActiveVsRecent(t *testing.T) {
	// 3 running + 14 completed → "3 active". The 3 running children have
	// task parts with status "running"; the 14 completed have "completed".
	m := New(Config{URL: "http://x", SessionID: "ses_parent"})
	m.width, m.height = 120, 30
	m.screen = ScreenSession
	sessions := []Session{{ID: "ses_parent", Title: "Build the thing"}}
	msgs := []Message{{ID: "msg_parent", SessionID: "ses_parent", Role: "assistant"}}
	var parts []Part
	for i := 0; i < 17; i++ {
		childID := "ses_child_" + string(rune('a'+i))
		sessions = append(sessions, Session{ID: childID, ParentID: "ses_parent",
			Title: "@general subagent: task " + string(rune('a'+i))})
		status := "completed"
		if i < 3 {
			status = "running"
		}
		parts = append(parts, Part{
			ID: "p_task_" + string(rune('a'+i)), MessageID: "msg_parent",
			SessionID: "ses_parent", Type: "tool", Tool: "task",
			State: taskStateJSON(t, status, "do work", "general", childID),
		})
	}
	m.store.sessions = sessions
	m.store.messages["ses_parent"] = msgs
	m.store.parts["msg_parent"] = parts

	got := m.subagentFooterView(80)
	if !strings.Contains(got, "3 active") {
		t.Fatalf("3 running + 14 completed → %q, want '3 active'", stripANSI(got))
	}

	// 0 running + 17 completed → "17 recent".
	m2 := seedParentWithTaskChildren(t, 17, "completed")
	got2 := m2.subagentFooterView(80)
	if !strings.Contains(got2, "17 recent") {
		t.Fatalf("0 running + 17 completed → %q, want '17 recent'", stripANSI(got2))
	}
	if strings.Contains(got2, "active") {
		t.Fatalf("no running children should not show 'active': %q", stripANSI(got2))
	}

	// 0 children → "" (hidden).
	m3 := New(Config{URL: "http://x", SessionID: "ses_solo"})
	m3.width, m3.height = 120, 30
	m3.screen = ScreenSession
	m3.store.sessions = []Session{{ID: "ses_solo", Title: "solo"}}
	if got := m3.subagentFooterView(80); got != "" {
		t.Fatalf("0 children → %q, want empty (hidden)", got)
	}
}

// TestSubagentFooter_ChildStatusEmpty_NoUndercount verifies the E2 fix: before
// any child messages load, a running task tool part in the parent still drives
// the active count. Without E2a (deriving from the parent's task parts), all
// children would have childStatus == "" and the count would read "0 active"
// (an undercount). With E2a, the running task parts produce "running" and the
// count is correct.
func TestSubagentFooter_ChildStatusEmpty_NoUndercount(t *testing.T) {
	m := seedParentWithTaskChildren(t, 2, "running")
	// Sanity: no child messages are loaded (the lazy-load gap).
	for _, s := range m.store.sessions {
		if s.ParentID != "" && len(m.store.messages[s.ID]) != 0 {
			t.Fatalf("precondition: child %q should have no messages loaded", s.ID)
		}
	}
	got := m.subagentFooterView(80)
	if !strings.Contains(got, "2 active") {
		t.Fatalf("2 running task parts with no child messages loaded → %q, want '2 active' (E2a: no undercount)",
			stripANSI(got))
	}
}

func TestSubagentFooter_HiddenWhenNoChildren(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_root"})
	m.width, m.height = 120, 30
	m.screen = ScreenSession
	m.store.sessions = []Session{{ID: "ses_root", Title: "root"}}
	if got := m.subagentFooterView(80); got != "" {
		t.Fatalf("root with no children → %q, want empty", got)
	}
}

// TestTaskChildStatusFromParent_MatchesOpencodeTaskStatus verifies the
// parent-derived status mirrors opencode's taskStatus
// (run/subagent-data.ts:295-309) for each state: running, pending→running,
// completed, error→error, error+interrupted→cancelled,
// error+"Tool execution aborted"→cancelled.
func TestTaskChildStatusFromParent_MatchesOpencodeTaskStatus(t *testing.T) {
	cases := []struct {
		name    string
		state   string
		meta    map[string]any
		errText string
		want    string
	}{
		{"running", "running", nil, "", "running"},
		{"pending→running", "pending", nil, "", "running"},
		{"empty→running", "", nil, "", "running"},
		{"completed", "completed", nil, "", "completed"},
		{"error", "error", nil, "boom", "error"},
		{"error+interrupted meta→cancelled", "error", map[string]any{"interrupted": true}, "x", "cancelled"},
		{"error+aborted text→cancelled", "error", nil, "Tool execution aborted", "cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(Config{URL: "http://x", SessionID: "ses_parent"})
			m.store.sessions = []Session{
				{ID: "ses_parent"},
				{ID: "ses_child", ParentID: "ses_parent"},
			}
			stateMap := map[string]any{
				"status": tc.state,
				"input":  map[string]any{"description": "x", "subagent_type": "general", "prompt": "x"},
			}
			if tc.errText != "" {
				stateMap["error"] = tc.errText
			}
			if tc.meta != nil {
				stateMap["metadata"] = tc.meta
			}
			// Ensure metadata.sessionId is set so childSessionID can match.
			if tc.meta == nil {
				tc.meta = map[string]any{}
			}
			tc.meta["sessionId"] = "ses_child"
			stateMap["metadata"] = tc.meta

			m.store.messages["ses_parent"] = []Message{{ID: "msg", SessionID: "ses_parent", Role: "assistant"}}
			m.store.parts["msg"] = []Part{{
				ID: "p", MessageID: "msg", Type: "tool", Tool: "task",
				State: rawState(t, stateMap),
			}}
			if got := m.childStatus("ses_child"); got != tc.want {
				t.Fatalf("childStatus = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSidebar_CancelledGlyph verifies the E3 cancelled/error distinction in
// the sidebar: an errored+interrupted task part shows ○ (cancelled), while a
// plain error shows ✗ (error).
func TestSidebar_CancelledGlyph(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_parent"})
	m.width, m.height = 120, 30
	m.screen = ScreenSession
	m.store.sessions = []Session{
		{ID: "ses_parent", Title: "parent"},
		{ID: "ses_child_cancelled", ParentID: "ses_parent", Title: "@general subagent: cancelled one"},
		{ID: "ses_child_errored", ParentID: "ses_parent", Title: "@general subagent: errored one"},
	}
	m.store.messages["ses_parent"] = []Message{{ID: "msg", SessionID: "ses_parent", Role: "assistant"}}
	m.store.parts["msg"] = []Part{
		{
			ID: "p_cancelled", MessageID: "msg", Type: "tool", Tool: "task",
			State: rawState(t, map[string]any{
				"status": "error",
				"error":  "Tool execution aborted",
				"input":  map[string]any{"description": "x", "subagent_type": "general", "prompt": "x"},
				"metadata": map[string]any{
					"sessionId":   "ses_child_cancelled",
					"interrupted": true,
				},
			}),
		},
		{
			ID: "p_errored", MessageID: "msg", Type: "tool", Tool: "task",
			State: rawState(t, map[string]any{
				"status": "error",
				"error":  "exit 1",
				"input":  map[string]any{"description": "x", "subagent_type": "general", "prompt": "x"},
				"metadata": map[string]any{
					"sessionId": "ses_child_errored",
				},
			}),
		},
	}

	plain := stripANSI(m.sidebarView())
	if !strings.Contains(plain, "○") {
		t.Errorf("cancelled child should show ○ in TASKS:\n%s", plain)
	}
	if !strings.Contains(plain, "✗") {
		t.Errorf("errored child should show ✗ in TASKS:\n%s", plain)
	}
}
