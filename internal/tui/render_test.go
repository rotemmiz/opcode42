package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

// Note: stripANSI is defined in ptypane_test.go (package-level, same test package).

func rawState(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func seededSessionModel(t *testing.T) Model {
	t.Helper()
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 100, 60
	m.store.sessions = []Session{{ID: "ses_1", Title: "Fix the bug"}}
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_1", SessionID: "ses_1", Role: "user"},
		{ID: "msg_2", SessionID: "ses_1", Role: "assistant"},
	}
	m.store.parts["msg_1"] = []Part{{ID: "prt_1", MessageID: "msg_1", Type: "text", Text: "fix the failing test"}}
	m.store.parts["msg_2"] = []Part{
		{ID: "prt_2", MessageID: "msg_2", Type: "reasoning", Text: "checking the test"},
		{ID: "prt_3", MessageID: "msg_2", Type: "tool", Tool: "read",
			// Pass filePath in input so toolHeader produces "Read main_test.go".
			State: rawState(t, map[string]any{
				"status": "completed",
				"title":  "main_test.go",
				"input":  map[string]any{"filePath": "main_test.go"},
			})},
		{ID: "prt_4", MessageID: "msg_2", Type: "text", Text: "All green now."},
	}
	return m
}

func TestRenderSession_ShowsAllBlockKinds(t *testing.T) {
	m := seededSessionModel(t)
	// Plan 17 §D1: reasoning now always renders a header (hide mode) or
	// header+body (show mode). Set show mode explicitly so the test asserts
	// the body content ("Thought" header is always present; the body is the
	// distinguishing factor between hide and show). The seeded reasoning
	// part has no Time.End so it renders as a streaming spinner — set a
	// finalized Time so the static "+ Thought" header shows.
	m.view.hideThinking = false
	for i, p := range m.store.parts["msg_2"] {
		if p.Type == "reasoning" {
			p.Time = PartTime{Start: 1000, End: 2500}
			m.store.parts["msg_2"][i] = p
		}
	}
	out := m.renderView()
	// Strip ANSI escapes before substring search: prose() now renders via
	// glamour which emits SGR codes in TTY environments.  The text content is
	// still present; stripping lets Contains find it reliably.
	// stripANSI is defined in ptypane_test.go (same test package).
	plain := stripANSI(out)
	for _, want := range []string{
		"Fix the bug",          // session title
		"fix the failing test", // user turn
		"Thought",              // reasoning header (show mode: "+ Thought")
		"Read",                 // tool header (per-tool name, capitalised by toolHeader)
		"main_test.go",         // salient arg extracted from input.filePath
		"All green now.",       // assistant prose
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("rendered session missing %q (checked plain text, ANSI stripped)", want)
		}
	}
}

func TestToolRow_ErrorIsRedWithMessage(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100
	row := m.toolRow(Part{Tool: "bash", Type: "tool",
		State: rawState(t, map[string]any{"status": "error", "error": "exit 1"})})
	// toolHeader capitalises the tool name ("Bash") and the error appears on a
	// sub-line prefixed with two spaces.
	if !strings.Contains(row, "Bash") || !strings.Contains(row, "exit 1") {
		t.Fatalf("error tool row wrong: %q", row)
	}
}

func TestFrame_TailScrollsToNewest(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.height = 4 // 3 body lines + a 1-line footer
	body := "l1\nl2\nl3\nl4\nl5"
	out := m.frame(body, "status")
	if strings.Contains(out, "l1") || !strings.Contains(out, "l5") {
		t.Fatalf("frame should show the tail, got:\n%s", out)
	}
}

func TestIngestHistory_PopulatesStore(t *testing.T) {
	s := newStore()
	s = s.ingestHistory("ses_1", []wireWithParts{
		{Info: Message{ID: "msg_1", SessionID: "ses_1", Role: "assistant"},
			Parts: []Part{{ID: "prt_1", MessageID: "msg_1", Type: "text", Text: "hi"}}},
	})
	if len(s.messages["ses_1"]) != 1 || len(s.parts["msg_1"]) != 1 || s.parts["msg_1"][0].Text != "hi" {
		t.Fatalf("history not ingested: %+v", s)
	}
}

func TestRenderSession_SurfacesAssistantError(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width, m.height = 100, 40
	a := Message{ID: "msg_1", SessionID: "ses_1", Role: "assistant"}
	a.Error = &MsgError{Name: "ProviderAuthError"}
	a.Error.Data.Message = "Google Generative AI API key is missing."
	m.store.messages["ses_1"] = []Message{a}
	plain := stripANSI(m.renderView())
	for _, want := range []string{"ProviderAuthError", "API key is missing"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("assistant error not surfaced (%q) in plain output", want)
		}
	}
}

