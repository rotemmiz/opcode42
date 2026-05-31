package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/rotemmiz/forge/internal/engine/question"
)

// These tools depend on collaborators delivered by later milestones/plans
// (question manager, web search provider, skill loader, the agent loop for
// subagents). Each takes its dependency as an interface so it is unit-testable
// now; an unset dependency yields a clear "not available" error rather than a
// panic.

// Asker asks the user one or more questions and blocks for the answers, one
// selected-label array per question, in order (question.Manager).
type Asker interface {
	Ask(ctx context.Context, sessionID string, questions []question.Info) ([][]string, error)
}

// Question asks the user one or more multiple-choice questions and returns their
// answers (question/index.ts; tool/question.ts). Its Asker is the per-instance
// question manager, injected at execution time via tool.Context.Questioner
// (mirroring how the executor delivers the permission gate).
type Question struct{}

// Info describes the question tool. The parameters mirror opencode's
// QuestionPrompt array (tool/question.ts): each question has a header, the
// question text, and selectable options ({label, description}).
func (Question) Info() Info {
	option := obj(map[string]any{
		"label":       strProp("The option's display label."),
		"description": strProp("Explanation of the choice."),
	}, "label", "description")
	question := obj(map[string]any{
		"question": strProp("The complete question."),
		"header":   strProp("A very short label (max 30 chars)."),
		"options":  map[string]any{"type": "array", "items": option, "description": "Available choices."},
		"multiple": map[string]any{"type": "boolean", "description": "Allow selecting multiple choices."},
	}, "question", "header", "options")
	return Info{
		ID:          "question",
		Description: "Ask the user one or more multiple-choice questions and wait for their answers.",
		Parameters: obj(map[string]any{
			"questions": map[string]any{"type": "array", "items": question, "description": "Questions to ask."},
		}, "questions"),
	}
}

type questionParams struct {
	Questions []question.Info `json:"questions"`
}

// Run asks via the injected Asker and formats the answers back to the model,
// matching opencode's question tool output (tool/question.ts).
func (q Question) Run(ctx context.Context, input map[string]any, tctx Context) (Result, error) {
	if tctx.Questioner == nil {
		return Result{}, fmt.Errorf("question: not available")
	}
	var p questionParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	answers, err := tctx.Questioner.Ask(ctx, tctx.SessionID, p.Questions)
	if err != nil {
		return Result{}, err
	}
	pairs := make([]string, 0, len(p.Questions))
	for i, q := range p.Questions {
		ans := "Unanswered"
		if i < len(answers) && len(answers[i]) > 0 {
			ans = strings.Join(answers[i], ", ")
		}
		pairs = append(pairs, fmt.Sprintf("%q=%q", q.Question, ans))
	}
	noun := "question"
	if len(p.Questions) > 1 {
		noun = "questions"
	}
	return Result{
		Title: fmt.Sprintf("Asked %d %s", len(p.Questions), noun),
		Output: "User has answered your questions: " + strings.Join(pairs, ", ") +
			". You can now continue with the user's answers in mind.",
		Metadata: map[string]any{"answers": answers},
	}, nil
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

// Task spawns a subagent session. Its runner is injected at execution time via
// tool.Context.Subagent (per-instance, like the question manager).
type Task struct{}

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
	if tctx.Subagent == nil {
		return Result{}, fmt.Errorf("task: not available")
	}
	var p taskParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	out, err := tctx.Subagent.Run(ctx, TaskRequest{Description: p.Description, Prompt: p.Prompt,
		Agent: p.Agent, ParentSessionID: tctx.SessionID})
	if err != nil {
		return Result{}, err
	}
	return Result{Title: p.Description, Output: out}, nil
}
