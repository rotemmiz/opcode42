package message

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

// Ports opencode's message-v2.test.ts "toModelMessage" suite to Opcode42's
// provider-neutral ModelMessage shape. Where opencode keeps tool-result media
// inline for some providers, Opcode42 uniformly promotes it to a trailing user
// message (documented divergence in ToModelMessages); those provider-transform
// cases are intentionally not ported here.

const testProvider = "test"
const testModelID = "test-model"

func testModel() SerializeModel {
	return SerializeModel{ProviderID: testProvider, ModelID: testModelID}
}

func rawState(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return b
}

func userMsg(id string, parts ...Part) WithParts {
	u := &UserMessage{ID: id, SessionID: "session", Role: RoleUser, Agent: "user",
		Model: Model{ProviderID: testProvider, ModelID: "test"}}
	return WithParts{Info: Info{User: u}, Parts: parts}
}

func assistantMsg(id, parentID string, parts ...Part) WithParts {
	a := &AssistantMessage{ID: id, SessionID: "session", Role: RoleAssistant, ParentID: parentID,
		ProviderID: testProvider, ModelID: testModelID, Agent: "agent",
		Path: Path{CWD: "/", Root: "/"}}
	return WithParts{Info: Info{Assistant: a}, Parts: parts}
}

func base(msgID, partID string) PartBase {
	return PartBase{ID: partID, SessionID: "session", MessageID: msgID}
}

func assertMessages(t *testing.T, got, want []llm.ModelMessage) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.MarshalIndent(got, "", "  ")
		wj, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("ModelMessages mismatch\n got: %s\nwant: %s", gj, wj)
	}
}

func TestToModelMessages_FiltersEmptyAndIgnored(t *testing.T) {
	in := []WithParts{
		userMsg("msg_empty"),
		userMsg("msg_user", &TextPart{PartBase: base("msg_user", "prt_1"), Type: "text", Text: "hello"}),
	}
	want := []llm.ModelMessage{{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "hello"}}}}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), want)
}

func TestToModelMessages_OnlyIgnoredOrEmptyUserDropped(t *testing.T) {
	in := []WithParts{userMsg("msg_user",
		&TextPart{PartBase: base("msg_user", "prt_1"), Type: "text", Text: "ignored", Ignored: true},
		&TextPart{PartBase: base("msg_user", "prt_2"), Type: "text", Text: ""},
	)}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), nil)
}

func TestToModelMessages_FiltersEmptyKeepsNonEmpty(t *testing.T) {
	in := []WithParts{userMsg("msg_user",
		&TextPart{PartBase: base("msg_user", "prt_1"), Type: "text", Text: ""},
		&TextPart{PartBase: base("msg_user", "prt_2"), Type: "text", Text: "hello"},
	)}
	want := []llm.ModelMessage{{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "hello"}}}}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), want)
}

func TestToModelMessages_SyntheticTextIncluded(t *testing.T) {
	in := []WithParts{
		userMsg("msg_user", &TextPart{PartBase: base("msg_user", "prt_1"), Type: "text", Text: "hello", Synthetic: true}),
		assistantMsg("msg_assistant", "msg_user", &TextPart{PartBase: base("msg_assistant", "prt_a1"), Type: "text", Text: "assistant", Synthetic: true}),
	}
	want := []llm.ModelMessage{
		{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "hello"}}},
		{Role: llm.RoleAssistant, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "assistant"}}},
	}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), want)
}

func TestToModelMessages_UserFilesAndPrompts(t *testing.T) {
	in := []WithParts{userMsg("msg_user",
		&TextPart{PartBase: base("msg_user", "prt_1"), Type: "text", Text: "hello"},
		&TextPart{PartBase: base("msg_user", "prt_2"), Type: "text", Text: "ignored", Ignored: true},
		&FilePart{PartBase: base("msg_user", "prt_3"), Type: "file", MIME: "image/png", Filename: "img.png", URL: "https://example.com/img.png"},
		&FilePart{PartBase: base("msg_user", "prt_4"), Type: "file", MIME: "text/plain", Filename: "note.txt", URL: "https://example.com/note.txt"},
		&FilePart{PartBase: base("msg_user", "prt_5"), Type: "file", MIME: "application/x-directory", Filename: "dir", URL: "https://example.com/dir"},
		&CompactionPart{PartBase: base("msg_user", "prt_6"), Type: "compaction", Auto: true},
		&SubtaskPart{PartBase: base("msg_user", "prt_7"), Type: "subtask", Prompt: "p", Description: "d", Agent: "agent"},
	)}
	want := []llm.ModelMessage{{Role: llm.RoleUser, Content: []llm.ContentPart{
		{Kind: llm.ContentText, Text: "hello"},
		{Kind: llm.ContentFile, MediaType: "image/png", Filename: "img.png", URL: "https://example.com/img.png"},
		{Kind: llm.ContentText, Text: "What did we do so far?"},
		{Kind: llm.ContentText, Text: "The following tool was executed by the user"},
	}}}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), want)
}

