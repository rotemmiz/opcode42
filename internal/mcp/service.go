package mcp

import (
	"context"
	"errors"

	"github.com/mark3labs/mcp-go/mcp"
)

// ErrServerNotFound is returned by the mutating operations when a name resolves
// to no configured (or runtime-added) MCP server. The HTTP layer maps it to
// opencode's McpServerNotFoundError (HTTP 404; handlers/mcp.ts).
var ErrServerNotFound = errors.New("MCP server not found")

// ErrOAuthDisabled is returned by StartAuth/Authenticate when the named server is
// not a remote server or has OAuth opted out (`oauth: false`). The HTTP layer
// maps it to opencode's McpUnsupportedOAuthError (HTTP 400).
var ErrOAuthDisabled = errors.New("MCP server does not support OAuth")

// serverConfig returns the configuration for name, preferring a runtime-added
// (dynamic) entry over the config-loaded one. ok=false ⇒ ErrServerNotFound.
// Caller holds m.mu.
func (m *Manager) serverConfig(name string) (Server, bool) {
	if s, ok := m.dynamic[name]; ok {
		return s, true
	}
	s, ok := m.servers[name]
	return s, ok
}

// Add registers a new MCP server at runtime and connects it, returning the full
// status map (mcp/index.ts add → { status }). It overrides any prior entry of the
// same name.
func (m *Manager) Add(ctx context.Context, name string, s Server) map[string]Status {
	m.connect(ctx)
	s.Name = name
	m.mu.Lock()
	if m.dynamic == nil {
		m.dynamic = map[string]Server{}
	}
	m.dynamic[name] = s
	m.mu.Unlock()

	m.connectOne(ctx, name, s)
	return m.Status(ctx)
}

// Connect force-enables and (re)connects an existing server (mcp/index.ts
// connect: createAndStore with enabled:true). Returns ErrServerNotFound for an
// unknown name.
func (m *Manager) Connect(ctx context.Context, name string) error {
	m.connect(ctx)
	m.mu.Lock()
	s, ok := m.serverConfig(name)
	m.mu.Unlock()
	if !ok {
		return ErrServerNotFound
	}
	enabled := true
	s.Enabled = &enabled
	m.connectOne(ctx, name, s)
	return nil
}

