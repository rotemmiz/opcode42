package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/session"
	"github.com/rotemmiz/opcode42/internal/storage"
)

// TestSSEStreamClosesOnBaseCancel proves graceful shutdown unblocks a live SSE
// stream: cancelling BaseCtx must end the /event response so http.Server can
// drain (plan 01 §9).
func TestSSEStreamClosesOnBaseCancel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), ".config"))
	db, err := storage.Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	g := bus.NewGlobal()
	base, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	h, err := New(Options{
		Version:   "0.0.1",
		Cwd:       t.TempDir(),
		Sessions:  session.NewStore(db),
		Instances: instance.NewManager(g),
		Global:    g,
		BaseCtx:   base,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/event", nil)
	req.Header.Set("x-opencode-directory", t.TempDir())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read the first server.connected event so we know the stream is live.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data:") {
			break
		}
	}

	// Begin shutdown; the stream must end (scanner stops) promptly.
	cancelBase()

	done := make(chan struct{})
	go func() {
		for scanner.Scan() {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE stream did not close after BaseCtx was cancelled")
	}
}
