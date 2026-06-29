package conformance

import (
	"context"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/client"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/engine/permission"
	"github.com/rotemmiz/opcode42/internal/engine/processor"
	"github.com/rotemmiz/opcode42/internal/engine/registry"
	"github.com/rotemmiz/opcode42/internal/mcp"
)

// TestMCPToolCallEmitsPermissionAsked is the plan-12 conformance assertion for
// Track C C3: opencode routes MCP tool execution through the same permission
// ask path as built-in tools (session/tools.ts:135), publishing a
// permission.asked bus event keyed on the flattened tool name. Opcode42 must do the
// same. This drives the registry executor in-process against a stub MCP server
// (no LLM/provider needed) and asserts the event fires before the tool runs.
func TestMCPToolCallEmitsPermissionAsked(t *testing.T) {
	// Stub MCP server exposing one tool.
	srv := mcpserver.NewMCPServer("stub", "1.0.0")
	srv.AddTool(mcpsdk.NewTool("ping", mcpsdk.WithDescription("pong")),
		func(context.Context, mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return mcpsdk.NewToolResultText("pong"), nil
		})

	instBus := bus.NewInstanceBus("/tmp/conformance-mcp", nil)
	events, unsub := instBus.Subscribe()
	defer unsub()

	pm := permission.NewManager(instBus)
	m := mcp.NewManager(map[string]mcp.Server{"echo": {Type: "local", Command: []string{"x"}}})
	mcp.SetDialForTest(m, func(context.Context, mcp.Server) (*mcpgo.Client, bool, error) {
		c, err := mcpgo.NewInProcessClient(srv)
		return c, false, err
	})
	defer m.Close()

	ex := &registry.Executor{Registry: registry.New(), Asker: pm, MCP: m, SessionID: "s"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ex.Execute(context.Background(),
			processor.ToolCall{Name: "echo_ping", SessionID: "s", Input: map[string]any{}})
	}()

	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Type != "permission.asked" {
				continue
			}
			req, ok := e.Properties.(permission.Request)
			if !ok {
				t.Fatalf("permission.asked properties type = %T", e.Properties)
			}
			if req.Permission != "echo_ping" {
				t.Fatalf("permission key = %q, want echo_ping (flattened MCP tool name)", req.Permission)
			}
			// Grant so the executor goroutine can finish cleanly.
			if err := pm.Reply(req.ID, permission.ReplyOnce); err != nil {
				t.Fatalf("reply: %v", err)
			}
			<-done
			return
		case <-deadline:
			t.Fatal("MCP tool call did not emit permission.asked (C3 gate missing)")
		}
	}
}
