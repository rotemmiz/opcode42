package enginetest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/registry"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/storage"
)

// rig is a full engine wired over an in-memory store + bus + mock provider, the
// deterministic integration harness for the M9 text-only and tool-call gates.
type rig struct {
	eng       *engine.Engine
	store     *message.Store
	bus       *bus.Bus
	sub       <-chan bus.Event
	sessionID string
	mock      *MockProvider
	dir       string
}

func newRig(t *testing.T, scripts ...[]llm.Event) *rig {
	t.Helper()
	return newRigInDir(t, t.TempDir(), scripts...)
}

func newRigInDir(t *testing.T, dir string, scripts ...[]llm.Event) *rig {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const sessionID = "ses_e2e"
	if _, err := db.Exec(`INSERT INTO project (id, worktree, time_created, time_updated) VALUES ('p','/tmp',0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO session (id, project_id, slug, directory, version, time_created, time_updated)
		VALUES (?, 'p','s','/tmp','1',0,0)`, sessionID); err != nil {
		t.Fatal(err)
	}
	store := message.NewStore(db)
	b := bus.NewInstanceBus(sessionID, nil)
	sub, _ := b.Subscribe()
	mock := NewMockProvider(scripts...)

	reg := registry.New(tool.Bash{}, tool.Read{}, tool.Write{}, tool.Edit{}, tool.Glob{}, tool.Grep{})
	eng := engine.New(engine.Config{
		Store:       store,
		Catalog:     catalog.Fixture(),
		Registry:    reg,
		Permissions: permission.NewManager(b),
		Bus:         b,
		Directory:   dir,
		// Allow all tools so the loop runs unattended in tests.
		Rulesets:  []permission.Ruleset{{{Permission: "*", Pattern: "*", Action: permission.ActionAllow}}},
		Providers: func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
	})
	return &rig{eng: eng, store: store, bus: b, sub: sub, sessionID: sessionID, mock: mock, dir: dir}
}

func (r *rig) prompt(t *testing.T, text string) message.WithParts {
	t.Helper()
	out, err := r.eng.Prompt(context.Background(), engine.PromptInput{
		SessionID: r.sessionID, Provider: "openai", Model: "gpt-4o",
		Parts: []engine.PartInput{{Type: "text", Text: text}},
	})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	return out
}

func (r *rig) drain() []bus.Event {
	var out []bus.Event
	for {
		select {
		case e := <-r.sub:
			out = append(out, e)
		default:
			return out
		}
	}
}

