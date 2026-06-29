package pluginbridge

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// findRuntime returns the first available JS runtime, or "" if none. The
// integration tests skip cleanly when no runtime is installed so CI stays green
// and fast on hosts without bun/node.
func findRuntime() string {
	for _, bin := range []string{"bun", "node"} {
		if _, err := exec.LookPath(bin); err == nil {
			return bin
		}
	}
	return ""
}

// hostScriptPath locates packages/opcode42-plugin-host/src/index.ts relative to
// this test file (internal/pluginbridge → repo root → packages/...).
func hostScriptPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(repoRoot, "packages", "opcode42-plugin-host", "src", "index.ts")
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "testdata")
}

// startHostBridge spawns the real sidecar and waits for ready. Skips if no
// runtime is available.
func startHostBridge(t *testing.T) *Bridge {
	t.Helper()
	if findRuntime() == "" {
		t.Skip("no JS runtime (bun/node) available; skipping plugin-host integration test")
	}
	script := hostScriptPath(t)

	b := New(Config{
		Enabled:    true,
		Directory:  fixtureDir(t),
		ServerURL:  "http://127.0.0.1:0",
		AuthHeader: "Basic dGVzdDp0ZXN0",
		HostScript: script,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start real plugin host: %v", err)
	}
	t.Cleanup(func() { b.Stop(context.Background()) })
	return b
}

func TestIntegration_HostStartsAndReady(t *testing.T) {
	b := startHostBridge(t)
	if !b.Ready() {
		t.Fatal("bridge not ready after Start")
	}
}

func TestIntegration_ChatParamsHookMutates(t *testing.T) {
	b := startHostBridge(t)
	// Give the host a moment to deliver plugin.tools after ready.
	waitForTools(t, b, 1)

	out := map[string]any{"temperature": 0.7, "topP": 0.9}
	b.Trigger(context.Background(), HookChatParams,
		map[string]any{"sessionID": "s1", "agent": "build"}, &out)
	if out["temperature"] != 0.123 {
		t.Fatalf("fixture chat.params hook did not mutate temperature: %v", out)
	}
	if out["topP"] != 0.9 {
		t.Fatalf("fixture hook dropped untouched field: %v", out)
	}
}

func TestIntegration_PluginToolRegistersAndExecutes(t *testing.T) {
	b := startHostBridge(t)
	waitForTools(t, b, 1)

	tools := b.Tools()
	if len(tools) != 1 || tools[0].ID != "fixture_echo" {
		t.Fatalf("expected fixture_echo tool, got %v", tools)
	}

	res, err := b.ExecuteTool(context.Background(), "fixture_echo",
		json.RawMessage(`{"text":"hello"}`), map[string]any{"sessionID": "s1"})
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if res.Output != "hello" {
		t.Fatalf("fixture tool output mismatch: %+v", res)
	}
}

func TestIntegration_EventDoesNotError(t *testing.T) {
	b := startHostBridge(t)
	waitForTools(t, b, 1)
	// Fire-and-forget; just assert it does not panic/block.
	b.Event("session.idle", map[string]any{"sessionID": "s1"})
	time.Sleep(100 * time.Millisecond)
	if !b.Ready() {
		t.Fatal("bridge dropped readiness after event")
	}
}

func waitForTools(t *testing.T, b *Bridge, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for len(b.Tools()) < n {
		select {
		case <-deadline:
			t.Fatalf("plugin tools not registered within timeout (have %d, want %d)", len(b.Tools()), n)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
