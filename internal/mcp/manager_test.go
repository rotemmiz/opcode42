package mcp

import (
	"context"
	"errors"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/rotemmiz/forge/internal/engine/processor"
	"github.com/rotemmiz/forge/internal/engine/registry"
)

// inProcessServer builds an in-process MCP server exposing one tool.
func inProcessServer() *server.MCPServer {
	s := server.NewMCPServer("test", "1.0.0")
	s.AddTool(
		mcp.NewTool("ping", mcp.WithDescription("returns pong")),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("pong"), nil
		},
	)
	return s
}

func TestManager_ConnectsAndListsTools(t *testing.T) {
	m := NewManager(map[string]Server{
		"echo": {Type: "local", Command: []string{"unused"}},
		"off":  {Type: "local", Command: []string{"unused"}, Enabled: boolPtr(false)},
	})
	m.dial = func(_ context.Context, _ Server) (conn, error) {
		return mcpgo.NewInProcessClient(inProcessServer())
	}

	status := m.Status(context.Background())
	if status["echo"].Status != "connected" {
		t.Fatalf("echo status = %+v", status["echo"])
	}
	if status["off"].Status != "disabled" {
		t.Fatalf("off status = %+v", status["off"])
	}
	m.mu.Lock()
	tools := m.tools["echo"]
	m.mu.Unlock()
	if len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("echo tools = %+v", tools)
	}
	m.Close()
}

func TestManager_ToolsAndCallTool(t *testing.T) {
	m := NewManager(map[string]Server{"my srv": {Type: "local", Command: []string{"x"}}})
	m.dial = func(_ context.Context, _ Server) (conn, error) {
		return mcpgo.NewInProcessClient(inProcessServer())
	}
	ctx := context.Background()

	defs := m.Tools(ctx)
	if len(defs) != 1 {
		t.Fatalf("want 1 tool def, got %d: %+v", len(defs), defs)
	}
	// Name is sanitize(server)_sanitize(tool); "my srv" → "my_srv".
	if defs[0].Name != "my_srv_ping" {
		t.Fatalf("flattened name = %q, want my_srv_ping", defs[0].Name)
	}
	if defs[0].InputSchema["type"] != "object" || defs[0].InputSchema["additionalProperties"] != false {
		t.Fatalf("schema not normalized: %+v", defs[0].InputSchema)
	}

	out, found, err := m.CallTool(ctx, "my_srv_ping", map[string]any{})
	if err != nil || !found || out != "pong" {
		t.Fatalf("CallTool = %q, found=%v, err=%v", out, found, err)
	}
	if _, found, _ := m.CallTool(ctx, "no_such_tool", nil); found {
		t.Fatal("unknown tool should report found=false")
	}
	m.Close()
}

// TestExecutorDispatchesMCPTool is the end-to-end proof: the registry executor,
// on a name that isn't a built-in, dispatches to the MCP manager and returns the
// server's result. (In package mcp so the in-process dial can be injected;
// registry doesn't import mcp, so there's no cycle.)
func TestExecutorDispatchesMCPTool(t *testing.T) {
	m := NewManager(map[string]Server{"echo": {Type: "local", Command: []string{"x"}}})
	m.dial = func(_ context.Context, _ Server) (conn, error) {
		return mcpgo.NewInProcessClient(inProcessServer())
	}
	ex := &registry.Executor{Registry: registry.New(), MCP: m}
	res, err := ex.Execute(context.Background(), processor.ToolCall{Name: "echo_ping", Input: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "pong" {
		t.Fatalf("executor MCP dispatch output = %q, want pong", res.Output)
	}
	// A genuinely-unknown name still errors.
	if _, err := ex.Execute(context.Background(), processor.ToolCall{Name: "nope"}); err == nil {
		t.Fatal("unknown tool should error")
	}
	m.Close()
}

func TestManager_CallTool_ErrorTextAsOutput(t *testing.T) {
	srv := server.NewMCPServer("t", "1.0.0")
	srv.AddTool(mcp.NewTool("boom"), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultError("it broke"), nil
	})
	m := NewManager(map[string]Server{"x": {Type: "local", Command: []string{"u"}}})
	m.dial = func(_ context.Context, _ Server) (conn, error) { return mcpgo.NewInProcessClient(srv) }

	// opencode surfaces a tool's error text to the model as output, not a failure.
	out, found, err := m.CallTool(context.Background(), "x_boom", nil)
	if err != nil || !found || out != "it broke" {
		t.Fatalf("CallTool error result = %q, found=%v, err=%v", out, found, err)
	}
	m.Close()
}

func TestFlatTools_DedupesCollidingNames(t *testing.T) {
	m := NewManager(nil)
	// server "a"+tool "b_c" and server "a_b"+tool "c" both flatten to "a_b_c".
	m.tools = map[string][]mcp.Tool{
		"a":   {{Name: "b_c"}},
		"a_b": {{Name: "c"}},
	}
	m.mu.Lock()
	fts := m.flatTools()
	m.mu.Unlock()
	if len(fts) != 1 || fts[0].name != "a_b_c" {
		t.Fatalf("colliding names not deduped: %+v", fts)
	}
	if fts[0].server != "a" { // tiebreak by server: "a" < "a_b"
		t.Fatalf("dedupe tiebreak wrong: kept server %q", fts[0].server)
	}
}

func TestManager_DialFailureIsFailed(t *testing.T) {
	m := NewManager(map[string]Server{"broken": {Type: "local", Command: []string{"x"}}})
	m.dial = func(_ context.Context, _ Server) (conn, error) {
		return nil, errors.New("spawn failed")
	}
	status := m.Status(context.Background())
	if status["broken"].Status != "failed" || status["broken"].Error != "spawn failed" {
		t.Fatalf("broken status = %+v", status["broken"])
	}
}

func TestManager_CloseRacesConnect(t *testing.T) {
	// Close() must not race the lazy connect's map writes (regression for the
	// nil-map panic at shutdown). Run under -race.
	for i := 0; i < 50; i++ {
		m := NewManager(map[string]Server{"echo": {Type: "local", Command: []string{"x"}}})
		m.dial = func(_ context.Context, _ Server) (conn, error) {
			return mcpgo.NewInProcessClient(inProcessServer())
		}
		result := make(chan map[string]Status, 1)
		go func() { result <- m.Status(context.Background()) }()
		m.Close()
		if s := <-result; s == nil {
			t.Fatal("Status returned a nil map")
		}
	}
}

func TestStdioDial_RemoteUnsupported(t *testing.T) {
	if _, err := stdioDial(context.Background(), Server{Type: "remote", URL: "https://x"}); err == nil {
		t.Fatal("remote should be unsupported in this slice")
	}
}

func boolPtr(b bool) *bool { return &b }
