package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/engine/permission"
	"github.com/rotemmiz/forge/internal/engine/processor"
	"github.com/rotemmiz/forge/internal/engine/registry"
)

// remoteToolServer builds an in-process MCP server exposing one "ping" tool and
// (optionally) announcing tool-list-changed capability so the watcher fires.
func remoteToolServer(toolListChanged bool) *server.MCPServer {
	opts := []server.ServerOption{}
	if toolListChanged {
		opts = append(opts, server.WithToolCapabilities(true))
	}
	s := server.NewMCPServer("remote", "1.0.0", opts...)
	s.AddTool(mcp.NewTool("ping", mcp.WithDescription("pong")),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("pong"), nil
		})
	return s
}

// TestRemoteDial_StreamableHTTP connects to a real StreamableHTTP MCP server and
// surfaces its tools, proving the remote transport (C1).
func TestRemoteDial_StreamableHTTP(t *testing.T) {
	ts := server.NewTestStreamableHTTPServer(remoteToolServer(false))
	defer ts.Close()

	m := NewManager(map[string]Server{"r": {Type: "remote", URL: ts.URL}})
	status := m.Status(context.Background())
	if status["r"].Status != "connected" {
		t.Fatalf("remote status = %+v", status["r"])
	}
	defs := m.Tools(context.Background())
	if len(defs) != 1 || defs[0].Name != "r_ping" {
		t.Fatalf("remote tools = %+v", defs)
	}
	m.Close()
}

// TestRemoteDial_SSEFallback proves the SSE fallback (C1): the manager connects
// to a URL whose StreamableHTTP handshake fails (the endpoint returns 405 on the
// StreamableHTTP POST) but whose SSE handshake succeeds, so it falls back to SSE.
//
// The SSE server and the StreamableHTTP-rejecting handler share one origin (one
// httptest.Server) so the SSE endpoint URL the server advertises matches the
// connection origin (mark3labs rejects an origin mismatch).
func TestRemoteDial_SSEFallback(t *testing.T) {
	sseSrv := server.NewSSEServer(remoteToolServer(false),
		server.WithSSEEndpoint("/sse"), server.WithMessageEndpoint("/message"))

	mux := http.NewServeMux()
	// SSE handshake + message posting.
	mux.Handle("/sse", sseSrv.SSEHandler())
	mux.Handle("/message", sseSrv.MessageHandler())
	// The StreamableHTTP attempt POSTs to the configured URL ("/mcp"); reject it
	// so the manager falls through to SSE.
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	front := httptest.NewServer(mux)
	defer front.Close()

	// The SSE server advertises a relative message endpoint (no baseURL set), so
	// the client resolves it against the SSE URL's origin — same host as /sse.
	m := NewManager(map[string]Server{"r": {Type: "remote", URL: front.URL + "/sse", Timeout: 5000}})
	status := m.Status(context.Background())
	if status["r"].Status != "connected" {
		t.Fatalf("SSE-fallback status = %+v", status["r"])
	}
	if defs := m.Tools(context.Background()); len(defs) != 1 || defs[0].Name != "r_ping" {
		t.Fatalf("SSE-fallback tools = %+v", defs)
	}
	m.Close()
}

// TestRemoteDial_AllTransportsFail surfaces a failed status when neither
// transport connects (no MCP server listening).
func TestRemoteDial_AllTransportsFail(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer ts.Close()

	m := NewManager(map[string]Server{"r": {Type: "remote", URL: ts.URL, Timeout: 1500}})
	status := m.Status(context.Background())
	if status["r"].Status != "failed" {
		t.Fatalf("unreachable remote should be failed, got %+v", status["r"])
	}
	m.Close()
}

// recordingBus captures published events for the watcher test.
type recordingBus struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	typ   string
	props any
}

func (b *recordingBus) Publish(typ string, props any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, recordedEvent{typ: typ, props: props})
}

func (b *recordingBus) count(typ string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.events {
		if e.typ == typ {
			n++
		}
	}
	return n
}

