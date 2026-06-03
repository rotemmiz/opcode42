package lsp

import (
	"os/exec"
	"testing"

	"github.com/rotemmiz/forge/internal/config"
)

// TestLiveGopls spawns the real gopls binary if it is present on PATH. It is
// skip-gated so CI without gopls still passes. It exercises the foundation
// lifecycle only: lazy spawn for a .go file, then process-group teardown.
func TestLiveGopls(t *testing.T) {
	bin, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("gopls not on PATH; skipping live integration test")
	}

	dir := t.TempDir()
	touch(t, dir, "go.mod")
	src := touch(t, dir, "main.go")

	// Resolver that returns the real gopls and never installs.
	s := NewService(dir, config.LSPConfig{Enabled: true}, liveResolver{gopls: bin})
	defer s.DisposeAll()

	ids, err := s.EnsureClients(src)
	if err != nil {
		t.Fatalf("EnsureClients(gopls): %v", err)
	}
	found := false
	for _, id := range ids {
		if id == "gopls" {
			found = true
		}
	}
	if !found {
		t.Fatalf("gopls should be running for a .go file, got %v", ids)
	}
	if got := s.Status(); len(got) == 0 {
		t.Fatalf("Status should report the running gopls")
	}
}

type liveResolver struct{ gopls string }

func (r liveResolver) Which(name string) string {
	if name == "gopls" {
		return r.gopls
	}
	return ""
}
func (r liveResolver) EnsureGopls() string { return r.gopls }
