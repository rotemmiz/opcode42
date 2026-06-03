package lsp

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// fakeServer is an in-process LSP server used to exercise the client's
// handshake + diagnostics paths without a real language server binary. It
// answers initialize with configurable capabilities and serves
// textDocument/diagnostic pull requests from a canned report.
type fakeServer struct {
	conn jsonrpc2.Conn

	// capabilities returned from initialize.
	caps map[string]any
	// pullItems is the documents report returned for textDocument/diagnostic.
	pullItems []protocol.Diagnostic
}

func (f *fakeServer) handle(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	switch req.Method() {
	case "initialize":
		return reply(ctx, map[string]any{"capabilities": f.caps}, nil)
	case "textDocument/diagnostic":
		return reply(ctx, map[string]any{"items": f.pullItems}, nil)
	case "workspace/diagnostic":
		return reply(ctx, map[string]any{"items": []any{}}, nil)
	}
	// initialized / didOpen / didChange / didChangeConfiguration are notifications.
	if _, ok := req.(*jsonrpc2.Call); ok {
		return reply(ctx, nil, nil)
	}
	return nil
}

// publishDiagnostics pushes a textDocument/publishDiagnostics notification for
// file with the given diagnostics.
func (f *fakeServer) publishDiagnostics(t *testing.T, file string, diags []protocol.Diagnostic) {
	t.Helper()
	err := f.conn.Notify(context.Background(), "textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{
		URI:         uri.File(file),
		Diagnostics: diags,
	})
	if err != nil {
		t.Fatalf("publishDiagnostics: %v", err)
	}
}

// newClientPair wires a Client to an in-process fakeServer over net.Pipe and
// runs the handshake. It returns both ends; the caller defers cleanup.
func newClientPair(t *testing.T, dir string, caps map[string]any, pull []protocol.Diagnostic) (*Client, *fakeServer) {
	t.Helper()
	clientEnd, serverEnd := net.Pipe()

	srv := &fakeServer{caps: caps, pullItems: pull}
	srv.conn = jsonrpc2.NewConn(jsonrpc2.NewStream(serverEnd))
	srv.conn.Go(context.Background(), srv.handle)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := newClient(ctx, "fake", dir, dir, clientEnd, clientEnd, nil)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	t.Cleanup(func() {
		c.Shutdown()
		_ = srv.conn.Close()
	})
	return c, srv
}

func diag(msg string, line uint32) protocol.Diagnostic {
	return protocol.Diagnostic{
		Range: protocol.Range{
			Start: protocol.Position{Line: line, Character: 0},
			End:   protocol.Position{Line: line, Character: 1},
		},
		Severity: protocol.DiagnosticSeverityError,
		Message:  msg,
		Source:   "fake",
	}
}

// TestClientPushDiagnostics verifies a publishDiagnostics push is recorded and
// surfaced via Diagnostics(), keyed by the file's absolute path.
func TestClientPushDiagnostics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, srv := newClientPair(t, dir, map[string]any{}, nil)
	srv.publishDiagnostics(t, file, []protocol.Diagnostic{diag("boom", 0)})

	waitFor(t, time.Second, func() bool {
		return len(c.Diagnostics()[filepath.Clean(file)]) == 1
	})
}

// TestClientPullDiagnostics verifies that with a static diagnosticProvider, a
// document pull populates diagnostics and WaitForDiagnostics returns once the
// file has them.
func TestClientPullDiagnostics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	caps := map[string]any{"diagnosticProvider": map[string]any{}}
	c, _ := newClientPair(t, dir, caps, []protocol.Diagnostic{diag("pull-err", 1)})

	if _, err := c.Open(context.Background(), file); err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.WaitForDiagnostics(ctx, file, DiagModeDocument)

	got := c.Diagnostics()[filepath.Clean(file)]
	if len(got) != 1 || got[0].Message != "pull-err" {
		t.Fatalf("expected one pull diagnostic, got %v", got)
	}
}

// TestClientDedupesDiagnostics verifies push+pull duplicates collapse by
// {code,severity,message,source,range}.
func TestClientDedupesDiagnostics(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	same := diag("dup", 2)
	caps := map[string]any{"diagnosticProvider": map[string]any{}}
	c, srv := newClientPair(t, dir, caps, []protocol.Diagnostic{same})

	// Push the same diagnostic, then pull it too.
	srv.publishDiagnostics(t, file, []protocol.Diagnostic{same})
	waitFor(t, time.Second, func() bool {
		return len(c.Diagnostics()[filepath.Clean(file)]) >= 1
	})
	if _, err := c.Open(context.Background(), file); err != nil {
		t.Fatalf("Open: %v", err)
	}
	c.requestDocumentDiagnostics(context.Background(), filepath.Clean(file))

	got := c.Diagnostics()[filepath.Clean(file)]
	if len(got) != 1 {
		t.Fatalf("push+pull duplicate should dedupe to one, got %d: %v", len(got), got)
	}
}

// TestDedupeDiagnostics covers the dedupe helper directly.
func TestDedupeDiagnostics(t *testing.T) {
	a := diag("x", 0)
	b := diag("y", 0)
	out := dedupeDiagnostics([]protocol.Diagnostic{a, a, b})
	if len(out) != 2 {
		t.Fatalf("expected 2 after dedupe, got %d", len(out))
	}
}

// TestConfigurationValue covers the dotted-section resolution.
func TestConfigurationValue(t *testing.T) {
	settings := map[string]any{"go": map[string]any{"buildFlags": []any{"-tags=x"}}}
	if v := configurationValue(settings, "go.buildFlags"); v == nil {
		t.Fatalf("expected nested value, got nil")
	}
	if v := configurationValue(settings, "missing.key"); v != nil {
		t.Fatalf("expected nil for missing section, got %v", v)
	}
	raw, _ := json.Marshal(configurationValue(settings, ""))
	if len(raw) == 0 {
		t.Fatalf("empty section should return whole settings")
	}
}

// waitFor polls cond until true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
