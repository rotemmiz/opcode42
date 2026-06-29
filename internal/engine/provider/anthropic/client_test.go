package anthropic

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

func sseServer(t *testing.T, body string, captured *messagesRequest) *httptest.Server {
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

const textSSE = `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":12,"cache_read_input_tokens":3}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}

event: message_stop
data: {"type":"message_stop"}

`

func TestStream_TextWithThinkingAndUsage(t *testing.T) {
	srv := sseServer(t, textSSE, nil)
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "claude-sonnet-4-6", HTTPClient: srv.Client()})

	events := collect(t, c, &llm.Request{})
	want := []llm.EventType{
		llm.EventStepStart, llm.EventTextStart, llm.EventTextDelta, llm.EventTextDelta,
		llm.EventTextEnd, llm.EventStepFinish, llm.EventFinish,
	}
	if got := types(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	sf := events[5]
	if sf.Reason != "stop" || sf.Usage == nil {
		t.Fatalf("step-finish wrong: %+v", sf)
	}
	if sf.Usage.Input != 12 || sf.Usage.Output != 7 || sf.Usage.CacheRead != 3 {
		t.Fatalf("usage mapping wrong: %+v", *sf.Usage)
	}
}

const reasoningSSE = `data: {"type":"message_start","message":{"usage":{"input_tokens":5}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me think"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-abc"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"content_block_start","index":1,"content_block":{"type":"text"}}

data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}}

data: {"type":"content_block_stop","index":1}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}

data: {"type":"message_stop"}

`

func TestStream_ReasoningSignature(t *testing.T) {
	srv := sseServer(t, reasoningSSE, nil)
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "claude-opus-4-8", HTTPClient: srv.Client()})

	events := collect(t, c, &llm.Request{})
	var end llm.Event
	for _, e := range events {
		if e.Type == llm.EventReasoningEnd {
			end = e
		}
	}
	anth, _ := end.ProviderMetadata["anthropic"].(map[string]any)
	if anth == nil || anth["signature"] != "sig-abc" {
		t.Fatalf("reasoning signature not captured: %+v", end.ProviderMetadata)
	}
}

const toolSSE = `data: {"type":"message_start","message":{"usage":{"input_tokens":20}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"SF\"}"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}

data: {"type":"message_stop"}

`

func TestStream_ToolUse(t *testing.T) {
	srv := sseServer(t, toolSSE, nil)
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "claude-sonnet-4-6", HTTPClient: srv.Client()})

	events := collect(t, c, &llm.Request{})
	want := []llm.EventType{
		llm.EventStepStart, llm.EventToolInputStart, llm.EventToolInputDelta, llm.EventToolInputDelta,
		llm.EventToolInputEnd, llm.EventToolCall, llm.EventStepFinish, llm.EventFinish,
	}
	if got := types(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	call := events[5]
	if call.ID != "toolu_1" || call.Name != "get_weather" {
		t.Fatalf("tool-call ids wrong: %+v", call)
	}
	if city, _ := call.Input["city"].(string); city != "SF" {
		t.Fatalf("tool input wrong: %+v", call.Input)
	}
	if events[6].Reason != "tool-calls" {
		t.Fatalf("finish reason = %q, want tool-calls", events[6].Reason)
	}
}

const interleavedSSE = `data: {"type":"message_start","message":{"usage":{"input_tokens":8}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"content_block_start","index":1,"content_block":{"type":"text"}}

data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"let me check"}}

data: {"type":"content_block_stop","index":1}

data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_9","name":"ls"}}

data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{}"}}

data: {"type":"content_block_stop","index":2}

data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":6}}

data: {"type":"message_stop"}

`

func TestStream_InterleavedThinkingTextTool(t *testing.T) {
	srv := sseServer(t, interleavedSSE, nil)
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "claude-opus-4-8", HTTPClient: srv.Client()})

	want := []llm.EventType{
		llm.EventStepStart,
		llm.EventReasoningStart, llm.EventReasoningDelta, llm.EventReasoningEnd,
		llm.EventTextStart, llm.EventTextDelta, llm.EventTextEnd,
		llm.EventToolInputStart, llm.EventToolInputDelta, llm.EventToolInputEnd, llm.EventToolCall,
		llm.EventStepFinish, llm.EventFinish,
	}
	if got := types(collect(t, c, &llm.Request{})); !reflect.DeepEqual(got, want) {
		t.Fatalf("interleaved event types = %v, want %v", got, want)
	}
}

func TestStream_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"error","error":{"message":"bad"}}`)
	}))
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "claude", HTTPClient: srv.Client()})
	events := collect(t, c, &llm.Request{})
	if len(events) != 1 || events[0].Type != llm.EventProviderError || events[0].StatusCode != 400 {
		t.Fatalf("want provider-error 400, got %v", events)
	}
}

func TestBuildRequest_RendersBlocksAndMerges(t *testing.T) {
	var captured messagesRequest
	srv := sseServer(t, "data: {\"type\":\"message_stop\"}\n\n", &captured)
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL, Model: "claude-sonnet-4-6", APIKey: "sk", HTTPClient: srv.Client()})

	req := &llm.Request{
		SystemPrompts: []string{"be terse", "stay safe"},
		Messages: []llm.ModelMessage{
			{Role: llm.RoleUser, Content: []llm.ContentPart{{Kind: llm.ContentText, Text: "weather?"}}},
			{Role: llm.RoleAssistant, Content: []llm.ContentPart{
				{Kind: llm.ContentToolCall, ToolCallID: "toolu_1", ToolName: "get_weather", Input: map[string]any{"city": "SF"}},
			}},
			{Role: llm.RoleTool, Content: []llm.ContentPart{
				{Kind: llm.ContentToolResult, ToolCallID: "toolu_1", Output: "sunny"},
			}},
		},
		Tools:      []llm.ToolDefinition{{Name: "get_weather", InputSchema: map[string]any{"type": "object"}}},
		ToolChoice: llm.ToolChoiceAuto,
	}
	_ = collect(t, c, req)

	if captured.System != "be terse\n\nstay safe" || captured.MaxTokens != defaultMaxTokens || !captured.Stream {
		t.Fatalf("system/max_tokens/stream wrong: %+v", captured)
	}
	// user, assistant(tool_use), user(tool_result) — 3 messages, alternating.
	if len(captured.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d: %+v", len(captured.Messages), captured.Messages)
	}
	if captured.Messages[0].Role != "user" || captured.Messages[1].Role != "assistant" || captured.Messages[2].Role != "user" {
		t.Fatalf("roles wrong: %+v", captured.Messages)
	}
	if captured.Messages[1].Content[0].Type != "tool_use" || captured.Messages[1].Content[0].ID != "toolu_1" {
		t.Fatalf("tool_use block wrong: %+v", captured.Messages[1].Content)
	}
	tr := captured.Messages[2].Content[0]
	if tr.Type != "tool_result" || tr.ToolUseID != "toolu_1" || tr.Content != "sunny" {
		t.Fatalf("tool_result block wrong: %+v", tr)
	}
	if captured.ToolChoice["type"] != "auto" || len(captured.Tools) != 1 {
		t.Fatalf("tools/tool_choice wrong: %+v %+v", captured.ToolChoice, captured.Tools)
	}
}