// TestBodyLines_StableOnScroll verifies that the pre-rendered body lines
// (m.bodyLines) are unchanged across pure scroll (plan 20 §4). renderBodyLines
// is called once in Update; View just windows the pre-rendered slice via
// frameStreamLines — zero rendering on scroll. The test re-renders once,
// scrolls, and asserts the lines are byte-identical (no re-render happened).
func TestBodyLines_StableOnScroll(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	m = m.rerenderFull()

	lines1 := append([]string(nil), m.bodyLines...)
	if len(lines1) == 0 {
		t.Fatal("renderBodyLines produced no lines")
	}

	// Pure scroll: only m.scroll.Offset changes. The pre-rendered body is NOT
	// recomputed (Update isn't called), so m.bodyLines is unchanged.
	m.scroll.Back(3)

	if len(m.bodyLines) != len(lines1) {
		t.Fatalf("body lines changed on scroll: was=%d got=%d", len(lines1), len(m.bodyLines))
	}
	for i := range lines1 {
		if m.bodyLines[i] != lines1[i] {
			t.Fatalf("line %d differs on scroll:\n  was: %q\n  got: %q", i, lines1[i], m.bodyLines[i])
		}
	}
}

// TestBodyLines_UpdatedOnStoreChange verifies that a store mutation followed
// by rerenderFull() rebuilds the pre-rendered body (plan 20 §4). The
// rendered body reflects the new content.
func TestBodyLines_UpdatedOnStoreChange(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	m = m.rerenderFull()

	v1 := m.store.version
	m.store = m.store.Reduce(ev("message.part.delta", map[string]any{
		"messageID": "msg_2", "partID": "prt_4", "field": "text", "delta": " Updated!",
	}))
	if m.store.version <= v1 {
		t.Fatal("store.version did not increment after Reduce")
	}
	m = m.rerenderFull()

	found := false
	for _, line := range m.bodyLines {
		if strings.Contains(stripANSI(line), "Updated!") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("rerenderFull did not rebuild: appended text not found in m.bodyLines")
	}
}

