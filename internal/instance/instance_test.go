package instance

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rotemmiz/forge/internal/bus"
)

func TestGetIsCachedAndPerDirectory(t *testing.T) {
	m := NewManager(bus.NewGlobal())
	a1 := m.Get("/dir/a")
	a2 := m.Get("/dir/a")
	b := m.Get("/dir/b")
	if a1 != a2 {
		t.Error("Get must return the cached instance for the same directory")
	}
	if a1 == b {
		t.Error("different directories must get different instances")
	}
	if a1.Bus == nil || a1.Pty == nil {
		t.Error("instance Context must have a Bus and Pty")
	}
}

func TestDisposeAllEmitsDisposed(t *testing.T) {
	m := NewManager(bus.NewGlobal())
	c := m.Get("/dir")
	events, unsub := c.Bus.Subscribe()
	defer unsub()

	m.DisposeAll()

	select {
	case e := <-events:
		if e.Type != bus.EventInstanceDisposed {
			t.Errorf("got %q, want %q", e.Type, bus.EventInstanceDisposed)
		}
	case <-time.After(time.Second):
		t.Fatal("DisposeAll did not emit server.instance.disposed")
	}

	// The cache is cleared: a subsequent Get builds a fresh instance.
	if m.Get("/dir") == c {
		t.Error("DisposeAll should clear the cache")
	}
}

// TestMCPBusSSEEnvelope proves the LSP/MCP services' events reach SSE
// subscribers through the instance bus with opencode's {id,type,properties}
// envelope (M3-6 SSE wiring). mcpBus is the adapter both lsp.WithBus and
// mcp.WithBus receive; the LSP service emits lsp.updated with empty properties
// (lsp.ts:20) and the MCP manager emits mcp.tools.changed with {server}
// (mcp/index.ts:51-55).
func TestMCPBusSSEEnvelope(t *testing.T) {
	instBus := bus.NewInstanceBus("/dir", nil)
	events, unsub := instBus.Subscribe()
	defer unsub()
	adapter := mcpBus{instBus}

	// lsp.updated: empty properties marshal to {} (never null), id is a fresh evt_.
	adapter.Publish("lsp.updated", map[string]any{})
	got := receive(t, events)
	if got.Type != "lsp.updated" {
		t.Errorf("type = %q, want lsp.updated", got.Type)
	}
	if got.ID == "" {
		t.Error("event must carry a non-empty id")
	}
	if props := marshalProps(t, got); props != "{}" {
		t.Errorf("lsp.updated properties = %s, want {}", props)
	}

	// mcp.tools.changed: properties carry the server name.
	adapter.Publish("mcp.tools.changed", map[string]any{"server": "fs"})
	got = receive(t, events)
	if got.Type != "mcp.tools.changed" {
		t.Errorf("type = %q, want mcp.tools.changed", got.Type)
	}
	if props := marshalProps(t, got); props != `{"server":"fs"}` {
		t.Errorf("mcp.tools.changed properties = %s, want {\"server\":\"fs\"}", props)
	}
}

func receive(t *testing.T, events <-chan bus.Event) bus.Event {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(time.Second):
		t.Fatal("expected an event on the instance bus")
		return bus.Event{}
	}
}

func marshalProps(t *testing.T, e bus.Event) string {
	t.Helper()
	b, err := json.Marshal(e.Properties)
	if err != nil {
		t.Fatalf("marshal properties: %v", err)
	}
	return string(b)
}
