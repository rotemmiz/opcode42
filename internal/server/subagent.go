package server

import (
	"context"
	"strings"

	"github.com/rotemmiz/forge/internal/engine"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/instance"
)

// subagentRunner implements tool.SubagentRunner for the `task` tool. It creates
// a child session linked to the parent, runs a nested agent loop under the
// requested agent (its permission ruleset + system prompt), and returns the
// subagent's final assistant text. The child engine is built WITHOUT a subagent
// runner, so a subagent cannot spawn further subagents (recursion is bounded).
type subagentRunner struct {
	opts      Options
	inst      *instance.Context
	directory string
	// provider/model are the parent's, inherited when the requested agent
	// declares no model of its own.
	provider string
	model    string
}

func (r subagentRunner) Run(ctx context.Context, req tool.TaskRequest) (string, error) {
	child, err := r.opts.Sessions.CreateChild(ctx, r.directory, req.ParentSessionID)
	if err != nil {
		return "", err
	}
	agent := resolveAgent(r.directory, req.Agent)

	provider, model := r.provider, r.model
	if agent.Model != nil && agent.Model.ProviderID != "" && agent.Model.ModelID != "" {
		provider, model = agent.Model.ProviderID, agent.Model.ModelID
	}

	// nil subagent on the child engine bounds recursion (no nested subagents);
	// nil titles skips title generation for child sessions (opencode returns
	// early for sessions with a parentID, prompt.ts:247).
	eng := buildEngine(r.opts, r.inst, r.directory, agentRulesets(agent), nil, agentMaxSteps(agent), nil)
	out, err := eng.Prompt(ctx, engine.PromptInput{
		SessionID: child.ID, Provider: provider, Model: model,
		Agent: agentNameOrDefault(agent.Name), System: agent.Prompt,
		Parts: []engine.PartInput{{Type: "text", Text: req.Prompt}},
	})
	if err != nil {
		return "", err
	}
	return wrapTaskResult(child.ID, lastText(out)), nil
}

// lastText returns the final text part of a completed assistant turn, matching
// opencode's `parts.findLast(type==="text")` (tool/task.ts:201). A turn can hold
// several text segments around tool calls; only the last is the answer.
func lastText(w message.WithParts) string {
	for i := len(w.Parts) - 1; i >= 0; i-- {
		if tp, ok := w.Parts[i].(*message.TextPart); ok {
			return tp.Text
		}
	}
	return ""
}

// wrapTaskResult formats the subagent's answer in opencode's task envelope
// (tool/task.ts:54-55).
func wrapTaskResult(sessionID, text string) string {
	return strings.Join([]string{
		`<task id="` + sessionID + `" state="completed">`,
		"<task_result>", text, "</task_result>", "</task>",
	}, "\n")
}

func agentNameOrDefault(name string) string {
	if name == "" {
		return "build"
	}
	return name
}
