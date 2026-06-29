package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// sanitizeRe matches characters not allowed in a tool name; opencode replaces
// them with "_" (mcp/index.ts:115).
var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitize(s string) string { return sanitizeRe.ReplaceAllString(s, "_") }

// ToolDef is a flattened MCP tool exposed to the agent. Name is the unique key
// the model calls: sanitize(server)+"_"+sanitize(tool) (mcp/index.ts:697).
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// defaultTimeout is opencode's MCP request timeout (mcp/index.ts:37, 30s — the
// config description says 5s but the code uses 30s).
const defaultTimeout = 30 * time.Second

// Status is a server's connection status (openapi MCPStatus). Error is present
// (required) for "failed" and "needs_client_registration"; absent otherwise.
type Status struct {
	Status string `json:"status"` // connected|disabled|failed|needs_auth|needs_client_registration
	Error  string `json:"error,omitempty"`
}

// conn is the subset of mcp-go's client used here (so tests can substitute an
// in-process client).
type conn interface {
	Start(ctx context.Context) error
	Initialize(ctx context.Context, req mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
	OnNotification(handler func(mcp.JSONRPCNotification))
	Close() error
}

// Manager owns one instance's MCP clients. Connection is lazy (on first Status/
// Tools) so creating an instance stays cheap; results are cached for the
// instance's lifetime.
type Manager struct {
	servers map[string]Server
	// dial connects a server's transport. ready reports whether the returned
	// conn is already Started+Initialized (remote, which must handshake to pick a
	// transport) so dialAndList does not re-Initialize it.
	dial func(ctx context.Context, s Server) (c conn, ready bool, err error)
	// bus, when set, receives mcp.tools.changed events when a connected server's
	// tool list changes (nil in unit tests that don't assert the event).
	bus eventPublisher

	once    sync.Once
	mu      sync.Mutex
	closed  bool
	status  map[string]Status
	clients map[string]conn
	tools   map[string][]mcp.Tool // server name → its tools
	// dynamic holds servers added at runtime via Add() (mcp/index.ts State.config),
	// merged over the config-loaded `servers` for lookups.
	dynamic map[string]Server
	// pending holds in-progress OAuth flows keyed by server name, bridging the
	// separate StartAuth and FinishAuth HTTP requests (the Go analogue of
	// opencode's pendingOAuthTransports, mcp/index.ts:104).
	pending map[string]*authFlow
}

// eventPublisher is the subset of *bus.Bus the manager needs to emit
// mcp.tools.changed (kept as an interface to avoid an import cycle and to let
// tests substitute a recorder).
type eventPublisher interface {
	Publish(typ string, props any)
}

// NewManager builds a manager for the given server configs.
func NewManager(servers map[string]Server) *Manager {
	return &Manager{servers: servers, dial: dialTransport}
}

// WithBus attaches an event publisher so the manager emits mcp.tools.changed
// when a connected server notifies that its tool list changed. Returns the
// manager for chaining at construction time.
func (m *Manager) WithBus(b eventPublisher) *Manager {
	m.bus = b
	return m
}

// SetDialForTest substitutes the transport dialer with one returning an mcp-go
// client (e.g. an in-process server), so tests outside this package — notably
// the plan-12 conformance assertion — can exercise the manager without spawning
// a real subprocess. ready reports whether the client is already
// Started+Initialized. Test-only; not used in production paths.
func SetDialForTest(m *Manager, dial func(ctx context.Context, s Server) (c *mcpgo.Client, ready bool, err error)) {
	m.dial = func(ctx context.Context, s Server) (conn, bool, error) {
		c, ready, err := dial(ctx, s)
		if err != nil {
			return nil, false, err
		}
		return c, ready, nil
	}
}

// Status connects (once) and returns each server's status.
func (m *Manager) Status(ctx context.Context) map[string]Status {
	m.connect(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Status, len(m.status))
	for k, v := range m.status {
		out[k] = v
	}
	return out
}

// connect dials every enabled server once, caching status and tool lists. It
// dials WITHOUT holding m.mu (dialing spawns processes / does I/O), then
// publishes the results under m.mu — so a concurrent Close (which also takes
// m.mu) can't race the map writes. If Close ran during the dial, the freshly
// dialed clients are closed and nothing is published.
func (m *Manager) connect(ctx context.Context) {
	m.once.Do(func() {
		m.mu.Lock()
		closed := m.closed
		servers := m.servers
		m.mu.Unlock()
		if closed {
			return
		}

		status := map[string]Status{}
		clients := map[string]conn{}
		tools := map[string][]mcp.Tool{}
		for name, s := range servers {
			if !s.enabled() {
				status[name] = Status{Status: "disabled"}
				continue
			}
			c, tl, err := m.dialAndList(ctx, name, s)
			if err != nil {
				status[name] = m.dialErrorStatus(ctx, name, s, err)
				continue
			}
			clients[name] = c
			tools[name] = tl
			status[name] = Status{Status: "connected"}
		}

		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			for _, c := range clients {
				_ = c.Close()
			}
			return
		}
		m.status, m.clients, m.tools = status, clients, tools
		m.mu.Unlock()
	})
}