func TestToModelMessages_ToolCompletion(t *testing.T) {
	state := ToolStateCompleted{Status: "completed", Input: map[string]any{"cmd": "ls"}, Output: "ok", Title: "Bash", Metadata: map[string]any{}}
	state.Time.Start, state.Time.End = 0, 1
	in := []WithParts{
		userMsg("msg_user", &TextPart{PartBase: base("msg_user", "prt_u1"), Type: "text", Text: "run tool"}),
		assistantMsg("msg_assistant", "msg_user",
			&TextPart{PartBase: base("msg_assistant", "prt_a1"), Type: "text", Text: "done", Metadata: map[string]any{"openai": map[string]any{"assistant": "meta"}}},
			&ToolPart{PartBase: base("msg_assistant", "prt_a2"), Type: "tool", CallID: "call-1", Tool: "bash",
				State: rawState(t, state), Metadata: map[string]any{"openai": map[string]any{"tool": "meta"}}},
		),
	}
	toolMeta := map[string]any{"openai": map[string]any{"tool": "meta"}}
	want := []llm.ModelMessage{
		{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "run tool"}}},
		{Role: llm.RoleAssistant, Content: []llm.ContentPart{
			{Kind: llm.ContentText, Text: "done", ProviderMetadata: map[string]any{"openai": map[string]any{"assistant": "meta"}}},
			{Kind: llm.ContentToolCall, ToolCallID: "call-1", ToolName: "bash", Input: map[string]any{"cmd": "ls"}, ProviderMetadata: toolMeta},
		}},
		{Role: llm.RoleTool, Content: []llm.ContentPart{
			{Kind: llm.ContentToolResult, ToolCallID: "call-1", ToolName: "bash", Input: map[string]any{"cmd": "ls"}, Output: "ok", ProviderMetadata: toolMeta},
		}},
	}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), want)
}

func TestToModelMessages_DifferentModelOmitsMetadata(t *testing.T) {
	state := ToolStateCompleted{Status: "completed", Input: map[string]any{"cmd": "ls"}, Output: "ok", Title: "Bash", Metadata: map[string]any{}}
	state.Time.End = 1
	wp := assistantMsg("msg_assistant", "msg_user",
		&TextPart{PartBase: base("msg_assistant", "prt_a1"), Type: "text", Text: "done", Metadata: map[string]any{"openai": map[string]any{"assistant": "meta"}}},
		&ReasoningPart{PartBase: base("msg_assistant", "prt_a2"), Type: "reasoning", Text: "thinking", Metadata: map[string]any{"openai": map[string]any{"reasoning": "meta"}}},
		&ToolPart{PartBase: base("msg_assistant", "prt_a3"), Type: "tool", CallID: "call-1", Tool: "bash", State: rawState(t, state), Metadata: map[string]any{"openai": map[string]any{"tool": "meta"}}},
	)
	wp.Info.Assistant.ProviderID = "other"
	wp.Info.Assistant.ModelID = "other"
	in := []WithParts{userMsg("msg_user", &TextPart{PartBase: base("msg_user", "prt_u1"), Type: "text", Text: "run tool"}), wp}
	want := []llm.ModelMessage{
		{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "run tool"}}},
		{Role: llm.RoleAssistant, Content: []llm.ContentPart{
			{Kind: llm.ContentText, Text: "done"},
			{Kind: llm.ContentText, Text: "thinking"}, // reasoning -> text when different model
			{Kind: llm.ContentToolCall, ToolCallID: "call-1", ToolName: "bash", Input: map[string]any{"cmd": "ls"}},
		}},
		{Role: llm.RoleTool, Content: []llm.ContentPart{
			{Kind: llm.ContentToolResult, ToolCallID: "call-1", ToolName: "bash", Input: map[string]any{"cmd": "ls"}, Output: "ok"},
		}},
	}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), want)
}

func TestToModelMessages_CompactedOutputPlaceholder(t *testing.T) {
	compacted := int64(1)
	state := ToolStateCompleted{Status: "completed", Input: map[string]any{"cmd": "ls"}, Output: "cleared me", Title: "Bash", Metadata: map[string]any{}}
	state.Time.End, state.Time.Compacted = 1, &compacted
	in := []WithParts{assistantMsg("msg_assistant", "msg_parent",
		&ToolPart{PartBase: base("msg_assistant", "prt_a1"), Type: "tool", CallID: "call-1", Tool: "bash", State: rawState(t, state)},
	)}
	got := ToModelMessages(in, testModel(), SerializeOptions{})
	if got[1].Content[0].Output != "[Old tool result content cleared]" {
		t.Fatalf("want placeholder, got %q", got[1].Content[0].Output)
	}
}

