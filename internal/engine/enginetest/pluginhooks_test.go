package enginetest

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
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

// recordingHooks is a test engine.PluginHooks that records each hook name it is
// asked to trigger and can optionally mutate the output, standing in for the
// real flag-gated bridge. This proves the plan-05 call sites exist and route
// through the bridge (the review pass's "Validation fix"), not merely that no
// subprocess spawns.
type recordingHooks struct {
	mu     sync.Mutex
	names  []string
	mutate func(name string, out any)
}

func (r *recordingHooks) Trigger(_ context.Context, name string, _ any, out any) {
	r.mu.Lock()
	r.names = append(r.names, name)
	r.mu.Unlock()
	if r.mutate != nil {
		r.mutate(name, out)
	}
}

func (r *recordingHooks) triggered(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.names {
		if n == name {
			return true
		}
	}
	return false
}

func newRigWithHooks(t *testing.T, hooks engine.PluginHooks, scripts ...[]llm.Event) *rig {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const sessionID = "ses_hooks"
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
	reg := registry.New(tool.Bash{}, tool.Read{}, tool.Write{})
	dir := t.TempDir()
	eng := engine.New(engine.Config{
		Store:       store,
		Catalog:     catalog.Fixture(),
		Registry:    reg,
		Permissions: permission.NewManager(b),
		Bus:         b,
		Directory:   dir,
		Rulesets:    []permission.Ruleset{{{Permission: "*", Pattern: "*", Action: permission.ActionAllow}}},
		Providers:   func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
		Plugins:     hooks,
	})
	return &rig{eng: eng, store: store, bus: b, sub: sub, sessionID: sessionID, mock: mock, dir: dir}
}

// TestPluginHookCallSitesFire asserts the loop routes the request-build hooks
// (chat.params, chat.headers, experimental.chat.system.transform) through the
// configured plugin bridge on a normal turn.
func TestPluginHookCallSitesFire(t *testing.T) {
	script := NewScript().StepStart().Text("t1", "hi").
		StepFinish("stop", llm.TokenUsage{Input: 5, Output: 1}).Finish().Events()
	hooks := &recordingHooks{}
	r := newRigWithHooks(t, hooks, script)

	r.prompt(t, "hello")

	for _, name := range []string{"chat.params", "chat.headers", "experimental.chat.system.transform"} {
		if !hooks.triggered(name) {
			t.Errorf("expected hook %q to fire at the loop request-build call site", name)
		}
	}
}

// TestPluginParamsHookMutatesRequest asserts a plugin's chat.params mutation is
// applied to the outgoing LLM request (temperature carried into the provider).
func TestPluginParamsHookMutatesRequest(t *testing.T) {
	script := NewScript().StepStart().Text("t1", "hi").
		StepFinish("stop", llm.TokenUsage{Input: 5, Output: 1}).Finish().Events()
	temp := 0.42
	hooks := &recordingHooks{mutate: func(name string, out any) {
		if name != "chat.params" {
			return
		}
		if m, ok := out.(*engine.ChatParamsOutput); ok {
			m.Temperature = &temp
		}
	}}
	r := newRigWithHooks(t, hooks, script)
	r.prompt(t, "hello")

	reqs := r.mock.Requests()
	if len(reqs) == 0 {
		t.Fatal("no LLM request issued")
	}
	req := reqs[0]
	if req.Temperature == nil {
		t.Fatalf("expected temperature set on request, got %+v", req)
	}
	if *req.Temperature != temp {
		t.Fatalf("temperature not applied: got %v want %v", *req.Temperature, temp)
	}
}

// TestNilPluginHooksIsNoOp asserts that with no bridge configured the loop runs
// normally and applies no plugin mutations.
func TestNilPluginHooksIsNoOp(t *testing.T) {
	script := NewScript().StepStart().Text("t1", "hi").
		StepFinish("stop", llm.TokenUsage{Input: 5, Output: 1}).Finish().Events()
	r := newRigWithHooks(t, nil, script)
	out := r.prompt(t, "hello")
	if out.Info.Assistant == nil || out.Info.Assistant.Finish != "stop" {
		t.Fatalf("loop should finish normally with nil hooks: %+v", out.Info.Assistant)
	}
	if reqs := r.mock.Requests(); len(reqs) > 0 && reqs[0].Temperature != nil {
		t.Fatalf("nil hooks should not set temperature: %+v", reqs[0])
	}
}

// TestPluginChatMessageHookFires asserts the chat.message hook fires once after
// the user message and its parts are stored (prompt.ts:1073).
func TestPluginChatMessageHookFires(t *testing.T) {
	script := NewScript().StepStart().Text("t1", "hi").
		StepFinish("stop", llm.TokenUsage{Input: 5, Output: 1}).Finish().Events()
	hooks := &recordingHooks{}
	r := newRigWithHooks(t, hooks, script)
	r.prompt(t, "hello")
	if !hooks.triggered("chat.message") {
		t.Errorf("expected chat.message to fire after the user message is stored")
	}
}