// TestMCPToolCall_EmitsPermissionAsked proves C3 (and the plan-12 assertion):
// dispatching an MCP tool through the executor routes through the permission ask
// path, which publishes permission.asked on the bus — exactly as built-in tools
// do (opencode session/tools.ts:135 keys the ask on the flattened tool name).
func TestMCPToolCall_EmitsPermissionAsked(t *testing.T) {
	instBus := bus.NewInstanceBus("/tmp/x", nil)
	events, unsub := instBus.Subscribe()
	defer unsub()

	pm := permission.NewManager(instBus)
	m := NewManager(map[string]Server{"echo": {Type: "local", Command: []string{"x"}}})
	m.dial = dialInProcess(inProcessServer())

	ex := &registry.Executor{Registry: registry.New(), Asker: pm, MCP: m, SessionID: "s"}

	// The ask blocks until a reply, so run the call in a goroutine and grant once.
	done := make(chan error, 1)
	go func() {
		_, err := ex.Execute(context.Background(),
			processor.ToolCall{Name: "echo_ping", SessionID: "s", Input: map[string]any{}})
		done <- err
	}()

	var asked *permission.Request
	deadline := time.After(3 * time.Second)
	for asked == nil {
		select {
		case e := <-events:
			if e.Type == "permission.asked" {
				if req, ok := e.Properties.(permission.Request); ok {
					r := req
					asked = &r
				}
			}
		case <-deadline:
			t.Fatal("no permission.asked emitted for the MCP tool call")
		}
	}
	// The ask is keyed on the flattened MCP tool name with patterns "*".
	if asked.Permission != "echo_ping" {
		t.Fatalf("permission.asked key = %q, want echo_ping", asked.Permission)
	}
	if len(asked.Patterns) != 1 || asked.Patterns[0] != "*" {
		t.Fatalf("permission.asked patterns = %v, want [*]", asked.Patterns)
	}

	if err := pm.Reply(asked.ID, permission.ReplyOnce); err != nil {
		t.Fatalf("reply: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("granted MCP tool call should succeed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("MCP tool call did not complete after grant")
	}
	m.Close()
}

// TestMCPToolCall_PermissionDenied proves a denied MCP tool call surfaces the
// DeniedError (the model sees a tool error), not a silent bypass.
func TestMCPToolCall_PermissionDenied(t *testing.T) {
	pm := permission.NewManager(bus.NewInstanceBus("/tmp/x", nil))
	m := NewManager(map[string]Server{"echo": {Type: "local", Command: []string{"x"}}})
	m.dial = dialInProcess(inProcessServer())
	ex := &registry.Executor{
		Registry: registry.New(), Asker: pm, MCP: m, SessionID: "s",
		Rulesets: []permission.Ruleset{{{Permission: "echo_ping", Pattern: "*", Action: permission.ActionDeny}}},
	}
	_, err := ex.Execute(context.Background(),
		processor.ToolCall{Name: "echo_ping", SessionID: "s", Input: map[string]any{}})
	var denied *permission.DeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("denied MCP tool call should return DeniedError, got %v", err)
	}
	m.Close()
}

// watchConn is a conn stub that captures the tools-changed handler the manager
// registers (via OnNotification) and lets the test fire it, plus swaps the tool
// list returned by a subsequent ListTools. It exercises C2's watch/refresh path
// deterministically (the in-process transport doesn't deliver server-side
// list-changed notifications without sampling/elicitation handlers).
type watchConn struct {
	mu      sync.Mutex
	tools   []mcp.Tool
	handler func(mcp.JSONRPCNotification)
}

func (c *watchConn) Start(context.Context) error { return nil }
func (c *watchConn) Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return &mcp.InitializeResult{}, nil
}
func (c *watchConn) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &mcp.ListToolsResult{Tools: append([]mcp.Tool(nil), c.tools...)}, nil
}
func (c *watchConn) CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText("ok"), nil
}
func (c *watchConn) OnNotification(h func(mcp.JSONRPCNotification)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = h
}
func (c *watchConn) Close() error { return nil }

func (c *watchConn) setTools(tools []mcp.Tool) {
	c.mu.Lock()
	c.tools = tools
	h := c.handler
	c.mu.Unlock()
	if h != nil {
		h(mcp.JSONRPCNotification{Notification: mcp.Notification{Method: mcp.MethodNotificationToolsListChanged}})
	}
}

// TestToolsChanged_PublishesEvent proves C2: when a connected server's tool list
// changes, the manager re-lists and emits mcp.tools.changed on the bus, and the
// new tool becomes visible in the cached defs.
func TestToolsChanged_PublishesEvent(t *testing.T) {
	wc := &watchConn{tools: []mcp.Tool{{Name: "ping"}}}
	bus := &recordingBus{}
	m := NewManager(map[string]Server{"r": {Type: "local", Command: []string{"x"}}}).WithBus(bus)
	m.dial = func(context.Context, Server) (conn, bool, error) { return wc, false, nil }

	if status := m.Status(context.Background()); status["r"].Status != "connected" {
		t.Fatalf("status = %+v", status["r"])
	}

	// A non-tools notification must NOT trigger a refresh/publish.
	wc.handler(mcp.JSONRPCNotification{Notification: mcp.Notification{Method: "notifications/message"}})
	if bus.count("mcp.tools.changed") != 0 {
		t.Fatal("unrelated notification should not emit mcp.tools.changed")
	}

	// The server's tool list changes → list-changed notification fires.
	wc.setTools([]mcp.Tool{{Name: "ping"}, {Name: "pong"}})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && bus.count("mcp.tools.changed") == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if bus.count("mcp.tools.changed") == 0 {
		t.Fatal("expected mcp.tools.changed after the server's tool list changed")
	}

	names := map[string]bool{}
	for _, d := range m.Tools(context.Background()) {
		names[d.Name] = true
	}
	if !names["r_ping"] || !names["r_pong"] {
		t.Fatalf("refreshed tools missing new entry: %v", names)
	}
	m.Close()
}