// errString returns a non-empty error message (the failed status requires one).
func errString(err error) string {
	if s := err.Error(); s != "" {
		return s
	}
	return "connection failed"
}

// dialErrorStatus classifies a failed dial into a status. A remote OAuth server
// whose connect failed because authentication is required reports needs_auth or
// needs_client_registration (mcp/index.ts:360-414); everything else is "failed".
// The metadata-discovery / DCR probe is timeout-bounded (the server's timeout, or
// the default) so a slow auth server can't hang a GET /mcp indefinitely.
func (m *Manager) dialErrorStatus(ctx context.Context, name string, s Server, err error) Status {
	if s.Type == "remote" && !s.oauthDisabled() {
		timeout := defaultTimeout
		if s.Timeout > 0 {
			timeout = time.Duration(s.Timeout) * time.Millisecond
		}
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if st, ok := authStatusFromConnectError(probeCtx, name, s, err); ok {
			return st
		}
	}
	return Status{Status: "failed", Error: errString(err)}
}

// probeInitialize runs the MCP Start+Initialize handshake against a freshly-built
// client. It is shared by dialAndList (local/stdio) and the OAuth probe.
func probeInitialize(ctx context.Context, c conn) error {
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "opcode42", Version: "0.0.1"}
	_, err := c.Initialize(ctx, initReq)
	return err
}

// dialAndList connects, initializes, and lists a server's tools. When dial
// already handshook the connection (ready), the Start/Initialize here is
// skipped so a remote transport isn't initialized twice (some servers reject a
// second `initialize`).
func (m *Manager) dialAndList(ctx context.Context, name string, s Server) (conn, []mcp.Tool, error) {
	timeout := defaultTimeout
	if s.Timeout > 0 {
		timeout = time.Duration(s.Timeout) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c, ready, err := m.dial(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	if !ready {
		// Note: the stdio constructor already spawns the subprocess (under its own
		// background ctx), so this Start is idempotent and the timeout ctx bounds
		// Initialize/ListTools rather than the spawn itself.
		if err := c.Start(ctx); err != nil {
			_ = c.Close()
			return nil, nil, err
		}
		if err := probeInitialize(ctx, c); err != nil {
			_ = c.Close()
			return nil, nil, err
		}
	}
	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = c.Close()
		return nil, nil, err
	}
	m.watch(name, c)
	return c, res.Tools, nil
}

// watch registers a notifications/tools/list_changed handler on a connected
// client (opencode mcp/index.ts:509-521). On notification it re-fetches the
// server's tools, updates the cache, and publishes mcp.tools.changed on the
// instance bus so the agent loop re-queries tools before its next LLM call.
func (m *Manager) watch(name string, c conn) {
	c.OnNotification(func(n mcp.JSONRPCNotification) {
		if n.Method != mcp.MethodNotificationToolsListChanged {
			return
		}
		m.refreshTools(name, c)
	})
}

// refreshTools re-lists a server's tools and, if the connection is still the one
// cached for name (not closed/replaced), updates the cache and emits
// mcp.tools.changed with {server: name} — matching opencode's payload
// (mcp/index.ts:519). The agent loop re-queries tools on the event.
func (m *Manager) refreshTools(name string, c conn) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return
	}
	m.mu.Lock()
	if m.closed || m.clients[name] != c {
		m.mu.Unlock()
		return
	}
	m.tools[name] = res.Tools
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish("mcp.tools.changed", map[string]any{"server": name})
	}
}