func TestToModelMessages_Truncation(t *testing.T) {
	state := ToolStateCompleted{Status: "completed", Input: map[string]any{"cmd": "ls"}, Output: "abcdefghij", Title: "Shell", Metadata: map[string]any{}}
	state.Time.End = 1
	in := []WithParts{assistantMsg("msg_assistant", "msg_parent",
		&ToolPart{PartBase: base("msg_assistant", "prt_a1"), Type: "tool", CallID: "call-1", Tool: "bash", State: rawState(t, state)},
	)}
	got := ToModelMessages(in, testModel(), SerializeOptions{ToolOutputMaxChars: 4})
	want := "abcd\n[Tool output truncated for compaction: omitted 6 chars]"
	if got[1].Content[0].Output != want {
		t.Fatalf("want %q, got %q", want, got[1].Content[0].Output)
	}
}

func TestToModelMessages_ToolError(t *testing.T) {
	state := ToolStateError{Status: "error", Input: map[string]any{"cmd": "ls"}, Error: "nope", Metadata: map[string]any{}}
	state.Time.End = 1
	in := []WithParts{assistantMsg("msg_assistant", "msg_parent",
		&ToolPart{PartBase: base("msg_assistant", "prt_a1"), Type: "tool", CallID: "call-1", Tool: "bash", State: rawState(t, state)},
	)}
	got := ToModelMessages(in, testModel(), SerializeOptions{})
	res := got[1].Content[0]
	if !res.IsError || res.Output != "nope" {
		t.Fatalf("want error-text 'nope', got isError=%v output=%q", res.IsError, res.Output)
	}
}

func TestToModelMessages_InterruptedForwardsOutput(t *testing.T) {
	state := ToolStateError{Status: "error", Input: map[string]any{"command": "x"}, Error: "Tool execution aborted",
		Metadata: map[string]any{"interrupted": true, "output": "partial-output"}}
	state.Time.End = 1
	in := []WithParts{assistantMsg("msg_assistant", "msg_parent",
		&ToolPart{PartBase: base("msg_assistant", "prt_a1"), Type: "tool", CallID: "call-1", Tool: "bash", State: rawState(t, state)},
	)}
	got := ToModelMessages(in, testModel(), SerializeOptions{})
	res := got[1].Content[0]
	if res.IsError || res.Output != "partial-output" {
		t.Fatalf("want forwarded output, got isError=%v output=%q", res.IsError, res.Output)
	}
}

func TestToModelMessages_NonAbortErrorDropped(t *testing.T) {
	wp := assistantMsg("msg_assistant", "msg_parent", &TextPart{PartBase: base("msg_assistant", "prt_a1"), Type: "text", Text: "nope"})
	wp.Info.Assistant.Error = &Error{Name: "APIError", Data: map[string]any{"message": "boom"}}
	assertMessages(t, ToModelMessages([]WithParts{wp}, testModel(), SerializeOptions{}), nil)
}

func TestToModelMessages_AbortedIncludedOnlyWithRealContent(t *testing.T) {
	withContent := assistantMsg("msg_assistant_1", "msg_parent",
		&ReasoningPart{PartBase: base("msg_assistant_1", "prt_a1"), Type: "reasoning", Text: "thinking"},
		&TextPart{PartBase: base("msg_assistant_1", "prt_a2"), Type: "text", Text: "partial answer"},
	)
	withContent.Info.Assistant.Error = &Error{Name: abortedErrorName}
	onlyMeta := assistantMsg("msg_assistant_2", "msg_parent",
		&StepStartPart{PartBase: base("msg_assistant_2", "prt_b1"), Type: "step-start"},
		&ReasoningPart{PartBase: base("msg_assistant_2", "prt_b2"), Type: "reasoning", Text: "thinking"},
	)
	onlyMeta.Info.Assistant.Error = &Error{Name: abortedErrorName}
	want := []llm.ModelMessage{{Role: llm.RoleAssistant, Content: []llm.ContentPart{
		{Kind: llm.ContentReasoning, Text: "thinking"},
		{Kind: llm.ContentText, Text: "partial answer"},
	}}}
	assertMessages(t, ToModelMessages([]WithParts{withContent, onlyMeta}, testModel(), SerializeOptions{}), want)
}

