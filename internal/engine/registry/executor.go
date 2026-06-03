package registry

import (
	"context"
	"fmt"

	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/processor"
	"github.com/rotemmiz/forge/internal/engine/tool"
)

// Asker is the permission gate the executor consults before running a tool
// (satisfied by *permission.Manager).
type Asker interface {
	Ask(ctx context.Context, in permission.AskInput) error
}

// Executor adapts the registry's tools to a processor.ToolExecutor: it resolves
// the tool, runs its permission check, then executes it in the working
// directory. A denied permission surfaces as a tool error to the model.
type Executor struct {
	Registry  *Registry
	Asker     Asker
	SessionID string
	Directory string
	// Rulesets are the merged agent/config permission rules consulted on ask.
	Rulesets []permission.Ruleset
	// Questioner is the per-instance question manager passed to the `question`
	// tool via tool.Context (nil when no client can answer).
	Questioner tool.Asker
	// Subagent runs nested agent tasks for the `task` tool (nil to disable).
	Subagent tool.SubagentRunner
	// Skiller loads named skills for the `skill` tool (nil to disable).
	Skiller tool.SkillSource
	// LSP is the per-instance LSP service the `lsp` tool queries (nil to disable).
	// Satisfied by *lsp.Service.
	LSP tool.LSPService
	// MCP dispatches MCP tool calls when a name isn't a built-in tool (nil ⇒ no
	// MCP). Satisfied by *mcp.Manager.
	MCP MCPCaller
	// StructuredTool, when set, is the synthetic StructuredOutput tool's name. A
	// call to it is captured (not dispatched to the registry or MCP) and answered
	// with a canned success; the processor records the input as the run's
	// structured output (prompt.ts:1747-1773).
	StructuredTool string
}

// MCPCaller dispatches a flattened MCP tool name; found is false when no MCP
// tool matches the name. HasTool reports whether a name resolves to a connected
// MCP tool, so the executor can run the permission gate (and report "unknown
// tool" for a genuine miss) before dispatching.
type MCPCaller interface {
	HasTool(ctx context.Context, name string) bool
	CallTool(ctx context.Context, name string, args map[string]any) (output string, found bool, err error)
}

var _ processor.ToolExecutor = (*Executor)(nil)

// Execute runs the named tool after a permission check.
func (e *Executor) Execute(ctx context.Context, call processor.ToolCall) (processor.ToolResult, error) {
	// The synthetic StructuredOutput tool has no registry/MCP backing: the AI SDK
	// validates its input against the schema before dispatch, so a call reaching
	// here is the model's final structured answer (prompt.ts:1755-1773).
	if e.StructuredTool != "" && call.Name == e.StructuredTool {
		return processor.ToolResult{
			Output:   "Structured output captured successfully.",
			Title:    "Structured Output",
			Metadata: map[string]any{"valid": true},
		}, nil
	}
	t, ok := e.Registry.Get(call.Name)
	if !ok {
		// Not a built-in: try the instance's MCP tools. opencode gates MCP tool
		// execution through the same permission ask path as built-ins, keyed on
		// the flattened tool name with patterns/always "*" (session/tools.ts:135).
		// The gate runs only once the name resolves to a real MCP tool, so a
		// genuinely-unknown name still falls through to "unknown tool".
		if e.MCP != nil {
			if e.MCP.HasTool(ctx, call.Name) {
				if e.Asker != nil {
					if err := e.Asker.Ask(ctx, permission.AskInput{
						SessionID: call.SessionID, Permission: call.Name, Patterns: []string{"*"},
						Always: []string{"*"}, Rulesets: e.Rulesets, Tool: call.Name,
						Metadata: map[string]any{"tool": call.Name, "sessionID": call.SessionID},
					}); err != nil {
						return processor.ToolResult{}, err
					}
				}
				out, _, err := e.MCP.CallTool(ctx, call.Name, call.Input)
				if err != nil {
					return processor.ToolResult{}, err
				}
				return processor.ToolResult{Output: out, Title: call.Name}, nil
			}
		}
		return processor.ToolResult{}, fmt.Errorf("unknown tool: %s", call.Name)
	}
	if e.Asker != nil {
		key, pattern := permKeyPattern(call.Name, call.Input)
		if key != "" {
			err := e.Asker.Ask(ctx, permission.AskInput{
				SessionID: call.SessionID, Permission: key, Patterns: []string{pattern},
				Always: []string{pattern}, Rulesets: e.Rulesets, Tool: call.Name,
				Metadata: map[string]any{"tool": call.Name, "sessionID": call.SessionID},
			})
			if err != nil {
				return processor.ToolResult{}, err
			}
		}
	}
	res, err := t.Run(ctx, call.Input, tool.Context{
		SessionID: call.SessionID, MessageID: call.MessageID, CallID: call.CallID,
		Directory: e.Directory, Questioner: e.Questioner, Subagent: e.Subagent, Skiller: e.Skiller,
		LSP: e.LSP,
	})
	if err != nil {
		return processor.ToolResult{}, err
	}
	return processor.ToolResult{Output: res.Output, Title: res.Title, Metadata: res.Metadata, Attachments: res.Attachments}, nil
}

// permKeyPattern maps a tool call to its permission key and the pattern to
// evaluate (the relevant input field). An empty key means the tool is not
// permission-gated.
func permKeyPattern(name string, input map[string]any) (string, string) {
	switch name {
	case "bash":
		return permission.KeyBash, str(input, "command")
	case "read":
		return permission.KeyRead, str(input, "filePath")
	case "edit", "write", "patch":
		return permission.KeyEdit, str(input, "filePath")
	case "glob":
		return permission.KeyGlob, str(input, "pattern")
	case "grep":
		return permission.KeyGrep, str(input, "pattern")
	case "lsp":
		// opencode gates the lsp tool with patterns/always "*" (tool/lsp.ts:56-61),
		// regardless of operation — every lsp call asks the "lsp" permission.
		return permission.KeyLSP, "*"
	case "webfetch":
		return permission.KeyWebFetch, str(input, "url")
	case "websearch":
		return permission.KeyWebSearch, str(input, "query")
	case "task":
		return permission.KeyTask, str(input, "agent")
	case "skill":
		return permission.KeySkill, str(input, "name")
	case "question":
		return permission.KeyQuestion, ""
	case "todowrite":
		return permission.KeyTodoWrite, ""
	default:
		return "", "" // dynamic tools: ungated in M8 (MCP wiring is plan 03)
	}
}

func str(input map[string]any, key string) string {
	if v, ok := input[key].(string); ok {
		return v
	}
	return ""
}
