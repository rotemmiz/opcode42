package enginetest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/engine"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/llm"
	"github.com/rotemmiz/opcode42/internal/engine/message"
	"github.com/rotemmiz/opcode42/internal/engine/permission"
	"github.com/rotemmiz/opcode42/internal/engine/registry"
	"github.com/rotemmiz/opcode42/internal/engine/tool"
	"github.com/rotemmiz/opcode42/internal/storage"
)

// fakeTitles is an in-memory engine.TitleSetter that records the last set title.
type fakeTitles struct {
	mu      sync.Mutex
	title   string
	setCals int
}

func (f *fakeTitles) SetTitle(_ context.Context, _, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.title = title
	f.setCals++
	return nil
}

func (f *fakeTitles) IsDefaultTitle(title string) bool { return title == "New session - default" }

func (f *fakeTitles) Title(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.title, nil
}

func (f *fakeTitles) snapshot() (string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.title, f.setCals
}

// finalizationRig wires an engine with explicit MaxSteps / Format / Titles for
// the M9 finalization scenarios (A1/A2/A3).
type finalizationRig struct {
	eng       *engine.Engine
	store     *message.Store
	sessionID string
	mock      *MockProvider
	titles    *fakeTitles
}

type rigOpts struct {
	maxSteps int
	titles   *fakeTitles
}

func newFinalizationRig(t *testing.T, opt rigOpts, scripts ...[]llm.Event) *finalizationRig {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const sessionID = "ses_final"
	if _, err := db.Exec(`INSERT INTO project (id, worktree, time_created, time_updated) VALUES ('p','/tmp',0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated)
		VALUES (?, 'p','s','/tmp','1',0,0)`, sessionID); err != nil {
		t.Fatal(err)
	}
	store := message.NewStore(db)
	b := bus.NewInstanceBus(sessionID, nil)
	mock := NewMockProvider(scripts...)
	reg := registry.New(tool.Bash{}, tool.Read{}, tool.Write{}, tool.Edit{}, tool.Glob{}, tool.Grep{})
	cfg := engine.Config{
		Store:       store,
		Catalog:     catalog.Fixture(),
		Registry:    reg,
		Permissions: permission.NewManager(b),
		Bus:         b,
		Directory:   t.TempDir(),
		Rulesets:    []permission.Ruleset{{{Permission: "*", Pattern: "*", Action: permission.ActionAllow}}},
		Providers:   func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
		MaxSteps:    opt.maxSteps,
	}
	if opt.titles != nil {
		cfg.Titles = opt.titles
	}
	return &finalizationRig{
		eng: engine.New(cfg), store: store, sessionID: sessionID, mock: mock, titles: opt.titles,
	}
}

func (r *finalizationRig) prompt(t *testing.T, in engine.PromptInput) message.WithParts {
	t.Helper()
	in.SessionID = r.sessionID
	if in.Provider == "" {
		in.Provider = "openai"
	}
	if in.Model == "" {
		in.Model = "gpt-4o"
	}
	out, err := r.eng.Prompt(context.Background(), in)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	return out
}

// A1: with MaxSteps=1, the single (and last) step must carry the MAX_STEPS
// sentinel as an assistant message in the provider request.
func TestFinalization_MaxStepsSentinel(t *testing.T) {
	// The model keeps asking for a tool, but maxSteps=1 forces a single turn.
	step := NewScript().StepStart().
		ToolCall("call_1", "write", map[string]any{"filePath": "x.txt", "content": "y"}).
		StepFinish("tool-calls", llm.TokenUsage{Input: 5, Output: 2}).Finish().Events()
	r := newFinalizationRig(t, rigOpts{maxSteps: 1}, step)

	r.prompt(t, engine.PromptInput{Parts: []engine.PartInput{{Type: "text", Text: "do it"}}})

	if r.mock.Calls() != 1 {
		t.Fatalf("maxSteps=1 should yield exactly 1 provider call, got %d", r.mock.Calls())
	}
	reqs := r.mock.Requests()
	last := reqs[0].Messages
	tail := last[len(last)-1]
	if tail.Role != llm.RoleAssistant || len(tail.Content) == 0 {
		t.Fatalf("last message should be the assistant MAX_STEPS sentinel, got %+v", tail)
	}
	if !containsMaxSteps(tail.Content[0].Text) {
		t.Fatalf("sentinel text missing MAX_STEPS marker: %q", tail.Content[0].Text)
	}
}

// A higher MaxSteps must NOT append the sentinel on the first (non-last) step.
func TestFinalization_NoSentinelBeforeLastStep(t *testing.T) {
	step1 := NewScript().StepStart().
		ToolCall("call_1", "write", map[string]any{"filePath": "x.txt", "content": "y"}).
		StepFinish("tool-calls", llm.TokenUsage{Input: 5, Output: 2}).Finish().Events()
	step2 := NewScript().StepStart().Text("t", "done").
		StepFinish("stop", llm.TokenUsage{Input: 3, Output: 1}).Finish().Events()
	r := newFinalizationRig(t, rigOpts{maxSteps: 5}, step1, step2)

	r.prompt(t, engine.PromptInput{Parts: []engine.PartInput{{Type: "text", Text: "do it"}}})

	reqs := r.mock.Requests()
	if len(reqs) < 1 {
		t.Fatalf("expected at least 1 request")
	}
	for _, m := range reqs[0].Messages {
		if m.Role == llm.RoleAssistant {
			for _, c := range m.Content {
				if containsMaxSteps(c.Text) {
					t.Fatalf("sentinel must not appear on the first (non-last) step")
				}
			}
		}
	}
}