// Disconnect closes an existing server's connection and marks it disabled
// (mcp/index.ts disconnect). Returns ErrServerNotFound for an unknown name.
func (m *Manager) Disconnect(ctx context.Context, name string) error {
	m.connect(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.serverConfig(name); !ok {
		return ErrServerNotFound
	}
	m.closeLocked(name)
	m.status[name] = Status{Status: "disabled"}
	return nil
}

// SupportsOAuth reports whether name is a remote server with OAuth not disabled
// (mcp/index.ts supportsOAuth). Returns ErrServerNotFound for an unknown name.
func (m *Manager) SupportsOAuth(ctx context.Context, name string) (bool, error) {
	m.connect(ctx)
	m.mu.Lock()
	s, ok := m.serverConfig(name)
	m.mu.Unlock()
	if !ok {
		return false, ErrServerNotFound
	}
	return s.Type == "remote" && !s.oauthDisabled(), nil
}

// HasStoredTokens reports whether name has persisted OAuth tokens
// (mcp/index.ts hasStoredTokens).
func (m *Manager) HasStoredTokens(name string) bool { return hasStoredTokens(name) }

// Exists reports whether name resolves to a configured/added server. Used by the
// HTTP layer to 404 auth-remove on an unknown name (handlers/mcp.ts authRemove).
func (m *Manager) Exists(ctx context.Context, name string) bool {
	m.connect(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.serverConfig(name)
	return ok
}

// StartAuth begins an OAuth flow for a remote server: it performs dynamic client
// registration if needed, generates PKCE + state, and returns the browser
// authorization URL and the oauthState (mcp/index.ts startAuth → { authorizationUrl,
// oauthState }). The pending flow is stashed so FinishAuth can complete it.
func (m *Manager) StartAuth(ctx context.Context, name string) (authURL, oauthState string, err error) {
	m.connect(ctx)
	m.mu.Lock()
	s, ok := m.serverConfig(name)
	m.mu.Unlock()
	if !ok {
		return "", "", ErrServerNotFound
	}
	if s.Type != "remote" || s.oauthDisabled() {
		return "", "", ErrOAuthDisabled
	}

	url, fl, err := startAuthFlow(ctx, name, s)
	if err != nil {
		return "", "", err
	}
	m.mu.Lock()
	if m.pending == nil {
		m.pending = map[string]*authFlow{}
	}
	m.pending[name] = fl
	m.mu.Unlock()
	return url, fl.state, nil
}

// FinishAuth completes a pending OAuth flow with the authorization code: it
// exchanges the code for tokens, then reconnects the server and returns its new
// status (mcp/index.ts finishAuth → Status). Returns ErrServerNotFound for an
// unknown name.
func (m *Manager) FinishAuth(ctx context.Context, name, code string) (Status, error) {
	m.connect(ctx)
	m.mu.Lock()
	s, ok := m.serverConfig(name)
	fl := m.pending[name]
	m.mu.Unlock()
	if !ok {
		return Status{}, ErrServerNotFound
	}
	if fl == nil {
		return Status{Status: "failed", Error: "no pending OAuth flow for MCP server: " + name}, nil
	}

	if err := finishAuthFlow(ctx, name, fl, code); err != nil {
		return Status{Status: "failed", Error: "OAuth completion failed: " + err.Error()}, nil
	}
	m.mu.Lock()
	delete(m.pending, name)
	m.mu.Unlock()

	// Reconnect now that tokens are stored; the persistent token store
	// authenticates the new connection transparently.
	enabled := true
	s.Enabled = &enabled
	m.connectOne(ctx, name, s)
	return m.statusOf(name), nil
}

// RemoveAuth deletes a server's stored OAuth credentials and cancels any pending
// flow (mcp/index.ts removeAuth). It does not disconnect an already-connected
// client.
func (m *Manager) RemoveAuth(name string) error {
	m.mu.Lock()
	delete(m.pending, name)
	m.mu.Unlock()
	return removeAuthEntry(name)
}

// statusOf returns the cached status for name (disabled if unknown). Used after a
// reconnect to report the resulting status.
func (m *Manager) statusOf(name string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.status[name]; ok {
		return st
	}
	return Status{Status: "disabled"}
}

// connectOne (re)connects a single server and publishes its status/client/tools
// into the live maps, replacing any prior connection. A disabled server is closed
// and marked disabled; a dial failure is classified (failed/needs_auth/…) like
// the initial connect. It dials WITHOUT holding m.mu (I/O), then publishes under
// the lock, dropping the result if Close ran during the dial.
func (m *Manager) connectOne(ctx context.Context, name string, s Server) {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return
	}

	if !s.enabled() {
		m.mu.Lock()
		if !m.closed {
			m.closeLocked(name)
			m.ensureMaps()
			m.status[name] = Status{Status: "disabled"}
		}
		m.mu.Unlock()
		return
	}

	c, tl, err := m.dialAndList(ctx, name, s)
	if err != nil {
		st := m.dialErrorStatus(ctx, name, s, err)
		m.mu.Lock()
		if !m.closed {
			m.closeLocked(name)
			m.ensureMaps()
			m.status[name] = st
		}
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = c.Close()
		return
	}
	m.closeLocked(name)
	m.ensureMaps()
	m.clients[name] = c
	m.tools[name] = tl
	m.status[name] = Status{Status: "connected"}
	m.mu.Unlock()
}

// ensureMaps lazily initializes the live maps (the initial connect may have early
// -returned on a closed manager). Caller holds m.mu.
func (m *Manager) ensureMaps() {
	if m.status == nil {
		m.status = map[string]Status{}
	}
	if m.clients == nil {
		m.clients = map[string]conn{}
	}
	if m.tools == nil {
		m.tools = map[string][]mcp.Tool{}
	}
}

// closeLocked closes and forgets the client cached for name (idempotent). Caller
// holds m.mu.
func (m *Manager) closeLocked(name string) {
	if c := m.clients[name]; c != nil {
		_ = c.Close()
		delete(m.clients, name)
	}
	delete(m.tools, name)
}
