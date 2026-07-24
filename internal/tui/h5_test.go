package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// H5 tests — sidebar + footer LSP/MCP chrome (plan 08f G.5/G.6), matching
// opencode's routes/session/footer.tsx:69-85 counts and the sidebar/lsp.tsx +
// sidebar/mcp.tsx status lists.

// TestH5_Sidebar_ShowsLSPAndMCPFromSeededState pins the sidebar sections
// rendering from m.lspServers/m.mcpServers — not the old fake "MCP-as-LSP"
// count.
func TestH5_Sidebar_ShowsLSPAndMCPFromSeededState(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen, m.width, m.height = ScreenSession, 120, 30
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}
	m.lspServers = []lspItem{
		{ID: "typescript", Name: "typescript", Root: "", Status: "connected"},
		{ID: "gopls", Name: "gopls", Root: "internal", Status: "error"},
	}
	m.mcpServers = []mcpItem{
		{Name: "github", Status: "connected"},
		{Name: "broken", Status: "failed", Error: "boom"},
	}

	out := stripANSI(m.sidebarView())

	if !strings.Contains(out, "MCP") {
		t.Fatal("sidebar should render an MCP section header")
	}
	if !strings.Contains(out, "github") || !strings.Contains(out, "Connected") {
		t.Errorf("sidebar MCP section should list the connected server, got:\n%s", out)
	}
	if !strings.Contains(out, "broken") {
		t.Errorf("sidebar MCP section should list the failed server, got:\n%s", out)
	}
	if !strings.Contains(out, "LSP") {
		t.Fatal("sidebar should render an LSP section header")
	}
	if !strings.Contains(out, "typescript") {
		t.Errorf("sidebar LSP section should list the connected client, got:\n%s", out)
	}
	if !strings.Contains(out, "gopls") || !strings.Contains(out, "internal") {
		t.Errorf("sidebar LSP section should list the errored client with its root, got:\n%s", out)
	}
}

// TestH5_Sidebar_LSPPlaceholderWhenEmpty pins the "no clients yet" placeholder
// (opencode sidebar/lsp.tsx:25) and confirms the MCP section is omitted
// entirely when no servers are configured (mirrors sidebar/mcp.tsx's
// `<Show when={list().length > 0}>`).
func TestH5_Sidebar_LSPPlaceholderWhenEmpty(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen, m.width, m.height = ScreenSession, 120, 30
	m.store.sessions = []Session{{ID: "ses_1", Title: "S"}}

	out := stripANSI(m.sidebarView())

	if !strings.Contains(out, "LSPs will activate as files are read") {
		t.Errorf("empty LSP list should show the lazy-activation placeholder, got:\n%s", out)
	}
	if strings.Contains(out, "MCP") {
		t.Errorf("MCP section should be omitted when no servers are configured, got:\n%s", out)
	}
}

// TestH5_StatusBar_ShowsLSPAndMCPCounts pins the footer counts
// (footer.tsx:69-85): "• N LSP" always shown, "⊙ N MCP" shown only when at
// least one server is connected.
func TestH5_StatusBar_ShowsLSPAndMCPCounts(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.lspServers = []lspItem{
		{ID: "typescript", Status: "connected"},
		{ID: "gopls", Status: "connected"},
	}
	m.mcpServers = []mcpItem{
		{Name: "github", Status: "connected"},
		{Name: "broken", Status: "failed"},
	}

	bar := stripANSI(m.statusBarView(160))
	if !strings.Contains(bar, "2 LSP") {
		t.Errorf("status bar should show 2 LSP, got:\n%s", bar)
	}
	if !strings.Contains(bar, "1 MCP") {
		t.Errorf("status bar should show 1 MCP (connected count only), got:\n%s", bar)
	}
}

// TestH5_StatusBar_HidesMCPWhenNoneConnected pins the `<Show when={mcp()}>`
// gate: the MCP chip is omitted when zero servers are connected, even if
// some are configured (e.g. all failed/disabled).
func TestH5_StatusBar_HidesMCPWhenNoneConnected(t *testing.T) {
	m := New(Config{URL: "http://x", Provider: "p", Model: "m", SessionID: "ses_1"})
	m.mcpServers = []mcpItem{{Name: "broken", Status: "failed"}}

	bar := stripANSI(m.statusBarView(160))
	if strings.Contains(bar, "MCP") {
		t.Errorf("status bar should hide the MCP chip when none are connected, got:\n%s", bar)
	}
	if !strings.Contains(bar, "0 LSP") {
		t.Errorf("status bar should still show 0 LSP, got:\n%s", bar)
	}
}

// TestH5_LSPUpdated_TriggersReload pins the SSE lsp.updated → refetch wiring
// (lsp/lsp.ts:294 fires the event after a client's first successful spawn;
// Opcode42 re-fetches GET /lsp rather than reducing the payload since the
// event carries no data). collectMsgs (reconcile_test.go) runs the returned
// batch and gathers the leaf messages; a lspLoadedMsg (success or error —
// there's no live daemon here) confirms loadLSPCmd was dispatched.
func TestH5_LSPUpdated_TriggersReload(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.screen = ScreenSession
	// sseEventMsg's handler re-issues listenCmd(m.stream), so the model needs
	// a live stream for the returned batch to not panic; an empty EventStream
	// blocks listenCmd forever (no events) — collectMsgs leaves it running.
	m.stream = &opcode42client.EventStream{}
	defer m.stream.Close()

	_, cmd := step(t, m, sseEventMsg{ev: opcode42client.SSEEvent{Type: "lsp.updated"}})
	if cmd == nil {
		t.Fatal("lsp.updated should return a command batch")
	}
	if !containsLSPLoadedMsg(collectMsgs(t, cmd)) {
		t.Fatal("lsp.updated should dispatch loadLSPCmd (a lspLoadedMsg-producing command)")
	}
}

// TestH5_Bootstrap_LoadsLSPAndMCP pins the connectedMsg bootstrap batch: it
// must include the LSP and MCP status fetches so the sidebar/footer counts
// populate without opening either modal (plan 08f G.5).
func TestH5_Bootstrap_LoadsLSPAndMCP(t *testing.T) {
	m := New(Config{URL: "http://x"})
	_, cmd := step(t, m, connectedMsg{})
	if cmd == nil {
		t.Fatal("connectedMsg should return a bootstrap batch")
	}
	msgs := collectMsgs(t, cmd)
	if !containsLSPLoadedMsg(msgs) {
		t.Fatal("bootstrap batch should include loadLSPCmd")
	}
	if !containsMCPLoadedMsg(msgs) {
		t.Fatal("bootstrap batch should include loadMCPCmd")
	}
}

func containsLSPLoadedMsg(msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(lspLoadedMsg); ok {
			return true
		}
	}
	return false
}

func containsMCPLoadedMsg(msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(mcpLoadedMsg); ok {
			return true
		}
	}
	return false
}