func containsMaxSteps(s string) bool {
	return len(s) > 0 && (indexOf(s, "MAXIMUM STEPS REACHED") >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// A2: a json_schema format injects the StructuredOutput tool, forces tool use,
// captures the structured payload, and finishes the run.
func TestFinalization_StructuredOutputSuccess(t *testing.T) {
	want := map[string]any{"answer": "42"}
	step := NewScript().StepStart().
		ToolCall("call_1", "StructuredOutput", want).
		StepFinish("tool-calls", llm.TokenUsage{Input: 5, Output: 2}).Finish().Events()
	r := newFinalizationRig(t, rigOpts{}, step)

	out := r.prompt(t, engine.PromptInput{
		Parts:  []engine.PartInput{{Type: "text", Text: "give me json"}},
		Format: &message.OutputFormat{Type: "json_schema", Schema: map[string]any{"type": "object"}},
	})

	if out.Info.Assistant == nil {
		t.Fatalf("no assistant message")
	}
	got, ok := out.Info.Assistant.Structured.(map[string]any)
	if !ok {
		t.Fatalf("structured output not captured: %#v", out.Info.Assistant.Structured)
	}
	if got["answer"] != "42" {
		t.Fatalf("structured payload = %#v, want answer=42", got)
	}
	if out.Info.Assistant.Finish != "stop" {
		t.Fatalf("structured run should finish stop, got %q", out.Info.Assistant.Finish)
	}
	if out.Info.Assistant.Error != nil {
		t.Fatalf("unexpected error: %+v", out.Info.Assistant.Error)
	}
	// The request advertised the StructuredOutput tool and forced tool use.
	req := r.mock.Requests()[0]
	if req.ToolChoice != llm.ToolChoiceRequired {
		t.Fatalf("toolChoice = %q, want required", req.ToolChoice)
	}
	if !hasTool(req.Tools, "StructuredOutput") {
		t.Fatalf("StructuredOutput tool not advertised: %+v", req.Tools)
	}
	if !hasSystemContaining(req.SystemPrompts, "StructuredOutput tool") {
		t.Fatalf("structured-output system prompt missing: %+v", req.SystemPrompts)
	}
}

// A2: a finished turn that never calls StructuredOutput surfaces a
// StructuredOutputError on the assistant message.
func TestFinalization_StructuredOutputError(t *testing.T) {
	step := NewScript().StepStart().Text("t", "plain text, no tool").
		StepFinish("stop", llm.TokenUsage{Input: 5, Output: 2}).Finish().Events()
	r := newFinalizationRig(t, rigOpts{}, step)

	out := r.prompt(t, engine.PromptInput{
		Parts:  []engine.PartInput{{Type: "text", Text: "give me json"}},
		Format: &message.OutputFormat{Type: "json_schema", Schema: map[string]any{"type": "object"}},
	})

	if out.Info.Assistant == nil || out.Info.Assistant.Error == nil {
		t.Fatalf("expected a StructuredOutputError, got %+v", out.Info.Assistant)
	}
	if out.Info.Assistant.Error.Name != "StructuredOutputError" {
		t.Fatalf("error name = %q, want StructuredOutputError", out.Info.Assistant.Error.Name)
	}
}

func hasTool(tools []llm.ToolDefinition, name string) bool {
	for _, td := range tools {
		if td.Name == name {
			return true
		}
	}
	return false
}

func hasSystemContaining(prompts []string, sub string) bool {
	for _, p := range prompts {
		if indexOf(p, sub) >= 0 {
			return true
		}
	}
	return false
}

// A3: step-0 forks title generation; the streamed title agent output sets the
// session title while it is still the default.
func TestFinalization_TitleGeneration(t *testing.T) {
	titles := &fakeTitles{title: "New session - default"}
	// The title stream is forked, so its provider call races the main turn's.
	// Route by the title agent's system prompt so each stream is deterministic.
	titleScript := NewScript().Text("tt", "Greeting message").Finish().Events()
	mainScript := NewScript().StepStart().Text("m", "hello!").
		StepFinish("stop", llm.TokenUsage{Input: 3, Output: 1}).Finish().Events()
	r := newFinalizationRig(t, rigOpts{titles: titles}, mainScript)
	r.mock.WithRoute(func(req *llm.Request) []llm.Event {
		if hasSystemContaining(req.SystemPrompts, "title generator") {
			return titleScript
		}
		return mainScript
	})

	r.prompt(t, engine.PromptInput{Parts: []engine.PartInput{{Type: "text", Text: "hey"}}})

	// Title generation is forked; poll briefly for the async SetTitle.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, n := titles.snapshot(); n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, n := titles.snapshot()
	if n == 0 {
		t.Fatalf("title was never set")
	}
	if got == "New session - default" || got == "" {
		t.Fatalf("title not updated from default, got %q", got)
	}
	t.Logf("generated title: %q", got)
}

// A3: title generation is skipped when no TitleSetter is configured (subagents).
func TestFinalization_TitleSkippedWithoutSetter(t *testing.T) {
	mainScript := NewScript().StepStart().Text("m", "hi").
		StepFinish("stop", llm.TokenUsage{Input: 1, Output: 1}).Finish().Events()
	r := newFinalizationRig(t, rigOpts{}, mainScript)
	r.prompt(t, engine.PromptInput{Parts: []engine.PartInput{{Type: "text", Text: "hey"}}})
	// Only the main run should have streamed (no extra title call).
	if r.mock.Calls() != 1 {
		t.Fatalf("expected 1 provider call (no title gen), got %d", r.mock.Calls())
	}
}
