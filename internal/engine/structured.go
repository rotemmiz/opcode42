package engine

import (
	"context"
	"time"

	"github.com/rotemmiz/opcode42/internal/engine/llm"
	"github.com/rotemmiz/opcode42/internal/engine/message"
)

// wantsStructuredOutput reports whether the user turn requested a json_schema
// response format (prompt.ts:1403).
func wantsStructuredOutput(f *message.OutputFormat) bool {
	return f != nil && f.Type == "json_schema"
}

// structuredOutputTool builds the synthetic StructuredOutput tool advertised to
// the model for a json_schema format. The format's schema is the tool's input
// schema, with any top-level $schema key dropped (prompt.ts:1747-1773).
func structuredOutputTool(f *message.OutputFormat) llm.ToolDefinition {
	schema := map[string]any{}
	for k, v := range f.Schema {
		if k == "$schema" {
			continue
		}
		schema[k] = v
	}
	return llm.ToolDefinition{
		Name:        structuredOutputToolName,
		Description: structuredOutputDescription,
		InputSchema: schema,
	}
}

// finishStructured records the captured structured output on the assistant
// message and finalizes it as a normal stop (prompt.ts:1458-1462).
func (e *Engine) finishStructured(ctx context.Context, a *message.AssistantMessage, out any) {
	a.Structured = out
	if a.Finish == "" || a.Finish == "tool-calls" {
		a.Finish = "stop"
	}
	completed := time.Now().UnixMilli()
	a.Time.Completed = &completed
	_ = e.cfg.Store.PutMessage(ctx, message.Info{Assistant: a})
	e.emitAssistant(a)
}

// failStructured marks the assistant with a StructuredOutputError when a finished
// turn never produced structured output (prompt.ts:1466-1473). The error name
// matches opencode's StructuredOutputError schema (openapi.json).
func (e *Engine) failStructured(ctx context.Context, a *message.AssistantMessage) {
	a.Error = &message.Error{Name: "StructuredOutputError", Data: map[string]any{
		"message": "Model did not produce structured output",
		"retries": 0,
	}}
	if a.Time.Completed == nil {
		completed := time.Now().UnixMilli()
		a.Time.Completed = &completed
	}
	_ = e.cfg.Store.PutMessage(ctx, message.Info{Assistant: a})
	e.emitAssistant(a)
}
