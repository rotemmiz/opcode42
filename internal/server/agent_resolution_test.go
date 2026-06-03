package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/auth"
	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/engine/enginetest"
	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/registry"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/session"
	"github.com/rotemmiz/forge/internal/storage"
	"github.com/rotemmiz/forge/internal/worktree"
)

func TestResolveAgentAndRulesets(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	dir := t.TempDir()
	writeMD(t, dir, ".opencode/agent/locked.md", "---\npermission:\n  bash: ask\n---\nlocked")

	// build → allow-all (regression guard).
	build := resolveAgent(dir, "build")
	if build.Name != "build" {
		t.Fatalf("build agent not resolved: %+v", build)
	}
	rs := agentRulesets(build)
	if len(rs) != 1 || len(rs[0]) != 1 || rs[0][0].Action != permission.ActionAllow {
		t.Fatalf("build rulesets not allow-all: %+v", rs)
	}

	// unknown agent → falls back to build.
	if resolveAgent(dir, "does-not-exist").Name != "build" {
		t.Fatal("unknown agent should fall back to build")
	}

	// locked → its own ruleset (bash:ask), so the engine will prompt on bash.
	locked := resolveAgent(dir, "locked")
	if locked.Name != "locked" {
		t.Fatalf("locked agent not resolved: %+v", locked)
	}
	lr := agentRulesets(locked)
	if len(lr) != 1 || len(lr[0]) == 0 || lr[0][0].Permission != "bash" || lr[0][0].Action != permission.ActionAsk {
		t.Fatalf("locked rulesets wrong: %+v", lr)
	}
}

// TestPrompt_RestrictiveAgentTriggersPermission proves the agent's ruleset
// reaches the engine: a `locked` agent (bash:ask) running a bash tool call
// publishes permission.asked and blocks until a reply.
func TestPrompt_RestrictiveAgentTriggersPermission(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "cfg"))
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dir := t.TempDir()
	writeMD(t, dir, ".opencode/agent/locked.md", "---\npermission:\n  bash: ask\n---\nlocked agent")
	resolved := worktree.Resolve(dir)

	sessions := session.NewStore(db)
	sess, err := sessions.Create(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	// Non-default title so the forked step-0 title stream does not consume a
	// script slot / race the asserted permission flow.
	if err := sessions.SetTitle(context.Background(), sess.ID, "named"); err != nil {
		t.Fatal(err)
	}
	instances := instance.NewManager(bus.NewGlobal())
	usage := llm.TokenUsage{Input: 1, Output: 1}
	mock := enginetest.NewMockProvider(
		enginetest.NewScript().StepStart().
			ToolCall("c1", "bash", map[string]any{"command": "ls"}).
			StepFinish("tool_calls", usage).Finish().Events(),
		enginetest.NewScript().StepStart().Text("t", "done").
			StepFinish("stop", usage).Finish().Events(),
	)
	h, err := New(Options{
		Version: "test", Auth: auth.Config{}, Cwd: dir,
		Sessions: sessions, Instances: instances, Messages: message.NewStore(db),
		Catalog: catalog.Fixture(), Registry: registry.New(tool.Bash{}),
		Providers: func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	inst := instances.Get(resolved)
	sub, _ := inst.Bus.Subscribe()

	// Fire the prompt async (it blocks in the engine on the permission ask).
	body := map[string]any{
		"agent": "locked",
		"model": map[string]string{"providerID": "openai", "modelID": "gpt-4o"},
		"parts": []map[string]any{{"type": "text", "text": "list files"}},
	}
	buf, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/session/"+sess.ID+"/prompt_async", bytes.NewReader(buf))
	r.Header.Set("x-opencode-directory", dir)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("async prompt status = %d", rr.Code)
	}

	// The bash call must surface as permission.asked (it would not under allow-all).
	var reqID string
	deadline := time.After(3 * time.Second)
	for reqID == "" {
		select {
		case ev := <-sub:
			if ev.Type == "permission.asked" {
				req, ok := ev.Properties.(permission.Request)
				if !ok {
					t.Fatalf("permission.asked properties type %T", ev.Properties)
				}
				if req.Permission != "bash" {
					t.Fatalf("expected bash permission, got %q", req.Permission)
				}
				reqID = req.ID
			}
		case <-deadline:
			t.Fatal("permission.asked never published — agent ruleset not applied")
		}
	}

	// Replying unblocks the run (reject ends it cleanly; no hang).
	if err := inst.Permissions.Reply(reqID, permission.ReplyReject); err != nil {
		t.Fatalf("reply: %v", err)
	}
}
