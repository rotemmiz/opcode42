package registry

import (
	"context"
	"fmt"

	"github.com/rotemmiz/opcode42/internal/engine/permission"
	"github.com/rotemmiz/opcode42/internal/engine/processor"
	"github.com/rotemmiz/opcode42/internal/engine/tool"
)

// Asker is the permission gate the executor consults before running a tool
// (satisfied by *permission.Manager).
type Asker interface {
	Ask(ctx context.Context, in permission.AskInput) error
}

// PluginHooks is the executor's view of the flag-gated plugin host (plan 05).
// The concrete *pluginbridge.Bridge satisfies it; a nil PluginHooks (the
// default, plugin-host off) makes every call site a no-op. Trigger mutates out
// in place per opencode's hook contract; on any failure out is left untouched.
// Kept as an interface so the registry package need not import the sidecar —
// the wiring seam stays additive.
type PluginHooks interface {
	Trigger(ctx context.Context, name string, input any, out any)
}

// Plan-05 hook names routed from the executor. They mirror the keys in
// opencode's Hooks interface (opencode/packages/plugin/src/index.ts:265-282)
// and the pluginbridge constants. tool.execute.before/after fire around the
// tool run exactly as opencode (session/tools.ts:87-107): before mutates the
// args, after mutates the result's title/output/metadata.
const (
	hookToolExecuteBefore = "tool.execute.before"
	hookToolExecuteAfter  = "tool.execute.after"
)

// toolBeforeOutput is the mutable output of the tool.execute.before hook
// (plugin/src/index.ts:267-270): a plugin may rewrite the tool's args.
type toolBeforeOutput struct {
	Args map[string]any `json:"args"`
}

// toolAfterOutput is the mutable output of the tool.execute.after hook
// (plugin/src/index.ts:276-282): a plugin may rewrite the result title/output/
// metadata before it is recorded.
type toolAfterOutput struct {
	Title    string         `json:"title"`
	Output   string         `json:"output"`
	Metadata map[string]any `json:"metadata"`
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
	// Plugins routes the tool.execute.before/after hooks through the flag-gated
	// plugin host (plan 05). nil ⇒ no plugin host (the default): both call sites
	// are no-ops and the args/result pass through unmodified.
	Plugins PluginHooks
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
				// tool.execute.before fires before the permission gate, exactly as
				// opencode (session/tools.ts:131-135): the plugin may rewrite args.
				args := e.triggerToolBefore(ctx, call)
				if e.Asker != nil {
					if err := e.Asker.Ask(ctx, permission.AskInput{
						SessionID: call.SessionID, Permission: call.Name, Patterns: []string{"*"},
						Always: []string{"*"}, Rulesets: e.Rulesets, Tool: call.Name,
						Metadata: map[string]any{"tool": call.Name, "sessionID": call.SessionID},
					}); err != nil {
						return processor.ToolResult{}, err
					}
				}
				out, _, err := e.MCP.CallTool(ctx, call.Name, args)
				if err != nil {
					return processor.ToolResult{}, err
				}
				res := processor.ToolResult{Output: out, Title: call.Name}
				return e.triggerToolAfter(ctx, call, args, res), nil
			}
		}
		return processor.ToolResult{}, fmt.Errorf("unknown tool: %s", call.Name)
	}
	// tool.execute.before fires before the permission gate, exactly as opencode
	// (session/tools.ts:87-91): the plugin may rewrite args before they are run.
	args := e.triggerToolBefore(ctx, call)
	if e.Asker != nil {
		key, pattern := permKeyPattern(call.Name, args)
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
	res, err := t.Run(ctx, args, tool.Context{
		SessionID: call.SessionID, MessageID: call.MessageID, CallID: call.CallID,
		Directory: e.Directory, Questioner: e.Questioner, Subagent: e.Subagent, Skiller: e.Skiller,
		LSP: e.LSP,
	})
	if err != nil {
		return processor.ToolResult{}, err
	}
	out := processor.ToolResult{Output: res.Output, Title: res.Title, Metadata: res.Metadata, Attachments: res.Attachments}
	return e.triggerToolAfter(ctx, call, args, out), nil
}

// triggerToolBefore fires the tool.execute.before hook and returns the args the
// tool should run with (possibly rewritten by a plugin). With no plugin host it
// returns call.Input unchanged. The hook input is read-only {tool,sessionID,
// callID}; the output {args} is mutated in place (plugin/src/index.ts:267-270,
// session/tools.ts:87-91).
func (e *Executor) triggerToolBefore(ctx context.Context, call processor.ToolCall) map[string]any {
	if e.Plugins == nil {
		return call.Input
	}
	out := toolBeforeOutput{Args: call.Input}
	e.Plugins.Trigger(ctx, hookToolExecuteBefore,
		map[string]any{"tool": call.Name, "sessionID": call.SessionID, "callID": call.CallID}, &out)
	if out.Args == nil {
		return call.Input
	}
	return out.Args
}

// triggerToolAfter fires the tool.execute.after hook over a completed result and
// returns the (possibly rewritten) result. With no plugin host it returns res
// unchanged. The hook input is read-only {tool,sessionID,callID,args}; the
// output {title,output,metadata} is mutated in place (plugin/src/index.ts:
// 276-282, session/tools.ts:103-107).
func (e *Executor) triggerToolAfter(ctx context.Context, call processor.ToolCall, args map[string]any, res processor.ToolResult) processor.ToolResult {
	if e.Plugins == nil {
		return res
	}
	out := toolAfterOutput{Title: res.Title, Output: res.Output, Metadata: res.Metadata}
	e.Plugins.Trigger(ctx, hookToolExecuteAfter,
		map[string]any{"tool": call.Name, "sessionID": call.SessionID, "callID": call.CallID, "args": args}, &out)
	res.Title = out.Title
	res.Output = out.Output
	res.Metadata = out.Metadata
	return res
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
