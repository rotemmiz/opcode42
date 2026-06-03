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
	out := seededSessionModel(t).View()
	// Strip ANSI escapes before substring search: prose() now renders via
	// glamour which emits SGR codes in TTY environments.  The text content is
	// still present; stripping lets Contains find it reliably.
	// stripANSI is defined in ptypane_test.go (same test package).
	plain := stripANSI(out)
	for _, want := range []string{
		"Fix the bug",          // session title
		"fix the failing test", // user turn
		"Thought",              // reasoning line (collapsed one-liner header)
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
	m.width = 120 // wide enough that the error line fits without word-wrapping
	a := Message{ID: "msg_1", SessionID: "ses_1", Role: "assistant"}
	a.Error = &MsgError{Name: "ProviderAuthError"}
	a.Error.Data.Message = "Google Generative AI API key is missing."
	m.store.messages["ses_1"] = []Message{a}
	plain := stripANSI(m.View())
	for _, want := range []string{"ProviderAuthError", "API key is missing"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("assistant error not surfaced (%q) in plain output", want)
		}
	}
}