func eventTypes(events []bus.Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

func countType(events []bus.Event, typ string) int {
	n := 0
	for _, e := range events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// summarize renders SSE events compactly, annotating deltas with their text.
func summarize(events []bus.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		s := strings.TrimPrefix(e.Type, "message.")
		if e.Type == "message.part.delta" {
			if props, ok := e.Properties.(map[string]any); ok {
				s += "(" + asString(props["delta"]) + ")"
			}
		}
		out = append(out, s)
	}
	return out
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// roles renders a model-message list as role(content-kinds).
func roles(msgs []llm.ModelMessage) string {
	var parts []string
	for _, m := range msgs {
		var kinds []string
		for _, c := range m.Content {
			kinds = append(kinds, string(c.Kind))
		}
		parts = append(parts, string(m.Role)+"["+strings.Join(kinds, ",")+"]")
	}
	return strings.Join(parts, " ")
}

// dumpConversation logs each persisted message and its parts.
func dumpConversation(t *testing.T, msgs []message.WithParts) {
	t.Helper()
	for _, m := range msgs {
		t.Logf("  %s message %s:", m.Info.Role(), m.Info.ID())
		for _, p := range m.Parts {
			switch v := p.(type) {
			case *message.TextPart:
				t.Logf("    text: %q", v.Text)
			case *message.ToolPart:
				t.Logf("    tool %s (callID=%s) status=%s", v.Tool, v.CallID, v.Status())
			case *message.StepStartPart:
				t.Logf("    step-start")
			case *message.StepFinishPart:
				t.Logf("    step-finish reason=%s cost=%.6f tokens(in=%v out=%v)", v.Reason, v.Cost, v.Tokens.Input, v.Tokens.Output)
			default:
				t.Logf("    %T", p)
			}
		}
	}
}

// Scenario 1: a single text prompt produces a streamed text response.
func TestE2E_TextOnly(t *testing.T) {
	script := NewScript().StepStart().Text("t1", "Hello", ", world").
		StepFinish("stop", llm.TokenUsage{Input: 10, Output: 5}).Finish().Events()
	r := newRig(t, script)

	out := r.prompt(t, "hi there")

	if out.Info.Assistant == nil || out.Info.Assistant.Finish != "stop" {
		t.Fatalf("assistant did not finish stop: %+v", out.Info.Assistant)
	}
	// Final assistant carries the streamed text.
	var text string
	for _, p := range out.Parts {
		if tp, ok := p.(*message.TextPart); ok {
			text += tp.Text
		}
	}
	if text != "Hello, world" {
		t.Fatalf("assistant text = %q, want %q", text, "Hello, world")
	}

	events := r.drain()
	t.Logf("assistant text: %q  (finish=%s, tokens in=%v out=%v)",
		text, out.Info.Assistant.Finish, out.Info.Assistant.Tokens.Input, out.Info.Assistant.Tokens.Output)
	t.Logf("SSE sequence (%d events): %s", len(events), strings.Join(summarize(events), " → "))
	// User message, assistant placeholder, streamed deltas, and a final message.updated.
	if countType(events, "message.part.delta") != 2 {
		t.Fatalf("want 2 part.delta, got %d (%v)", countType(events, "message.part.delta"), eventTypes(events))
	}
	if countType(events, "message.updated") < 2 {
		t.Fatalf("want >=2 message.updated (user + assistant), got %d", countType(events, "message.updated"))
	}
	if countType(events, "message.part.updated") == 0 {
		t.Fatalf("missing message.part.updated events")
	}

	// DB: exactly one user + one assistant message persisted.
	msgs, _ := r.store.List(context.Background(), r.sessionID)
	if len(msgs) != 2 || !msgs[0].Info.IsUser() || msgs[1].Info.Assistant == nil {
		t.Fatalf("want [user, assistant] persisted, got %d messages", len(msgs))
	}
	dumpConversation(t, msgs)
	if r.mock.Calls() != 1 {
		t.Fatalf("want 1 provider call, got %d", r.mock.Calls())
	}
}

// Scenario 2: a tool call is executed, fed back, and a final answer streamed.
func TestE2E_ToolCall(t *testing.T) {
	dir := t.TempDir()
	// Step 1: model asks to write a file. Step 2: model answers with text.
	step1 := NewScript().StepStart().
		ToolCall("call_1", "write", map[string]any{"filePath": "out.txt", "content": "done"}).
		StepFinish("tool-calls", llm.TokenUsage{Input: 20, Output: 8}).Finish().Events()
	step2 := NewScript().StepStart().Text("t2", "Wrote the file.").
		StepFinish("stop", llm.TokenUsage{Input: 30, Output: 4}).Finish().Events()

	r := newRigInDir(t, dir, step1, step2)
	out := r.prompt(t, "write out.txt")

	t.Logf("SSE sequence: %s", strings.Join(summarize(r.drain()), " → "))

	if out.Info.Assistant == nil || out.Info.Assistant.Finish != "stop" {
		t.Fatalf("final assistant should finish stop: %+v", out.Info.Assistant)
	}
	if r.mock.Calls() != 2 {
		t.Fatalf("want 2 provider calls (tool round-trip), got %d", r.mock.Calls())
	}

	// The first assistant message holds a completed tool part.
	msgs, _ := r.store.List(context.Background(), r.sessionID)
	var sawCompletedTool bool
	for _, m := range msgs {
		for _, p := range m.Parts {
			if tp, ok := p.(*message.ToolPart); ok && tp.Tool == "write" && tp.Status() == message.ToolCompleted {
				sawCompletedTool = true
			}
		}
	}
	if !sawCompletedTool {
		t.Fatalf("no completed write tool part found")
	}
	t.Logf("provider calls: %d", r.mock.Calls())
	dumpConversation(t, msgs)
	// The second provider request must include the tool result in its messages.
	reqs := r.mock.Requests()
	if len(reqs) != 2 {
		t.Fatalf("want 2 requests, got %d", len(reqs))
	}
	if !hasToolResult(reqs[1].Messages) {
		t.Fatalf("second request missing tool-result message: %+v", reqs[1].Messages)
	}
	t.Logf("2nd request messages (tool result fed back): %s", roles(reqs[1].Messages))
	// And the file was actually written by the tool.
	if data, err := readFile(dir, "out.txt"); err != nil || strings.TrimSpace(data) != "done" {
		t.Fatalf("tool did not write file: %q err=%v", data, err)
	}
}

// Scenario 3 (M11): a prompt emits the run-lock lifecycle SSE — session.status
// (busy) once the run starts and session.idle (+ a final session.status idle)
// when it completes — mirroring opencode's run-state onBusy/onIdle
// (run-state.ts:58-63, status.ts:77-86). The assistant message also carries the
// resolved agent as mode and the worktree root as path.root.
func TestE2E_SessionStatusLifecycle(t *testing.T) {
	script := NewScript().StepStart().Text("t1", "ok").
		StepFinish("stop", llm.TokenUsage{Input: 1, Output: 1}).Finish().Events()
	r := newRig(t, script)

	out := r.prompt(t, "hello")
	events := r.drain()
	types := eventTypes(events)
	t.Logf("SSE lifecycle: %v", types)

	// Exactly one session.idle (terminal), at least one session.status (busy +
	// idle), and ordering: busy precedes idle, idle is the last lifecycle event.
	if countType(events, "session.idle") != 1 {
		t.Fatalf("want 1 session.idle, got %d (%v)", countType(events, "session.idle"), types)
	}
	if countType(events, "session.status") < 1 {
		t.Fatalf("want >=1 session.status, got %d", countType(events, "session.status"))
	}
	firstBusy, lastIdle := -1, -1
	for i, e := range events {
		if e.Type == "session.status" {
			if props, ok := e.Properties.(map[string]any); ok {
				if st, _ := props["status"].(map[string]any); st != nil && st["type"] == "busy" && firstBusy < 0 {
					firstBusy = i
				}
			}
		}
		if e.Type == "session.idle" {
			lastIdle = i
		}
	}
	if firstBusy < 0 {
		t.Fatalf("no session.status{busy} emitted: %v", types)
	}
	if lastIdle < firstBusy {
		t.Fatalf("session.idle (%d) must follow session.status{busy} (%d)", lastIdle, firstBusy)
	}
	if lastIdle != len(events)-1 {
		t.Fatalf("session.idle must be the last event, got index %d of %d", lastIdle, len(events))
	}

	// Assistant carries mode = agent name (default "build") and path.root = "/"
	// (the temp dir is not a git worktree), mirroring opencode.
	a := out.Info.Assistant
	if a == nil || a.Mode != "build" {
		t.Fatalf("assistant mode = %q, want build", modeOf(a))
	}
	if a.Path.Root != "/" {
		t.Fatalf("assistant path.root = %q, want / (non-git temp dir)", a.Path.Root)
	}
}

func modeOf(a *message.AssistantMessage) string {
	if a == nil {
		return "<nil>"
	}
	return a.Mode
}

func hasToolResult(msgs []llm.ModelMessage) bool {
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Kind == llm.ContentToolResult {
				return true
			}
		}
	}
	return false
}

func readFile(dir, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	return string(data), err
}
