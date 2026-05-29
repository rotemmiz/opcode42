package tool

import (
	"context"
	"fmt"
)

// These tools depend on collaborators delivered by later milestones/plans
// (question manager, web search provider, skill loader, the agent loop for
// subagents). Each takes its dependency as an interface so it is unit-testable
// now; an unset dependency yields a clear "not available" error rather than a
// panic.

// Asker asks the user a question and blocks for the answer (question.Manager).
type Asker interface {
	Ask(ctx context.Context, sessionID, text string, options []string) (string, error)
}

// Question asks the user a question and returns their answer.
type Question struct{ Asker Asker }

// Info describes the question tool.
func (Question) Info() Info {
	return Info{
		ID:          "question",
		Description: "Ask the user a question and wait for their answer.",
		Parameters: obj(map[string]any{
			"text":    strProp("The question to ask."),
			"options": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional choices."},
		}, "text"),
	}
}

type questionParams struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

// Run asks via the injected Asker.
func (q Question) Run(ctx context.Context, input map[string]any, tctx Context) (Result, error) {
	if q.Asker == nil {
		return Result{}, fmt.Errorf("question: not available")
	}
	var p questionParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	answer, err := q.Asker.Ask(ctx, tctx.SessionID, p.Text, p.Options)
	if err != nil {
		return Result{}, err
	}
	return Result{Title: p.Text, Output: answer, Metadata: map[string]any{"answer": answer}}, nil
}

// Searcher performs a web search (Exa/Parallel/etc., wired in plan 03/flags).
type Searcher interface {
	Search(ctx context.Context, query string) (string, error)
}

// WebSearch searches the web via an injected Searcher.
type WebSearch struct{ Searcher Searcher }

// Info describes the websearch tool.
func (WebSearch) Info() Info {
	return Info{
		ID:          "websearch",
		Description: "Search the web and return result snippets.",
		Parameters:  obj(map[string]any{"query": strProp("The search query.")}, "query"),
	}
}

// Run searches via the injected Searcher.
func (w WebSearch) Run(ctx context.Context, input map[string]any, _ Context) (Result, error) {
	if w.Searcher == nil {
		return Result{}, fmt.Errorf("websearch: not configured")
	}
	query, _ := input["query"].(string)
	if query == "" {
		return Result{}, fmt.Errorf("websearch: query is required")
	}
	out, err := w.Searcher.Search(ctx, query)
	if err != nil {
		return Result{}, err
	}
	return Result{Title: query, Output: out}, nil
}

// SkillSource resolves a named skill's instructions (plan 04 loaders).
type SkillSource interface {
	Load(name string) (string, error)
}

// Skill loads a named skill's instructions into the conversation.
type Skill struct{ Source SkillSource }

// Info describes the skill tool.
func (Skill) Info() Info {
	return Info{
		ID:          "skill",
		Description: "Load a named skill's instructions.",
		Parameters:  obj(map[string]any{"name": strProp("The skill name.")}, "name"),
	}
}

// Run loads the skill via the injected source.
func (s Skill) Run(_ context.Context, input map[string]any, _ Context) (Result, error) {
	if s.Source == nil {
		return Result{}, fmt.Errorf("skill: not available")
	}
	name, _ := input["name"].(string)
	if name == "" {
		return Result{}, fmt.Errorf("skill: name is required")
	}
	body, err := s.Source.Load(name)
	if err != nil {
		return Result{}, err
	}
	return Result{Title: name, Output: body}, nil
}

// TaskRequest describes a subagent task.
type TaskRequest struct {
	Description     string
	Prompt          string
	Agent           string
	ParentSessionID string
}

// SubagentRunner spawns a nested agent run and returns its final text (M9 loop).
type SubagentRunner interface {
	Run(ctx context.Context, req TaskRequest) (string, error)
}

// Task spawns a subagent session via an injected runner.
type Task struct{ Runner SubagentRunner }

// Info describes the task tool.
func (Task) Info() Info {
	return Info{
		ID:          "task",
		Description: "Delegate a sub-task to a subagent and return its result.",
		Parameters: obj(map[string]any{
			"description": strProp("A short description of the task."),
			"prompt":      strProp("The full instructions for the subagent."),
			"agent":       strProp("Which agent to run the task as."),
		}, "description", "prompt"),
	}
}

type taskParams struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Agent       string `json:"agent"`
}

// Run delegates to the injected runner.
func (t Task) Run(ctx context.Context, input map[string]any, tctx Context) (Result, error) {
	if t.Runner == nil {
		return Result{}, fmt.Errorf("task: not available")
	}
	var p taskParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	out, err := t.Runner.Run(ctx, TaskRequest{Description: p.Description, Prompt: p.Prompt,
		Agent: p.Agent, ParentSessionID: tctx.SessionID})
	if err != nil {
		return Result{}, err
	}
	return Result{Title: p.Description, Output: out}, nil
}
