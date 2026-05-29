package instance

import (
	"sync"

	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/pty"
)

// Context is the per-directory in-memory state for one project instance. It
// holds the instance event bus and PTY manager; config/LSP attach here in later
// milestones (plan 01 §7).
type Context struct {
	Directory string
	Bus       *bus.Bus
	Pty       *pty.Manager
}

// Manager is the directory→instance cache. Instances are created on first use
// and kept for the server lifetime (opencode keeps them with no TTL;
// project/instance-store.ts:105-120). The cache is keyed by the canonical
// (symlink-resolved) directory path produced by directory resolution.
type Manager struct {
	mu        sync.Mutex
	instances map[string]*Context
	global    *bus.Global
}

// NewManager creates an empty instance manager whose instance buses forward to
// the given global bus.
func NewManager(global *bus.Global) *Manager {
	return &Manager{instances: make(map[string]*Context), global: global}
}

// Get returns the instance for directory, creating it on first use. Creation is
// trivial today (a fresh bus), so a single lock suffices; when init grows
// expensive (config/LSP) this becomes the single-flight point (plan 01 §7).
func (m *Manager) Get(directory string) *Context {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.instances[directory]; ok {
		return c
	}
	c := &Context{
		Directory: directory,
		Bus:       bus.NewInstanceBus(directory, m.global),
		// configShell is wired from config in a later milestone; PreferredShell
		// falls back to $SHELL / a platform default until then.
		Pty: pty.NewManager(directory, ""),
	}
	m.instances[directory] = c
	return c
}