func TestToModelMessages_StepStartSplits(t *testing.T) {
	in := []WithParts{assistantMsg("msg_assistant", "msg_parent",
		&TextPart{PartBase: base("msg_assistant", "prt_1"), Type: "text", Text: "first"},
		&StepStartPart{PartBase: base("msg_assistant", "prt_2"), Type: "step-start"},
		&TextPart{PartBase: base("msg_assistant", "prt_3"), Type: "text", Text: "second"},
	)}
	want := []llm.ModelMessage{
		{Role: llm.RoleAssistant, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "first"}}},
		{Role: llm.RoleAssistant, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "second"}}},
	}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), want)
}

func TestToModelMessages_StepStartOnlyDropped(t *testing.T) {
	in := []WithParts{assistantMsg("msg_assistant", "msg_parent",
		&StepStartPart{PartBase: base("msg_assistant", "prt_1"), Type: "step-start"},
	)}
	assertMessages(t, ToModelMessages(in, testModel(), SerializeOptions{}), nil)
}

func TestToModelMessages_PendingRunningInterrupted(t *testing.T) {
	pending := ToolStatePending{Status: "pending", Input: map[string]any{"cmd": "ls"}, Raw: ""}
	running := ToolStateRunning{Status: "running", Input: map[string]any{"path": "/tmp"}}
	in := []WithParts{
		userMsg("msg_user", &TextPart{PartBase: base("msg_user", "prt_u1"), Type: "text", Text: "run tool"}),
		assistantMsg("msg_assistant", "msg_user",
			&ToolPart{PartBase: base("msg_assistant", "prt_a1"), Type: "tool", CallID: "call-pending", Tool: "bash", State: rawState(t, pending)},
			&ToolPart{PartBase: base("msg_assistant", "prt_a2"), Type: "tool", CallID: "call-running", Tool: "read", State: rawState(t, running)},
		),
	}
	got := ToModelMessages(in, testModel(), SerializeOptions{})
	tool := got[2]
	if tool.Role != llm.RoleTool || len(tool.Content) != 2 {
		t.Fatalf("want tool message with 2 results, got %+v", tool)
	}
	for _, r := range tool.Content {
		if !r.IsError || r.Output != "[Tool execution was interrupted]" {
			t.Fatalf("want interrupted error result, got %+v", r)
		}
	}
}

func TestToModelMessages_SignedReasoningEmptyTextBecomesSpace(t *testing.T) {
	in := []WithParts{assistantMsg("msg_assistant", "msg_parent",
		&StepStartPart{PartBase: base("msg_assistant", "prt_1"), Type: "step-start"},
		&ReasoningPart{PartBase: base("msg_assistant", "prt_2"), Type: "reasoning", Text: "thinking-one", Metadata: map[string]any{"anthropic": map[string]any{"signature": "sig1"}}},
		&TextPart{PartBase: base("msg_assistant", "prt_3"), Type: "text", Text: ""},
		&StepStartPart{PartBase: base("msg_assistant", "prt_4"), Type: "step-start"},
		&ReasoningPart{PartBase: base("msg_assistant", "prt_5"), Type: "reasoning", Text: "thinking-two", Metadata: map[string]any{"anthropic": map[string]any{"signature": "sig2"}}},
		&TextPart{PartBase: base("msg_assistant", "prt_6"), Type: "text", Text: "the answer"},
	)}
	got := ToModelMessages(in, testModel(), SerializeOptions{})
	if len(got) != 2 {
		t.Fatalf("want 2 assistant messages, got %d", len(got))
	}
	if firstText(got[0]) != " " {
		t.Fatalf("want space separator, got %q", firstText(got[0]))
	}
	if firstText(got[1]) != "the answer" {
		t.Fatalf("want 'the answer', got %q", firstText(got[1]))
	}
}

func TestToModelMessages_UnsignedReasoningKeepsEmptyText(t *testing.T) {
	in := []WithParts{assistantMsg("msg_assistant", "msg_parent",
		&ReasoningPart{PartBase: base("msg_assistant", "prt_1"), Type: "reasoning", Text: "thinking"},
		&TextPart{PartBase: base("msg_assistant", "prt_2"), Type: "text", Text: ""},
		&TextPart{PartBase: base("msg_assistant", "prt_3"), Type: "text", Text: "answer"},
	)}
	got := ToModelMessages(in, testModel(), SerializeOptions{})
	var texts []string
	for _, c := range got[0].Content {
		if c.Kind == llm.ContentText {
			texts = append(texts, c.Text)
		}
	}
	if !reflect.DeepEqual(texts, []string{"", "answer"}) {
		t.Fatalf("want ['','answer'], got %v", texts)
	}
}

func firstText(m llm.ModelMessage) string {
	for _, c := range m.Content {
		if c.Kind == llm.ContentText {
			return c.Text
		}
	}
	return ""
}
