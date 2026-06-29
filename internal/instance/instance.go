package instance

import (
	"context"
	"sync"

	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/config"
	"github.com/rotemmiz/opcode42/internal/engine/permission"
	"github.com/rotemmiz/opcode42/internal/engine/question"
	"github.com/rotemmiz/opcode42/internal/engine/runstate"
	"github.com/rotemmiz/opcode42/internal/lsp"
	"github.com/rotemmiz/opcode42/internal/mcp"
	"github.com/rotemmiz/opcode42/internal/pluginbridge"
	"github.com/rotemmiz/opcode42/internal/pty"
)

// Context is the per-directory in-memory state for one project instance. It
// holds the instance event bus, PTY manager, and the agent engine's
// per-instance state (the permission manager and the per-session run lock);
// config/LSP attach here in later milestones (plan 01 §7).
type Context struct {
	Directory   string
	Bus         *bus.Bus
	Pty         *pty.Manager
	Permissions *permission.Manager
	Questions   *question.Manager
	RunState    *runstate.RunState
	// MCP holds this instance's configured MCP servers (connection is lazy).
	MCP *mcp.Manager
	// LSP holds this instance's LSP servers (spawning is lazy, per file).
	LSP *lsp.Service
	// Plugins is the flag-gated plugin host for this instance (plan 05). It is
	// nil unless the daemon was started with the plugin host enabled and a
	// bridge factory was registered; when nil the engine's hook call sites are
	// no-ops. The bridge is started lazily on first instance use.
	Plugins *pluginbridge.Bridge
}

// Manager is the directory→instance cache. Instances are created on first use
// and kept for the server lifetime (opencode keeps them with no TTL;
// project/instance-store.ts:105-120). The cache is keyed by the canonical
// (symlink-resolved) directory path produced by directory resolution.
type Manager struct {
	mu        sync.Mutex
	instances map[string]*Context
	global    *bus.Global
	// pluginFactory builds a (started) plugin host for a directory when the
	// daemon has the plugin host enabled. nil ⇒ no plugin host (the default).
	pluginFactory PluginFactory
}

// PluginFactory builds and starts a plugin host bridge for one directory. It is
// registered by the daemon entrypoint only when the plugin host flag is on; a
// nil return (or an error swallowed inside the factory) leaves the instance
// plugin-free. Returning a disabled bridge is also fine (every call no-ops).
type PluginFactory func(directory string) *pluginbridge.Bridge

// NewManager creates an empty instance manager whose instance buses forward to
// the given global bus.
func NewManager(global *bus.Global) *Manager {
	return &Manager{instances: make(map[string]*Context), global: global}
}

// SetPluginFactory registers the plugin host factory (plan 05). Call before any
// instance is created; it is consulted once per new instance.
func (m *Manager) SetPluginFactory(f PluginFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pluginFactory = f
}

// DisposeAll tears down every instance on shutdown: it emits
// server.instance.disposed on each instance bus (which terminates that
// instance's /event SSE stream — handlers/event.ts:30-31) and shuts down its
// PTY sessions, then clears the cache (project/instance-store.ts:77-89).
func (m *Manager) DisposeAll() {
	m.mu.Lock()
	contexts := make([]*Context, 0, len(m.instances))
	for _, c := range m.instances {
		contexts = append(contexts, c)
	}
	m.instances = make(map[string]*Context)
	m.mu.Unlock()

	for _, c := range contexts {
		c.Bus.Publish(bus.NewEvent(bus.EventInstanceDisposed, map[string]any{"directory": c.Directory}))
		if c.Pty != nil {
			c.Pty.Shutdown()
		}
		if c.MCP != nil {
			c.MCP.Close()
		}
		if c.LSP != nil {
			c.LSP.DisposeAll()
		}
		if c.Plugins != nil {
			c.Plugins.Stop(context.Background())
		}
	}
}

// Get returns the instance for directory, creating it on first use. Creation is
// trivial today (a fresh bus + PTY manager), so a single lock suffices; when
// init grows expensive (config/LSP) this becomes the single-flight point
// (plan 01 §7).
func (m *Manager) Get(directory string) *Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.instances[directory]; ok {
		return c
	}
	instBus := bus.NewInstanceBus(directory, m.global)
	cfg := loadConfig(directory)
	c := &Context{
		Directory: directory,
		Bus:       instBus,
		// configShell is wired from config in a later milestone; PreferredShell
		// falls back to $SHELL / a platform default until then.
		Pty:         pty.NewManager(directory, ""),
		Permissions: permission.NewManager(instBus),
		Questions:   question.NewManager(instBus),
		RunState:    runstate.New(),
		MCP:         mcp.NewManager(mcp.ParseConfig(cfg)).WithBus(mcpBus{instBus}),
		LSP:         newLSP(directory, cfg, instBus),
	}
	if m.pluginFactory != nil {
		c.Plugins = m.pluginFactory(directory)
	}
	m.instances[directory] = c
	return c
}

// BusFor returns the instance bus serving directory, creating the instance on
// first use (like Get). It is the publish side for directory-scoped lifecycle
// events (session.created/updated/deleted, message.removed) that originate
// outside an active run — e.g. the session HTTP handlers and async title
// generation.
func (m *Manager) BusFor(directory string) *bus.Bus {
	return m.Get(directory).Bus
}

// loadConfig loads the directory's merged opencode config (nil on error, so a
// bad config never blocks instance creation).
func loadConfig(directory string) map[string]any {
	cfg, err := config.Load(directory)
	if err != nil {
		return nil
	}
	return cfg
}

// mcpBus adapts the instance bus to mcp.eventPublisher / lsp.EventPublisher so
// the managers can emit events (mcp.tools.changed, lsp.updated) in opencode's
// {id,type,properties} shape.
type mcpBus struct{ b *bus.Bus }

func (m mcpBus) Publish(typ string, props any) { m.b.Publish(bus.NewEvent(typ, props)) }

// newLSP builds the instance's LSP service from the loaded config, wiring the
// instance bus so the service emits lsp.updated when a client first spawns. A
// malformed `lsp` block (e.g. a custom server missing `extensions`) disables LSP
// for the instance rather than blocking creation.
func newLSP(directory string, cfg map[string]any, instBus *bus.Bus) *lsp.Service {
	lspCfg, err := config.ParseLSP(cfg, lsp.BuiltinIDs())
	if err != nil {
		lspCfg = config.LSPConfig{}
	}
	return lsp.NewService(directory, lspCfg, lsp.NewBinResolver(false)).WithBus(mcpBus{instBus})
}
