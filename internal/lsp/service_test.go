package lsp

import (
	"context"
	"os/exec"
	"sync"
	"testing"

	"github.com/rotemmiz/forge/internal/config"
)

// fakeResolver returns canned binary paths so tests don't depend on real LSP
// servers. An empty path simulates a missing binary (which lands the server in
// the broken set).
type fakeResolver struct {
	paths map[string]string
	gopls string
}

func (f fakeResolver) Which(name string) string { return f.paths[name] }
func (f fakeResolver) EnsureGopls() string      { return f.gopls }

// newSvc builds a service whose connect hook skips the real LSP handshake, so
// the lifecycle/dedup/broken tests can use a stand-in process (e.g. `sleep`) for
// a server binary without it needing to speak LSP. The handshake itself is
// covered by the live gopls integration test.
func newSvc(t *testing.T, dir string, r BinResolver) *Service {
	t.Helper()
	s := NewService(dir, config.LSPConfig{Enabled: true}, r)
	s.connect = func(_ context.Context, _ ServerDef, _ string, _ *handle) (*Client, error) {
		return nil, nil // process spawned; no JSON-RPC client attached
	}
	return s
}

func TestActiveServers_DisabledConfig(t *testing.T) {
	s := NewService(t.TempDir(), config.LSPConfig{}, fakeResolver{})
	if len(s.servers) != 0 {
		t.Fatalf("disabled config should yield no active servers, got %d", len(s.servers))
	}
}

func TestActiveServers_DisabledBuiltin(t *testing.T) {
	cfg := config.LSPConfig{Enabled: true, Servers: map[string]config.LSPEntry{}}
	// Mark gopls disabled via JSON to set the unexported disabledSet flag.
	var e config.LSPEntry
	mustUnmarshal(t, &e, `{"disabled":true}`)
	cfg.Servers["gopls"] = e

	s := NewService(t.TempDir(), cfg, fakeResolver{})
	if _, ok := s.servers["gopls"]; ok {
		t.Fatalf("gopls should be excluded when disabled in config")
	}
	if _, ok := s.servers["pyright"]; !ok {
		t.Fatalf("pyright should remain active")
	}
}

func TestEnsureClients_MissingBinaryGoesBroken(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	src := touch(t, dir, "main.go")

	// gopls resolver returns "" → unavailable → broken.
	s := newSvc(t, dir, fakeResolver{gopls: ""})
	ids, err := s.EnsureClients(src)
	if err == nil {
		t.Fatalf("missing gopls should return an error")
	}
	if len(ids) != 0 {
		t.Fatalf("no client should be running, got %v", ids)
	}
	broken := s.brokenSnapshot()
	if len(broken) != 1 {
		t.Fatalf("expected one broken key, got %v", broken)
	}

	// Second call must not retry (still broken, no new attempt observable here).
	if _, err := s.EnsureClients(src); err == nil {
		t.Fatalf("broken server should keep erroring")
	}
}

func TestEnsureClients_NoMatchingExtension(t *testing.T) {
	dir := t.TempDir()
	src := touch(t, dir, "README.md")
	s := newSvc(t, dir, fakeResolver{})
	ids, err := s.EnsureClients(src)
	if err != nil {
		t.Fatalf("non-matching extension should be a no-op, got %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("no server matches .md, got %v", ids)
	}
}

func TestEnsureClients_ExtensionIsolation(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	py := touch(t, dir, "script.py")

	// gopls available, but a .py file must not spawn gopls.
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not available")
	}
	s := newSvc(t, dir, fakeResolver{gopls: sleep, paths: map[string]string{"pyright-langserver": ""}})
	ids, _ := s.EnsureClients(py)
	for _, id := range ids {
		if id == "gopls" {
			t.Fatalf("gopls must not spawn for a .py file")
		}
	}
}

func TestSpawnDedupAndDispose(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	src := touch(t, dir, "main.go")

	// Use a long-lived process (`sleep`) as a stand-in for gopls so spawn
	// succeeds without a real LSP server.
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not available")
	}
	// Wrap so the spawned argv is `sleep 60`.
	r := fakeResolver{gopls: sleep}
	s := newSvc(t, dir, r)
	// Override the gopls command to add an arg so the process stays alive.
	s.servers["gopls"] = ServerDef{
		ID:         "gopls",
		Extensions: []string{".go"},
		Root:       goplsRoot,
		Command: func(_ string, res BinResolver) ([]string, error) {
			return []string{res.EnsureGopls(), "60"}, nil
		},
	}

	// Concurrent EnsureClients for the same file should dedup to one process.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.EnsureClients(src)
		}()
	}
	wg.Wait()

	s.mu.Lock()
	running := len(s.running)
	s.mu.Unlock()
	if running != 1 {
		t.Fatalf("dedup should yield exactly one running process, got %d", running)
	}
	if got := s.runningIDs(); len(got) != 1 || got[0] != "gopls" {
		t.Fatalf("runningIDs should report [gopls], got %v", got)
	}

	// DisposeAll must terminate the process group and be idempotent.
	s.DisposeAll()
	s.DisposeAll()
	s.mu.Lock()
	running = len(s.running)
	disposed := s.disposed
	s.mu.Unlock()
	if running != 0 || !disposed {
		t.Fatalf("DisposeAll should clear running and mark disposed (running=%d disposed=%v)", running, disposed)
	}

	// Spawning after dispose is a no-op.
	ids, _ := s.EnsureClients(src)
	if len(ids) != 0 {
		t.Fatalf("EnsureClients after dispose should be a no-op, got %v", ids)
	}
}

func mustUnmarshal(t *testing.T, v *config.LSPEntry, raw string) {
	t.Helper()
	if err := v.UnmarshalJSON([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}
