package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
)

// sseServer returns a server that replays the given SSE body for one request,
// plus a pointer to capture the decoded request body.
func sseServer(t *testing.T, body string, captured *chatRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, captured); err != nil {
				t.Errorf("decode request: %v", err)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
}

func collect(t *testing.T, c *Client, req *llm.Request) []llm.Event {
	t.Helper()
	ch, err := c.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var out []llm.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func types(events []llm.Event) []llm.EventType {
	out := make([]llm.EventType, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

const textOnlySSE = `data: {"choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2}}}

data: [DONE]

`

func TestStream_TextOnly(t *testing.T) {
	srv := sseServer(t, textOnlySSE, nil)
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "gpt-4o", HTTPClient: srv.Client()})

	events := collect(t, c, &llm.Request{Messages: []llm.ModelMessage{
		{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "hi"}}},
	}})

	want := []llm.EventType{
		llm.EventStepStart, llm.EventTextStart, llm.EventTextDelta, llm.EventTextDelta,
		llm.EventTextEnd, llm.EventStepFinish, llm.EventFinish,
	}
	if got := types(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if events[2].Text != "Hello" || events[3].Text != " world" {
		t.Fatalf("text deltas wrong: %q %q", events[2].Text, events[3].Text)
	}
	sf := events[5]
	if sf.Reason != "stop" || sf.Usage == nil {
		t.Fatalf("step-finish wrong: %+v", sf)
	}
	if sf.Usage.Input != 8 || sf.Usage.Output != 5 || sf.Usage.CacheRead != 2 {
		t.Fatalf("usage mapping wrong: %+v", *sf.Usage)
	}
}

const toolCallSSE = `data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"SF\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: {"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":8}}

data: [DONE]

`

func TestStream_ToolCall(t *testing.T) {
	srv := sseServer(t, toolCallSSE, nil)
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "gpt-4o", HTTPClient: srv.Client()})

	events := collect(t, c, &llm.Request{})
	want := []llm.EventType{
		llm.EventStepStart, llm.EventToolInputStart, llm.EventToolInputDelta, llm.EventToolInputDelta,
		llm.EventToolInputEnd, llm.EventToolCall, llm.EventStepFinish, llm.EventFinish,
	}
	if got := types(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	start := events[1]
	if start.ID != "call_1" || start.Name != "get_weather" {
		t.Fatalf("tool-input-start wrong: %+v", start)
	}
	call := events[5]
	if call.ID != "call_1" || call.Name != "get_weather" {
		t.Fatalf("tool-call ids wrong: %+v", call)
	}
	if city, _ := call.Input["city"].(string); city != "SF" {
		t.Fatalf("tool-call input wrong: %+v", call.Input)
	}
	if events[6].Reason != "tool-calls" {
		t.Fatalf("finish reason = %q, want tool-calls", events[6].Reason)
	}
}

func TestStream_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down"}}`)
	}))
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "gpt-4o", HTTPClient: srv.Client()})

	events := collect(t, c, &llm.Request{})
	if len(events) != 1 || events[0].Type != llm.EventProviderError {
		t.Fatalf("want single provider-error, got %v", types(events))
	}
	if events[0].StatusCode != 429 || events[0].Message == "" {
		t.Fatalf("provider-error fields wrong: %+v", events[0])
	}
}

func TestBuildRequest_RendersRolesAndTools(t *testing.T) {
	var captured chatRequest
	srv := sseServer(t, "data: [DONE]\n\n", &captured)
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "gpt-4o", APIKey: "sk-test", HTTPClient: srv.Client()})

	req := &llm.Request{
		SystemPrompts: []string{"be terse"},
		Messages: []llm.ModelMessage{
			{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "weather?"}}},
			{Role: llm.RoleAssistant, Content: []llm.ContentPart{
				{Kind: llm.ContentText, Text: "checking"},
				{Kind: llm.ContentToolCall, ToolCallID: "call_1", ToolName: "get_weather", Input: map[string]any{"city": "SF"}},
			}},
			{Role: llm.RoleTool, Content: []llm.ContentPart{
				{Kind: llm.ContentToolResult, ToolCallID: "call_1", ToolName: "get_weather", Output: "sunny"},
			}},
		},
		Tools:      []llm.ToolDefinition{{Name: "get_weather", Description: "weather", InputSchema: map[string]any{"type": "object"}}},
		ToolChoice: llm.ToolChoiceAuto,
	}
	_ = collect(t, c, req)

	if captured.Model != "gpt-4o" || !captured.Stream {
		t.Fatalf("model/stream wrong: %+v", captured)
	}
	// system, user, assistant(+tool_calls), tool
	if len(captured.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(captured.Messages), captured.Messages)
	}
	if captured.Messages[0].Role != "system" || captured.Messages[0].Content != "be terse" {
		t.Fatalf("system message wrong: %+v", captured.Messages[0])
	}
	if captured.Messages[1].Role != "user" || captured.Messages[1].Content != "weather?" {
		t.Fatalf("user message wrong: %+v", captured.Messages[1])
	}
	asst := captured.Messages[2]
	if asst.Role != "assistant" || asst.Content != "checking" || len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant message wrong: %+v", asst)
	}
	if asst.ToolCalls[0].ID != "call_1" || asst.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("tool_call wrong: %+v", asst.ToolCalls[0])
	}
	if got := asst.ToolCalls[0].Function.Arguments; got != `{"city":"SF"}` {
		t.Fatalf("tool_call args = %q", got)
	}
	tool := captured.Messages[3]
	if tool.Role != "tool" || tool.ToolCallID != "call_1" || tool.Content != "sunny" {
		t.Fatalf("tool message wrong: %+v", tool)
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Function.Name != "get_weather" || captured.ToolChoice != "auto" {
		t.Fatalf("tools/tool_choice wrong: %+v choice=%q", captured.Tools, captured.ToolChoice)
	}
}