// TestPluginMessagesTransformHookFires asserts the messages.transform hook fires
// at the loop serialization boundary (prompt.ts:1433).
func TestPluginMessagesTransformHookFires(t *testing.T) {
	script := NewScript().StepStart().Text("t1", "hi").
		StepFinish("stop", llm.TokenUsage{Input: 5, Output: 1}).Finish().Events()
	hooks := &recordingHooks{}
	r := newRigWithHooks(t, hooks, script)
	r.prompt(t, "hello")
	if !hooks.triggered("experimental.chat.messages.transform") {
		t.Errorf("expected experimental.chat.messages.transform to fire before serialization")
	}
}

// TestPluginToolExecuteHooksFire asserts tool.execute.before/after fire around a
// tool run, exactly as opencode (session/tools.ts:87-107).
func TestPluginToolExecuteHooksFire(t *testing.T) {
	step1 := NewScript().StepStart().Text("t1", "writing").
		ToolCall("call_1", "write", map[string]any{"filePath": "out.txt", "content": "done"}).
		StepFinish("tool-calls", llm.TokenUsage{Input: 20, Output: 8}).Finish().Events()
	step2 := NewScript().StepStart().Text("t2", "wrote it").
		StepFinish("stop", llm.TokenUsage{Input: 30, Output: 4}).Finish().Events()
	hooks := &recordingHooks{}
	r := newRigWithHooks(t, hooks, step1, step2)
	r.prompt(t, "write out.txt")

	for _, name := range []string{"tool.execute.before", "tool.execute.after"} {
		if !hooks.triggered(name) {
			t.Errorf("expected hook %q to fire around the tool run", name)
		}
	}
}

// TestPluginToolAfterHookMutatesOutput asserts a plugin's tool.execute.after
// rewrite of the result output is persisted on the completed tool part. The
// mutate callback edits the message-shaped output the bridge would hand back.
func TestPluginToolAfterHookMutatesOutput(t *testing.T) {
	step1 := NewScript().StepStart().Text("t1", "writing").
		ToolCall("call_1", "write", map[string]any{"filePath": "out.txt", "content": "data"}).
		StepFinish("tool-calls", llm.TokenUsage{Input: 20, Output: 8}).Finish().Events()
	step2 := NewScript().StepStart().Text("t2", "done").
		StepFinish("stop", llm.TokenUsage{Input: 30, Output: 4}).Finish().Events()
	const rewritten = "REWRITTEN BY PLUGIN"
	hooks := &recordingHooks{mutate: func(name string, out any) {
		if name != "tool.execute.after" {
			return
		}
		setField(out, "Output", rewritten)
		setField(out, "Title", "plugin-title")
	}}
	r := newRigWithHooks(t, hooks, step1, step2)
	r.prompt(t, "write out.txt")

	msgs, _ := r.store.List(context.Background(), r.sessionID)
	var got message.ToolStateCompleted
	var found bool
	for _, m := range msgs {
		for _, p := range m.Parts {
			tp, ok := p.(*message.ToolPart)
			if ok && tp.Tool == "write" && tp.Status() == message.ToolCompleted {
				if err := json.Unmarshal(tp.State, &got); err == nil {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("no completed write tool part found")
	}
	if got.Output != rewritten {
		t.Errorf("tool.execute.after output not applied: got %q want %q", got.Output, rewritten)
	}
	if got.Title != "plugin-title" {
		t.Errorf("tool.execute.after title not applied: got %q", got.Title)
	}
}

// TestPluginCompactionHooksFire asserts the compaction hooks fire during an
// explicit summarize (compaction.ts:398, 405).
func TestPluginCompactionHooksFire(t *testing.T) {
	// Seed enough turns that selectTail keeps a head to summarize, then the
	// summary turn streams a short text.
	summary := NewScript().StepStart().Text("s1", "## Goal\n- done").
		StepFinish("stop", llm.TokenUsage{Input: 10, Output: 4}).Finish().Events()
	turns := make([][]llm.Event, 0, 4)
	for i := 0; i < 3; i++ {
		turns = append(turns, NewScript().StepStart().Text("t", "ok").
			StepFinish("stop", llm.TokenUsage{Input: 5, Output: 1}).Finish().Events())
	}
	turns = append(turns, summary)
	hooks := &recordingHooks{}
	r := newRigWithHooks(t, hooks, turns...)
	// Three real prompts build history.
	r.prompt(t, "one")
	r.prompt(t, "two")
	r.prompt(t, "three")
	if err := r.eng.Summarize(context.Background(), engine.SummarizeInput{
		SessionID: r.sessionID, Provider: "openai", Model: "gpt-4o",
	}); err != nil {
		t.Fatalf("summarize: %v", err)
	}
	for _, name := range []string{"experimental.session.compacting", "experimental.chat.messages.transform"} {
		if !hooks.triggered(name) {
			t.Errorf("expected hook %q to fire during compaction", name)
		}
	}
}

// setField sets a string field on a pointer-to-struct out value by name using
// reflection, mirroring how the real bridge unmarshals the host's mutated
// output back over the engine's typed hook output struct.
func setField(out any, field, value string) {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	f := v.Elem().FieldByName(field)
	if f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
		f.SetString(value)
	}
}
