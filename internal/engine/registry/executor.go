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
}

var _ processor.ToolExecutor = (*Executor)(nil)

// Execute runs the named tool after a permission check.
func (e *Executor) Execute(ctx context.Context, call processor.ToolCall) (processor.ToolResult, error) {
	t, ok := e.Registry.Get(call.Name)
	if !ok {
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
