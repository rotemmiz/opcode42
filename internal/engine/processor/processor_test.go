package processor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/llm"
	"github.com/rotemmiz/opcode42/internal/engine/message"
	"github.com/rotemmiz/opcode42/internal/id"
	"github.com/rotemmiz/opcode42/internal/storage"
)

func decodeJSON(raw json.RawMessage, dst any) error { return json.Unmarshal(raw, dst) }

func feed(events ...llm.Event) <-chan llm.Event {
	ch := make(chan llm.Event, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

type fakeExecutor struct {
	out string
	err error
}

func (f fakeExecutor) Execute(_ context.Context, _ ToolCall) (ToolResult, error) {
	if f.err != nil {
		return ToolResult{}, f.err
	}
	return ToolResult{Output: f.out, Title: "Tool"}, nil
}

type recordingAsker struct{ calls int }

func (a *recordingAsker) AskPermission(context.Context, string, string, []string, map[string]any) error {
	a.calls++
	return nil
}

// harness builds a processor over an in-memory store + bus, with a persisted
// assistant message, and captures SSE events published during the run.
type harness struct {
	store     *message.Store
	bus       *bus.Bus
	sessionID string
	assistant *message.AssistantMessage
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const sessionID = "ses_proc"
	if _, err := db.Exec(`INSERT INTO project (id, worktree, time_created, time_updated) VALUES ('p','/tmp',0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated)
		VALUES (?, 'p','s','/tmp','1',0,0)`, sessionID); err != nil {
		t.Fatal(err)
	}
	store := message.NewStore(db)
	b := bus.NewInstanceBus(sessionID, nil)
	assistant := &message.AssistantMessage{ID: id.Ascending(id.Message), SessionID: sessionID, Role: message.RoleAssistant,
		ProviderID: "openai", ModelID: "gpt-4o", Agent: "build", Path: message.Path{CWD: "/tmp", Root: "/tmp"}}
	assistant.Time.Created = time.Now().UnixMilli()
	if err := store.PutMessage(context.Background(), message.Info{Assistant: assistant}); err != nil {
		t.Fatal(err)
	}
	return &harness{store: store, bus: b, sessionID: sessionID, assistant: assistant}
}

func (h *harness) proc(exec ToolExecutor, asker PermissionAsker) *Processor {
	return New(Config{Store: h.store, Bus: h.bus, Catalog: catalog.Fixture(),
		Executor: exec, Asker: asker, SessionID: h.sessionID}, h.assistant)
}

// drainBus collects all currently-buffered SSE events on the subscription.
func drainBus(sub <-chan bus.Event) []bus.Event {
	var out []bus.Event
	for {
		select {
		case e := <-sub:
			out = append(out, e)
		default:
			return out
		}
	}
}

func countType(events []bus.Event, typ string) int {
	n := 0
	for _, e := range events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func TestProcessor_TextOnly(t *testing.T) {
	h := newHarness(t)
	sub, _ := h.bus.Subscribe()
	p := h.proc(nil, nil)

	outcome := p.Run(context.Background(), feed(
		llm.Event{Type: llm.EventStepStart},
		llm.Event{Type: llm.EventTextStart, ID: "t1"},
		llm.Event{Type: llm.EventTextDelta, ID: "t1", Text: "Hello"},
		llm.Event{Type: llm.EventTextDelta, ID: "t1", Text: " world"},
		llm.Event{Type: llm.EventTextEnd, ID: "t1"},
		llm.Event{Type: llm.EventStepFinish, Reason: "stop", Usage: &llm.TokenUsage{Input: 10, Output: 5}},
		llm.Event{Type: llm.EventFinish},
	))
	if outcome != OutcomeContinue {
		t.Fatalf("outcome = %v, want Continue", outcome)
	}

	parts, _ := h.store.Parts(context.Background(), h.assistant.ID)
	var text *message.TextPart
	var sawStepStart, sawStepFinish bool
	for _, pt := range parts {
		switch v := pt.(type) {
		case *message.TextPart:
			text = v
		case *message.StepStartPart:
			sawStepStart = true
		case *message.StepFinishPart:
			sawStepFinish = true
			if v.Reason != "stop" || v.Tokens.Input != 10 {
				t.Fatalf("step-finish part wrong: %+v", v)
			}
		}
	}
	if !sawStepStart || !sawStepFinish || text == nil || text.Text != "Hello world" {
		t.Fatalf("parts wrong: stepStart=%v stepFinish=%v text=%+v", sawStepStart, sawStepFinish, text)
	}
	if h.assistant.Finish != "stop" || h.assistant.Tokens.Input != 10 || h.assistant.Time.Completed == nil {
		t.Fatalf("assistant not finalized: %+v", h.assistant)
	}

	evs := drainBus(sub)
	if countType(evs, "message.part.delta") != 2 {
		t.Fatalf("want 2 part.delta events, got %d", countType(evs, "message.part.delta"))
	}
	if countType(evs, "message.part.updated") == 0 || countType(evs, "message.updated") == 0 {
		t.Fatalf("missing part.updated / message.updated events: %+v", evs)
	}
}

func TestProcessor_ToolCallExecuted(t *testing.T) {
	h := newHarness(t)
	p := h.proc(fakeExecutor{out: "sunny"}, nil)

	outcome := p.Run(context.Background(), feed(
		llm.Event{Type: llm.EventStepStart},
		llm.Event{Type: llm.EventToolInputStart, ID: "call_1", Name: "get_weather"},
		llm.Event{Type: llm.EventToolInputEnd, ID: "call_1"},
		llm.Event{Type: llm.EventToolCall, ID: "call_1", Name: "get_weather", Input: map[string]any{"city": "SF"}},
		llm.Event{Type: llm.EventStepFinish, Reason: "tool-calls", Usage: &llm.TokenUsage{Input: 20, Output: 8}},
	))
	if outcome != OutcomeContinue {
		t.Fatalf("outcome = %v, want Continue", outcome)
	}
	parts, _ := h.store.Parts(context.Background(), h.assistant.ID)
	var tool *message.ToolPart
	for _, pt := range parts {
		if tp, ok := pt.(*message.ToolPart); ok {
			tool = tp
		}
	}
	if tool == nil || tool.Status() != message.ToolCompleted {
		t.Fatalf("tool not completed: %+v", tool)
	}
	var st message.ToolStateCompleted
	_ = decodeJSON(tool.State, &st)
	if st.Output != "sunny" || st.Input["city"] != "SF" {
		t.Fatalf("tool result wrong: %+v", st)
	}
}

func TestProcessor_ToolExecutorError(t *testing.T) {
	h := newHarness(t)
	p := h.proc(fakeExecutor{err: errors.New("boom")}, nil)
	p.Run(context.Background(), feed(
		llm.Event{Type: llm.EventToolCall, ID: "call_1", Name: "bash", Input: map[string]any{"cmd": "x"}},
		llm.Event{Type: llm.EventStepFinish, Reason: "tool-calls"},
	))
	parts, _ := h.store.Parts(context.Background(), h.assistant.ID)
	for _, pt := range parts {
		if tp, ok := pt.(*message.ToolPart); ok {
			if tp.Status() != message.ToolError {
				t.Fatalf("tool should be error, got %s", tp.Status())
			}
		}
	}
}

func TestProcessor_ProviderError(t *testing.T) {
	h := newHarness(t)
	p := h.proc(nil, nil)
	outcome := p.Run(context.Background(), feed(
		llm.Event{Type: llm.EventProviderError, StatusCode: 429, Message: "rate limited"},
	))
	if outcome != OutcomeStop {
		t.Fatalf("outcome = %v, want Stop", outcome)
	}
	if h.assistant.Error == nil || h.assistant.Error.Name != "APIError" {
		t.Fatalf("assistant error not set: %+v", h.assistant.Error)
	}
}

func TestProcessor_AbortMarksInterrupted(t *testing.T) {
	h := newHarness(t)
	p := h.proc(nil, nil) // no executor: the running tool stays unresolved
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := p.Run(ctx, feed(
		llm.Event{Type: llm.EventToolCall, ID: "call_1", Name: "bash", Input: map[string]any{"cmd": "x"}},
	))
	if outcome != OutcomeStop {
		t.Fatalf("outcome = %v, want Stop", outcome)
	}
	if h.assistant.Error == nil || h.assistant.Error.Name != "MessageAbortedError" {
		t.Fatalf("want aborted error, got %+v", h.assistant.Error)
	}
	parts, _ := h.store.Parts(context.Background(), h.assistant.ID)
	for _, pt := range parts {
		if tp, ok := pt.(*message.ToolPart); ok {
			var st message.ToolStateError
			_ = decodeJSON(tp.State, &st)
			if tp.Status() != message.ToolError || st.Metadata["interrupted"] != true {
				t.Fatalf("tool not interrupted: %+v", tp)
			}
		}
	}
}

func TestProcessor_DoomLoopAsks(t *testing.T) {
	h := newHarness(t)
	asker := &recordingAsker{}
	p := h.proc(fakeExecutor{out: "x"}, asker)
	same := map[string]any{"cmd": "ls"}
	p.Run(context.Background(), feed(
		llm.Event{Type: llm.EventToolCall, ID: "c1", Name: "bash", Input: same},
		llm.Event{Type: llm.EventToolCall, ID: "c2", Name: "bash", Input: same},
		llm.Event{Type: llm.EventToolCall, ID: "c3", Name: "bash", Input: same},
		llm.Event{Type: llm.EventStepFinish, Reason: "tool-calls"},
	))
	if asker.calls == 0 {
		t.Fatalf("doom-loop ask was not triggered")
	}
}

func TestProcessor_OverflowFlagsCompaction(t *testing.T) {
	h := newHarness(t)
	p := h.proc(nil, nil)
	// gpt-4o context is 128000; usable*0.9 = 92160. Input above that overflows.
	outcome := p.Run(context.Background(), feed(
		llm.Event{Type: llm.EventStepFinish, Reason: "stop", Usage: &llm.TokenUsage{Input: 120000, Output: 10}},
	))
	if outcome != OutcomeCompact {
		t.Fatalf("outcome = %v, want Compact", outcome)
	}
}
