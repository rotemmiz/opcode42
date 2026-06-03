package processor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
)

// ensureTool returns the ToolPart for callID, creating it in the pending state
// on first sight (processor.ts:ensureToolCall).
func (p *Processor) ensureTool(ctx context.Context, callID, name string) *message.ToolPart {
	p.mu.Lock()
	if part, ok := p.tools[callID]; ok {
		if name != "" && part.Tool == "" {
			part.Tool = name
		}
		p.mu.Unlock()
		return part
	}
	part := &message.ToolPart{PartBase: p.partBase(), Type: "tool", CallID: callID, Tool: name,
		State: mustState(message.ToolStatePending{Status: message.ToolPending, Input: map[string]any{}})}
	p.tools[callID] = part
	p.toolOrder = append(p.toolOrder, callID)
	p.mu.Unlock()
	p.updatePart(ctx, part)
	return part
}

// onToolCall transitions a tool part pending->running with parsed input, records
// it for execution, and runs the doom-loop guard (processor.ts:377-449).
func (p *Processor) onToolCall(ctx context.Context, ev llm.Event) {
	part := p.ensureTool(ctx, ev.ID, ev.Name)
	input := ev.Input
	if input == nil {
		input = map[string]any{}
	}

	p.mu.Lock()
	part.Tool = ev.Name
	running := message.ToolStateRunning{Status: message.ToolRunning, Input: input}
	running.Time.Start = time.Now().UnixMilli()
	part.State = mustState(running)
	if ev.ProviderMetadata != nil {
		part.Metadata = ev.ProviderMetadata
	}
	p.pending[ev.ID] = ToolCall{CallID: ev.ID, Name: ev.Name, Input: input,
		SessionID: p.cfg.SessionID, MessageID: p.assistant.ID}
	doom := p.isDoomLoop(ev.Name, input)
	p.mu.Unlock()

	p.updatePart(ctx, part)

	if doom && p.cfg.Asker != nil {
		if err := p.cfg.Asker.AskPermission(ctx, p.cfg.SessionID, "doom_loop", []string{ev.Name},
			map[string]any{"tool": ev.Name, "input": input}); err != nil {
			p.failCall(ctx, ev.ID, err.Error())
		}
	}
}

// isDoomLoop reports whether the last doomLoopThreshold parts are all the same
// tool with identical input (caller holds mu).
func (p *Processor) isDoomLoop(name string, input map[string]any) bool {
	if len(p.toolOrder) < doomLoopThreshold {
		return false
	}
	want, _ := json.Marshal(input)
	recent := p.toolOrder[len(p.toolOrder)-doomLoopThreshold:]
	for _, callID := range recent {
		part := p.tools[callID]
		if part == nil || part.Tool != name || part.Status() == message.ToolPending {
			return false
		}
		var got []byte
		switch part.Status() {
		case message.ToolRunning:
			var st message.ToolStateRunning
			_ = json.Unmarshal(part.State, &st)
			got, _ = json.Marshal(st.Input)
		case message.ToolCompleted:
			var st message.ToolStateCompleted
			_ = json.Unmarshal(part.State, &st)
			got, _ = json.Marshal(st.Input)
		default:
			return false
		}
		if string(got) != string(want) {
			return false
		}
	}
	return true
}

// onToolResult completes a call from a provider-emitted result.
func (p *Processor) onToolResult(ctx context.Context, ev llm.Event) {
	p.mu.Lock()
	delete(p.pending, ev.ID)
	p.mu.Unlock()
	p.completeCall(ctx, ev.ID, ToolResult{Output: ev.Output})
}

// onToolError fails a call from a provider-emitted error.
func (p *Processor) onToolError(ctx context.Context, ev llm.Event) {
	p.mu.Lock()
	delete(p.pending, ev.ID)
	p.mu.Unlock()
	p.failCall(ctx, ev.ID, ev.Message)
}

// executePending runs any tool calls the provider requested but did not resolve,
// concurrently, via the injected executor. With no executor, calls are left
// running and cleanup marks them interrupted.
func (p *Processor) executePending(ctx context.Context) {
	if p.cfg.Executor == nil {
		return
	}
	p.mu.Lock()
	calls := make([]ToolCall, 0, len(p.pending))
	for _, c := range p.pending {
		calls = append(calls, c)
	}
	p.pending = map[string]ToolCall{}
	p.mu.Unlock()

	for _, call := range calls {
		p.wg.Add(1)
		go func(call ToolCall) {
			defer p.wg.Done()
			res, err := p.cfg.Executor.Execute(ctx, call)
			if err != nil {
				if ctx.Err() != nil {
					// Cancelled mid-execution: record the interrupted shape so
					// replay shows "[Tool execution was interrupted]".
					p.interruptCall(ctx, call.CallID)
					return
				}
				p.failCall(ctx, call.CallID, err.Error())
				return
			}
			p.completeCall(ctx, call.CallID, res)
		}(call)
	}
}

func (p *Processor) completeCall(ctx context.Context, callID string, res ToolResult) {
	p.mu.Lock()
	part, ok := p.tools[callID]
	if !ok {
		p.mu.Unlock()
		return
	}
	input := inputOf(part)
	start := startOf(part)
	st := message.ToolStateCompleted{Status: message.ToolCompleted, Input: input,
		Output: res.Output, Title: res.Title, Metadata: orEmptyMap(res.Metadata), Attachments: res.Attachments}
	st.Time.Start, st.Time.End = start, time.Now().UnixMilli()
	part.State = mustState(st)
	// A successful StructuredOutput call carries the run's final structured
	// answer: capture its input and end the loop (prompt.ts:1458-1462).
	if p.cfg.StructuredTool != "" && part.Tool == p.cfg.StructuredTool {
		p.structured = input
		p.hasStructured = true
		p.shouldBreak = true
	}
	p.mu.Unlock()
	p.updatePart(ctx, part)
}

func (p *Processor) failCall(ctx context.Context, callID, errMsg string) {
	p.mu.Lock()
	part, ok := p.tools[callID]
	if !ok {
		p.mu.Unlock()
		return
	}
	start := startOf(part)
	st := message.ToolStateError{Status: message.ToolError, Input: inputOf(part), Error: errMsg}
	st.Time.Start, st.Time.End = start, time.Now().UnixMilli()
	part.State = mustState(st)
	p.mu.Unlock()
	p.updatePart(ctx, part)
}

func mustState(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("processor: marshal tool state: " + err.Error())
	}
	return b
}

func inputOf(part *message.ToolPart) map[string]any {
	switch part.Status() {
	case message.ToolRunning:
		var st message.ToolStateRunning
		_ = json.Unmarshal(part.State, &st)
		return st.Input
	case message.ToolPending:
		var st message.ToolStatePending
		_ = json.Unmarshal(part.State, &st)
		return st.Input
	default:
		return map[string]any{}
	}
}

func startOf(part *message.ToolPart) int64 {
	var st message.ToolStateRunning
	if err := json.Unmarshal(part.State, &st); err == nil && st.Time.Start != 0 {
		return st.Time.Start
	}
	return time.Now().UnixMilli()
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