// flatTool is one MCP tool with its flattened (unique) name and origin server.
type flatTool struct {
	name   string // sanitize(server)_sanitize(tool)
	server string
	tool   mcp.Tool
}

// flatTools returns the connected tools flattened to unique names, sorted by
// name with first-wins dedupe so the LLM listing (Tools) and dispatch (CallTool)
// always agree on which tool a colliding name resolves to. Caller holds m.mu.
func (m *Manager) flatTools() []flatTool {
	var all []flatTool
	for server, tools := range m.tools {
		for _, t := range tools {
			all = append(all, flatTool{name: sanitize(server) + "_" + sanitize(t.Name), server: server, tool: t})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].name != all[j].name {
			return all[i].name < all[j].name
		}
		return all[i].server < all[j].server
	})
	seen := map[string]bool{}
	out := all[:0]
	for _, ft := range all {
		if !seen[ft.name] {
			seen[ft.name] = true
			out = append(out, ft)
		}
	}
	return out
}

// Tools connects (once) and returns every connected server's tools as flattened
// defs (unique sanitized names) for the LLM tool list.
func (m *Manager) Tools(ctx context.Context) []ToolDef {
	m.connect(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	fts := m.flatTools()
	out := make([]ToolDef, 0, len(fts))
	for _, ft := range fts {
		out = append(out, ToolDef{
			Name:        ft.name,
			Description: ft.tool.Description,
			InputSchema: schemaToMap(ft.tool),
		})
	}
	return out
}

// HasTool reports whether name resolves to a connected MCP tool. It connects
// (once) so the executor's permission gate can decide before dispatch.
func (m *Manager) HasTool(ctx context.Context, name string) bool {
	m.connect(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ft := range m.flatTools() {
		if ft.name == name {
			return true
		}
	}
	return false
}

// CallTool dispatches a flattened tool name to its server. found is false when
// no MCP tool matches (so the caller can fall through to "unknown tool"). A tool
// that reports an error returns its error text as output (matching opencode,
// which surfaces the text to the model rather than failing the call).
func (m *Manager) CallTool(ctx context.Context, name string, args map[string]any) (output string, found bool, err error) {
	m.connect(ctx)
	m.mu.Lock()
	var client conn
	var toolName string
	for _, ft := range m.flatTools() {
		if ft.name == name {
			client, toolName = m.clients[ft.server], ft.tool.Name
			break
		}
	}
	m.mu.Unlock()
	if client == nil {
		return "", false, nil
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args
	res, err := client.CallTool(ctx, req)
	if err != nil {
		return "", true, err
	}
	text := resultText(res)
	if text == "" && res.IsError {
		text = "tool returned an error"
	}
	return text, true, nil
}

// resultText joins the text content of an MCP tool result.
func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// schemaToMap renders an MCP tool's input schema as a JSON-Schema object,
// forcing type:object + additionalProperties:false (opencode convertMcpTool).
func schemaToMap(t mcp.Tool) map[string]any {
	var raw []byte
	if len(t.RawInputSchema) > 0 {
		raw = t.RawInputSchema
	} else if b, err := json.Marshal(t.InputSchema); err == nil {
		raw = b
	}
	m := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	m["type"] = "object"
	if _, ok := m["properties"]; !ok {
		m["properties"] = map[string]any{}
	}
	m["additionalProperties"] = false
	return m
}

// Close shuts down all connected clients and marks the manager closed so an
// in-flight connect won't publish (or leak) new clients.
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	clients := m.clients
	m.clients = nil
	m.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}

// dialTransport is the default transport selector: local servers spawn a
// subprocess (stdio); remote servers connect over HTTP (with OAuth wired unless
// disabled). An OAuth-required connect failure is propagated so dialErrorStatus
// can classify it as needs_auth / needs_client_registration.
func dialTransport(ctx context.Context, s Server) (conn, bool, error) {
	switch s.Type {
	case "local":
		c, err := stdioDial(s)
		return c, false, err
	case "remote":
		return remoteDial(ctx, s)
	default:
		return nil, false, fmt.Errorf("unknown MCP server type %q", s.Type)
	}
}

// stdioDial spawns a local MCP server subprocess. The returned client is not yet
// Started/Initialized (the caller's dialAndList does that).
func stdioDial(s Server) (conn, error) {
	if len(s.Command) == 0 {
		return nil, fmt.Errorf("local MCP server has no command")
	}
	env := make([]string, 0, len(s.Environment))
	for k, v := range s.Environment {
		env = append(env, k+"="+v)
	}
	c, err := mcpgo.NewStdioMCPClient(s.Command[0], env, s.Command[1:]...)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// remoteDial connects to a remote MCP server. It tries the StreamableHTTP
// transport first, then falls back to SSE, mirroring opencode's transport order
// (mcp/index.ts:339-413). The chosen transport is the one whose Start+Initialize
// handshake succeeds; it returns ready=true so dialAndList does NOT re-run the
// handshake (some servers reject a second `initialize`).
//
// OAuth: unless `oauth: false`, each transport is wired with a persistent token
// store + the resolved redirect URI, so a stored token authenticates the connect
// transparently. If a transport reports OAuth is required, that error is returned
// immediately and the fallback loop breaks (matching opencode, which stops trying
// transports on an auth error); dialErrorStatus then maps it to needs_auth /
// needs_client_registration.
func remoteDial(ctx context.Context, s Server) (conn, bool, error) {
	if s.URL == "" {
		return nil, false, fmt.Errorf("remote MCP server has no url")
	}
	// Attach OAuth only when a token is already stored: mcp-go's OAuth transport
	// pre-empts with an "authorization required" error whenever the token store is
	// empty (oauth.go:250), which would wrongly mark servers that don't require
	// auth as needs_auth. So we dial plain when there's no token and rely on the
	// server's real 401 (base AuthorizationRequiredError) to drive the auth flow —
	// matching opencode, which only treats a 401 as needs_auth (mcp/index.ts:360).
	oauthOn := !s.oauthDisabled() && hasStoredTokens(s.Name)
	var cfg transport.OAuthConfig
	if oauthOn {
		cfg = oauthConfig(s.Name, s)
	}
	var lastErr error
	for _, build := range []func() (conn, error){
		func() (conn, error) {
			opts := []transport.StreamableHTTPCOption{transport.WithHTTPHeaders(s.Headers)}
			if oauthOn {
				opts = append(opts, transport.WithHTTPOAuth(cfg))
			}
			return mcpgo.NewStreamableHttpClient(s.URL, opts...)
		},
		func() (conn, error) {
			opts := []transport.ClientOption{transport.WithHeaders(s.Headers)}
			if oauthOn {
				opts = append(opts, transport.WithOAuth(cfg))
			}
			return mcpgo.NewSSEMCPClient(s.URL, opts...)
		},
	} {
		c, err := build()
		if err != nil {
			lastErr = err
			continue
		}
		if err := probeRemote(ctx, s, c); err != nil {
			_ = c.Close()
			// Stop on an auth error (401): trying the next transport would just hit
			// the same 401, and opencode breaks the loop here (mcp/index.ts:413).
			if isAuthRequired(err) {
				return nil, false, err
			}
			lastErr = err
			continue
		}
		return c, true, nil // already Started+Initialized by probeRemote
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no remote transport succeeded")
	}
	return nil, false, lastErr
}

// isAuthRequired reports whether err is a 401-driven authorization error, whether
// from a plain transport (AuthorizationRequiredError) or an OAuth-enabled one
// (OAuthAuthorizationRequiredError). Both signal the server requires auth.
func isAuthRequired(err error) bool {
	return mcpgo.IsOAuthAuthorizationRequiredError(err) || mcpgo.IsAuthorizationRequiredError(err)
}

// probeRemote runs the Start+Initialize handshake against a candidate transport
// so remoteDial can pick the first that connects. Start uses a long-lived
// context (the SSE transport binds its event stream to the Start context, so it
// must outlive the probe — only Initialize/ListTools are bounded by the server's
// timeout). The timeout defaults to defaultTimeout.
func probeRemote(ctx context.Context, s Server, c conn) error {
	timeout := defaultTimeout
	if s.Timeout > 0 {
		timeout = time.Duration(s.Timeout) * time.Millisecond
	}
	if err := c.Start(context.WithoutCancel(ctx)); err != nil {
		return err
	}
	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "opcode42", Version: "0.0.1"}
	if _, err := c.Initialize(initCtx, initReq); err != nil {
		return err
	}
	return nil
}
