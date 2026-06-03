package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/config"
)

// recorderBus records published event types so the live test can assert
// lsp.updated fires.
type recorderBus struct{ updated atomic.Bool }

func (r *recorderBus) Publish(typ string, _ any) {
	if typ == "lsp.updated" {
		r.updated.Store(true)
	}
}

// TestLiveGopls drives the real gopls binary if it is present on PATH. It is
// skip-gated so CI without gopls still passes. It exercises the full M3-4 path:
// lazy spawn + initialize handshake, the lsp.updated SSE event, TouchFile +
// diagnostics for a file with a compile error, the wire Status shape, and
// process-group teardown.
func TestLiveGopls(t *testing.T) {
	bin, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("gopls not on PATH; skipping live integration test")
	}

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/live\n\ngo 1.21\n")
	// An undeclared identifier is a guaranteed gopls diagnostic.
	src := writeFile(t, dir, "main.go", "package main\n\nfunc main() {\n\tx := undefinedSymbol\n\t_ = x\n}\n")

	bus := &recorderBus{}
	s := NewService(dir, config.LSPConfig{Enabled: true}, liveResolver{gopls: bin}).WithBus(bus)
	defer s.DisposeAll()

	ids, err := s.EnsureClients(src)
	if err != nil {
		t.Fatalf("EnsureClients(gopls): %v", err)
	}
	if !contains(ids, "gopls") {
		t.Fatalf("gopls should be running for a .go file, got %v", ids)
	}
	if !bus.updated.Load() {
		t.Fatalf("lsp.updated should fire after the first client spawns")
	}

	// Wire status shape: id/name/root/status, root relative to the instance dir.
	status := s.Status()
	if len(status) != 1 {
		t.Fatalf("Status should report exactly one server, got %v", status)
	}
	st := status[0]
	// Root is "" (not ".") when the server root IS the instance dir, matching
	// Node's path.relative used by opencode (lsp.ts:323).
	if st.ID != "gopls" || st.Name != "gopls" || st.Status != "connected" || st.Root != "" {
		t.Fatalf("unexpected status item: %+v", st)
	}

	// Touch the file in full mode and assert the compile error is surfaced.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.TouchFile(ctx, src, string(DiagModeFull))

	diags := s.Diagnostics()
	got := diags[filepath.Clean(src)]
	if len(got) == 0 {
		t.Fatalf("expected gopls diagnostics for %s, got none (all: %v)", src, diags)
	}
}

// writeFile writes content to root/rel (creating parents) and returns the path.
func writeFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

type liveResolver struct{ gopls string }

func (r liveResolver) Which(name string) string {
	if name == "gopls" {
		return r.gopls
	}
	return ""
}
func (r liveResolver) EnsureGopls() string { return r.gopls }