// TestBodyLines_UpdatedOnViewToggle verifies that a view-state toggle
// followed by rerenderFull() rebuilds the pre-rendered body (plan 20 §4).
// The rendered body reflects the new view state (e.g. thinking shown).
func TestBodyLines_UpdatedOnViewToggle(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	m = m.rerenderFull()
	lines1 := append([]string(nil), m.bodyLines...)

	m.view.hideThinking = false
	m = m.rerenderFull()

	same := true
	if len(lines1) != len(m.bodyLines) {
		same = false
	} else {
		for i := range lines1 {
			if m.bodyLines[i] != lines1[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatal("view toggle + rerenderFull did not rebuild — body is identical")
	}
}

// TestBodyLines_UpdatedOnWidthChange verifies that a width change followed
// by rerenderFull() rebuilds the pre-rendered body at the new width.
func TestBodyLines_UpdatedOnWidthChange(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	m = m.rerenderFull()
	lines1 := append([]string(nil), m.bodyLines...)

	m.width = 120
	m = m.rerenderFull()

	// The body should have been re-rendered at the new width. A different
	// width produces different wrapping → at least one line differs (or the
	// line count differs).
	same := true
	if len(lines1) != len(m.bodyLines) {
		same = false
	} else {
		for i := range lines1 {
			if m.bodyLines[i] != lines1[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatal("width change + rerenderFull did not rebuild — body is identical")
	}
}

// finalizeReasoning sets Time.End on the seeded reasoning part so
// animating() returns false, putting the model in a deterministic idle
// state for cache comparison.
func finalizeReasoning(m *Model) {
	for i, p := range m.store.parts["msg_2"] {
		if p.Type == "reasoning" {
			p.Time = PartTime{Start: 1000, End: 2500}
			m.store.parts["msg_2"][i] = p
		}
	}
}

// TestSidebar_StableOnScroll verifies that the pre-rendered sidebar string
// (m.sidebarRendered) is unchanged across pure scroll (plan 20 §3). The
// sidebar is rendered once in Update; View composites the pre-rendered
// string directly — zero rendering on scroll.
func TestSidebar_StableOnScroll(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	m.sidebarHidden = false // sidebar visible
	m = m.rerenderFull()

	s1 := m.sidebarRendered
	if s1 == "" {
		t.Fatal("renderSidebar produced an empty string")
	}

	// Pure scroll: only m.scroll.Offset changes. The pre-rendered sidebar is
	// NOT recomputed, so m.sidebarRendered is unchanged.
	m.scroll.Back(3)

	if m.sidebarRendered != s1 {
		t.Fatal("sidebar changed on scroll — should be byte-identical (pre-rendered)")
	}
}

// TestSidebar_UpdatedOnStoreChange verifies that a store mutation followed
// by rerenderFull() rebuilds the pre-rendered sidebar (plan 20 §3).
func TestSidebar_UpdatedOnStoreChange(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	m.sidebarHidden = false
	m = m.rerenderFull()
	s1 := m.sidebarRendered

	v1 := m.store.version
	// Apply a store change (new session.updated event — changes the sidebar's
	// session title + token display).
	m.store = m.store.Reduce(ev("session.updated", map[string]any{"info": map[string]any{
		"id": "ses_1", "title": "Updated title",
	}}))
	if m.store.version <= v1 {
		t.Fatal("store.version did not increment")
	}
	m = m.rerenderFull()

	// The sidebar should have been rebuilt (the title changed → the sidebar
	// content differs).
	if m.sidebarRendered == s1 {
		t.Fatal("store change + rerenderFull did not rebuild the sidebar")
	}
}

// TestChildStatusMap_RecomputedOnStoreChange verifies that
// recomputeChildStatuses builds the childStatusMap from the store's task
// tool parts, and that childStatus reads from the map (plan 20 §1a).
// With 52 subagents this is the difference between 75% of CPU (per-child
// per-frame JSON decode) and O(1) map lookup.
func TestChildStatusMap_RecomputedOnStoreChange(t *testing.T) {
	m := seedParentWithTaskChildren(t, 3, "running")
	// Before recompute, the map is empty (childStatus falls back to the
	// per-child scan).
	m.childStatusMap = nil
	if m.childStatus("ses_child_a") == "" {
		t.Fatal("childStatus fallback should still work with a nil map")
	}

	// After recompute, the map is populated and childStatus reads from it.
	m = m.recomputeChildStatuses()
	if len(m.childStatusMap) != 3 {
		t.Fatalf("recomputeChildStatuses should populate 3 children, got %d", len(m.childStatusMap))
	}
	for _, cid := range []string{"ses_child_a", "ses_child_b", "ses_child_c"} {
		if got := m.childStatus(cid); got != "running" {
			t.Fatalf("childStatus(%q) = %q, want %q (from map)", cid, got, "running")
		}
	}

	// A store change (one child completes) → recompute → map reflects the
	// new status.
	m.store = m.store.Reduce(ev("message.part.updated", map[string]any{
		"part": map[string]any{
			"id": "p_task_a", "messageID": "msg_parent",
			"type": "tool", "tool": "task",
			"state": map[string]any{
				"status": "completed",
				"input":  map[string]any{"description": "do work", "subagent_type": "general", "prompt": "x"},
				"metadata": map[string]any{
					"sessionId":       "ses_child_a",
					"parentSessionId": "ses_parent",
				},
			},
		},
	}))
	m = m.recomputeChildStatuses()
	if got := m.childStatus("ses_child_a"); got != "completed" {
		t.Fatalf("after store change + recompute, childStatus(ses_child_a) = %q, want %q", got, "completed")
	}
	if got := m.childStatus("ses_child_b"); got != "running" {
		t.Fatalf("unchanged child: childStatus(ses_child_b) = %q, want %q", got, "running")
	}
}

// TestView_NoRenderingOnScroll verifies that View() performs no rendering
// when only the scroll offset changes (plan 20 acceptance test). The
// pre-rendered buffers (m.bodyLines, m.footerRendered, m.sidebarRendered)
// are unchanged across pure scroll; View just windows the pre-rendered
// slice via frameStreamLines and composites the pre-rendered strings.
//
// This test asserts the invariant by snapshotting the pre-rendered buffers,
// scrolling, and verifying the buffers are byte-identical. The View output
// changes (the windowed slice differs) but the pre-rendered source does not.
func TestView_NoRenderingOnScroll(t *testing.T) {
	m := longSessionModel(t)
	// Disable the sidebar so the test focuses on the body + footer (the
	// sidebar adds width-dependent content that shifts on resize).
	m.sidebarHidden = true
	m.ensureMDCache()
	m = m.rerenderFull()

	// Snapshot the pre-rendered buffers.
	bodyLines := append([]string(nil), m.bodyLines...)
	footer := m.footerRendered
	footerH := m.footerHeight

	if len(bodyLines) == 0 {
		t.Fatal("renderBodyLines produced no lines")
	}

	// Pure scroll: only m.scroll.Offset changes. No Update is called, so no
	// re-render happens. The pre-rendered buffers must be unchanged.
	m.scroll.Back(5)

	if len(m.bodyLines) != len(bodyLines) {
		t.Fatalf("bodyLines changed on scroll: was=%d got=%d", len(bodyLines), len(m.bodyLines))
	}
	for i := range bodyLines {
		if m.bodyLines[i] != bodyLines[i] {
			t.Fatalf("bodyLines[%d] changed on scroll — View should not re-render", i)
		}
	}
	if m.footerRendered != footer {
		t.Fatal("footerRendered changed on scroll — View should not re-render")
	}
	if m.footerHeight != footerH {
		t.Fatal("footerHeight changed on scroll — View should not re-render")
	}

	// The View output should still be correct (the windowed slice reflects
	// the new scroll offset). The scrolled view should differ from the tail
	// view — if it doesn't, scroll isn't working.
	m0 := m
	m0.scroll.Offset = 0
	tail := stripANSI(m0.composeView())
	m.scroll.Offset = 1000 // clamped to the top
	back := stripANSI(m.composeView())
	if tail == back {
		t.Fatal("scrolling changed nothing — View output is identical at tail and top")
	}
}
