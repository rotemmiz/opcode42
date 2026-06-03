package enginetest

import (
	"context"
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
	reg := registry.New(tool.Bash{}, tool.Read{})
	eng := engine.New(engine.Config{
		Store:       store,
		Catalog:     catalog.Fixture(),
		Registry:    reg,
		Permissions: permission.NewManager(b),
		Bus:         b,
		Directory:   t.TempDir(),
		Rulesets:    []permission.Ruleset{{{Permission: "*", Pattern: "*", Action: permission.ActionAllow}}},
		Providers:   func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
		Plugins:     hooks,
	})
	return &rig{eng: eng, store: store, bus: b, sub: sub, sessionID: sessionID, mock: mock}
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
