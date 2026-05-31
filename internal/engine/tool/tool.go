// Package tool implements Forge's built-in agent tools (read, write, edit, glob,
// grep, bash, patch, …) behind a small Tool interface. Each tool declares a JSON
// Schema for its input and runs against a working directory carried on Context.
//
// Tools are provider-neutral and storage-free: the processor's ToolExecutor
// (wired by the registry in M8) adapts a tool call to Tool.Run. Permission
// checks (M7) are layered in by the executor, not the tools themselves.
package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rotemmiz/forge/internal/engine/message"
)

// Context carries the ambient identity and working directory for a tool run.
type Context struct {
	SessionID string
	MessageID string
	CallID    string
	// Directory is the resolved working directory the tool operates within.
	Directory string
	// Questioner is the per-instance question manager the `question` tool uses
	// to ask the user and block for answers; nil when no client can answer.
	Questioner Asker
	// Subagent runs a nested agent task for the `task` tool; nil when subagent
	// spawning is unavailable (e.g. inside a subagent, to bound recursion).
	Subagent SubagentRunner
	// Skiller loads a named skill's instructions for the `skill` tool; nil when
	// no skill source is available.
	Skiller SkillSource
}

// Result is a tool's output. Output is the model-facing text; Title is a short
// human label; Metadata is structured detail surfaced to clients.
type Result struct {
	Title       string
	Output      string
	Metadata    map[string]any
	Attachments []message.FilePart
}

// Info describes a tool to the model and registry.
type Info struct {
	ID          string
	Description string
	Parameters  map[string]any // JSON Schema (draft-07 object)
}

// Tool is one built-in or dynamic tool.
type Tool interface {
	Info() Info
	Run(ctx context.Context, input map[string]any, tctx Context) (Result, error)
}

// decode re-decodes a generic input map into a typed parameter struct.
func decode(input map[string]any, dst any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("invalid tool input: %w", err)
	}
	return nil
}

// obj is a tiny helper for building JSON Schema objects.
func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
