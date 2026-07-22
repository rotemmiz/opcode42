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

// TestBodyLines_CachedOnScroll verifies that cachedBodyLines returns the same
// []string on a cache hit (pure scroll — only m.scroll.Offset changes).
func TestBodyLines_CachedOnScroll(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m) // stop animating() so the cache is written
	m.ensureMDCache()
	innerW := m.width - 2*streamGutter

	lines1 := m.cachedBodyLines(m.cfg.SessionID, innerW)
	if len(lines1) == 0 {
		t.Fatal("first call returned no lines")
	}

	m.scroll.Back(3)

	lines2 := m.cachedBodyLines(m.cfg.SessionID, innerW)
	if len(lines1) != len(lines2) {
		t.Fatalf("cache miss on scroll: lines1=%d lines2=%d", len(lines1), len(lines2))
	}
	for i := range lines1 {
		if lines1[i] != lines2[i] {
			t.Fatalf("line %d differs on scroll:\n  was: %q\n  got: %q", i, lines1[i], lines2[i])
		}
	}
}

// TestBodyLines_InvalidatedOnStoreChange verifies that a store mutation
// (via Reduce) invalidates the cache.
func TestBodyLines_InvalidatedOnStoreChange(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	innerW := m.width - 2*streamGutter

	lines1 := m.cachedBodyLines(m.cfg.SessionID, innerW)
	v1 := m.store.version

	m.store = m.store.Reduce(ev("message.part.delta", map[string]any{
		"messageID": "msg_2", "partID": "prt_4", "field": "text", "delta": " Updated!",
	}))
	if m.store.version <= v1 {
		t.Fatal("store.version did not increment after Reduce")
	}

	lines2 := m.cachedBodyLines(m.cfg.SessionID, innerW)
	found := false
	for _, line := range lines2 {
		if strings.Contains(stripANSI(line), "Updated!") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cache miss did not rebuild: appended text not found")
	}
	_ = lines1
}

// TestBodyLines_InvalidatedOnViewToggle verifies that a view-state toggle
// (viewVersion++) invalidates the cache.
func TestBodyLines_InvalidatedOnViewToggle(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	innerW := m.width - 2*streamGutter

	lines1 := m.cachedBodyLines(m.cfg.SessionID, innerW)
	m.view.hideThinking = false
	m.viewVersion++
	lines2 := m.cachedBodyLines(m.cfg.SessionID, innerW)

	same := true
	if len(lines1) != len(lines2) {
		same = false
	} else {
		for i := range lines1 {
			if lines1[i] != lines2[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatal("view toggle did not invalidate cache — body is identical")
	}
}

// TestBodyLines_InvalidatedOnWidthChange verifies that a width change
// invalidates the cache.
func TestBodyLines_InvalidatedOnWidthChange(t *testing.T) {
	m := seededSessionModel(t)
	finalizeReasoning(&m)
	m.ensureMDCache()
	innerW := m.width - 2*streamGutter

	m.cachedBodyLines(m.cfg.SessionID, innerW)
	m.width = 120
	m.viewVersion++
	newInnerW := m.width - 2*streamGutter
	m.cachedBodyLines(m.cfg.SessionID, newInnerW)

	if len(m.bodyLinesCache) < 2 {
		t.Fatal("width change did not create a new cache entry")
	}
}

// finalizeReasoning sets Time.End on the seeded reasoning part so
// animating() returns false. Without this, the streaming reasoning part
// keeps animating() true, which prevents the cache from being written
// (plan 19 §2: the cache is not written during animation to avoid
// unbounded growth from the incrementing animFrame key).
func finalizeReasoning(m *Model) {
	for i, p := range m.store.parts["msg_2"] {
		if p.Type == "reasoning" {
			p.Time = PartTime{Start: 1000, End: 2500}
			m.store.parts["msg_2"][i] = p
		}
	}
}
