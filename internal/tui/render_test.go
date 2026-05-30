package tui

import (
	"encoding/json"
	"strings"
	"testing"
)

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
			State: rawState(t, map[string]any{"status": "completed", "title": "main_test.go"})},
		{ID: "prt_4", MessageID: "msg_2", Type: "text", Text: "All green now."},
	}
	return m
}

func TestRenderSession_ShowsAllBlockKinds(t *testing.T) {
	out := seededSessionModel(t).View()
	for _, want := range []string{
		"Fix the bug",          // session title
		"fix the failing test", // user turn
		"Thought",              // reasoning line
		"read",                 // tool name
		"main_test.go",         // tool title
		"All green now.",       // assistant prose
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered session missing %q in:\n%s", want, out)
		}
	}
}

func TestToolRow_ErrorIsRedWithMessage(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100
	row := m.toolRow(Part{Tool: "bash", Type: "tool",
		State: rawState(t, map[string]any{"status": "error", "error": "exit 1"})})
	if !strings.Contains(row, "bash") || !strings.Contains(row, "exit 1") {
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
	out := m.View()
	for _, want := range []string{"ProviderAuthError", "API key is missing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("assistant error not surfaced (%q) in:\n%s", want, out)
		}
	}
}
