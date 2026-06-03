package lsp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"go.lsp.dev/protocol"

	"github.com/rotemmiz/forge/internal/config"
)

// Service is one instance's LSP lifecycle manager. It owns lazy server spawning
// keyed by serverID+root, a broken set (servers that fail to spawn/handshake are
// not retried), spawn dedup (one server is not spawned twice for the same root),
// and process-group cleanup on DisposeAll. Each spawned server gets a JSON-RPC
// Client (M3-4) that runs the initialize handshake and tracks diagnostics; on
// the first successful client for any server the service publishes lsp.updated.
//
// Mirrors lsp/lsp.ts State (clients/servers/broken/spawning, lsp.ts:116-121) and
// getClients lazy spawn (lsp.ts:211-298).
type Service struct {
	directory string
	servers   map[string]ServerDef
	resolver  BinResolver
	// bus, when set, receives the lsp.updated event on first client spawn.
	bus EventPublisher
	// connect builds the JSON-RPC client over a spawned process's stdio. It is a
	// field so unit tests (which spawn a stand-in process such as `sleep`) can
	// substitute a no-op that skips the real LSP handshake. Production uses
	// connectClient (the real initialize/initialized handshake).
	connect func(ctx context.Context, def ServerDef, root string, h *handle) (*Client, error)

	mu       sync.Mutex
	disposed bool
	// running is keyed by serverID+"\x00"+root.
	running map[string]*handle
	// broken records spawn keys that failed; they are never retried.
	broken map[string]string // key → reason
	// spawning dedups concurrent spawns for the same key.
	spawning map[string]*spawnOnce
}

// EventPublisher emits an SSE bus event (the {id,type,properties} envelope is
// built by the bus). Kept as an interface to avoid an import cycle and to let
// tests substitute a recorder. Mirrors mcp.eventPublisher.
type EventPublisher interface {
	Publish(typ string, props any)
}

// handle is a spawned server process plus its JSON-RPC client. cmd/pgid drive
// process-group cleanup; client (when non-nil) is the initialized LSP client
// used for touchFile/diagnostics.
type handle struct {
	serverID string
	root     string
	cmd      *exec.Cmd
	pgid     int
	// rwc is the spawned process's stdio (stdout+stdin) for the JSON-RPC stream.
	rwc *stdioRWC
	// client is the initialized JSON-RPC client (nil until connect succeeds, and
	// for the test connect hook that skips the handshake).
	client *Client
}

// spawnOnce coordinates concurrent spawns of the same serverID+root: the first
// caller runs the spawn, others wait on done and read the shared result.
type spawnOnce struct {
	done chan struct{}
	h    *handle
	err  error
}

// NewService builds an LSP service for an instance directory. cfg gates which
// servers are active (bool|map from config); resolver locates/installs binaries.
// When cfg is disabled, the service has no active servers (spawning is a no-op).
func NewService(directory string, cfg config.LSPConfig, resolver BinResolver) *Service {
	if resolver == nil {
		resolver = NewBinResolver(false)
	}
	s := &Service{
		directory: directory,
		servers:   activeServers(cfg),
		resolver:  resolver,
		running:   make(map[string]*handle),
		broken:    make(map[string]string),
		spawning:  make(map[string]*spawnOnce),
	}
	s.connect = s.connectClient
	return s
}

// WithBus attaches an event publisher so the service emits lsp.updated when a
// server's client first spawns successfully (lsp.ts:294). Returns the service
// for chaining at construction time.
func (s *Service) WithBus(b EventPublisher) *Service {
	s.bus = b
	return s
}

// connectClient is the production connect hook: it adapts the spawned process's
// stdio (the rwc set up by spawn) into a JSON-RPC stream and runs the initialize
// handshake (M3-4).
func (s *Service) connectClient(ctx context.Context, def ServerDef, root string, h *handle) (*Client, error) {
	if h.rwc == nil {
		return nil, fmt.Errorf("lsp %s: no stdio for handshake", def.ID)
	}
	return newClient(ctx, def.ID, root, s.directory, h.rwc, h.rwc, def.Initialization)
}

// activeServers resolves the built-in subset against the LSP config: disabled
// config ⇒ none; otherwise all built-ins minus any explicitly disabled in
// config. Custom (non-built-in) entries are accepted by config parsing but have
// no built-in spawn definition in this foundation slice, so they are ignored
// here (a generic custom-command spawn is a follow-up).
func activeServers(cfg config.LSPConfig) map[string]ServerDef {
	if !cfg.Enabled {
		return nil
	}
	out := make(map[string]ServerDef, len(Servers))
	for id, def := range Servers {
		if entry, ok := cfg.Servers[id]; ok && entry.IsDisabled() {
			continue
		}
		out[id] = def
	}
	return out
}

// spawnKey is the dedup/broken key for a server at a root.
func spawnKey(serverID, root string) string { return serverID + "\x00" + root }

// EnsureClients lazily spawns every active server that matches file's extension,
// returning the server ids that are running for file's roots. It is the
// foundation analog of opencode's getClients (lsp.ts:211-298): for each matching
// server it resolves the root, skips broken keys, reuses a running process,
// dedups concurrent spawns, and otherwise spawns. The JSON-RPC client (and the
// lsp.updated SSE event) land in M3-4.
func (s *Service) EnsureClients(file string) ([]string, error) {
	ext := filepath.Ext(file)

	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return nil, nil
	}
	// Snapshot the matching servers under the lock.
	type cand struct {
		def  ServerDef
		root string
		key  string
	}
	var cands []cand
	for _, def := range s.servers {
		if !def.matchesExtension(ext) {
			continue
		}
		root := def.Root(file, s.directory)
		if root == "" {
			continue
		}
		cands = append(cands, cand{def: def, root: root, key: spawnKey(def.ID, root)})
	}
	s.mu.Unlock()

	var (
		ids      []string
		firstErr error
	)
	for _, c := range cands {
		id, err := s.ensureOne(c.def, c.root, c.key)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, firstErr
}

// ensureOne returns the server id if a process is running (existing or freshly
// spawned) for def at root, or "" with an error if it is broken / failed. It
// dedups concurrent spawns for the same key via the spawning map.
func (s *Service) ensureOne(def ServerDef, root, key string) (string, error) {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return "", nil
	}
	if reason, broken := s.broken[key]; broken {
		s.mu.Unlock()
		return "", fmt.Errorf("lsp server %q is broken: %s", def.ID, reason)
	}
	if _, ok := s.running[key]; ok {
		s.mu.Unlock()
		return def.ID, nil
	}
	if so, ok := s.spawning[key]; ok {
		// Another caller is spawning this exact server+root; wait for it.
		s.mu.Unlock()
		<-so.done
		if so.err != nil {
			return "", so.err
		}
		return def.ID, nil
	}
	so := &spawnOnce{done: make(chan struct{})}
	s.spawning[key] = so
	s.mu.Unlock()

	h, err := s.spawn(def, root)
	if err == nil {
		// Run the JSON-RPC handshake (45s init timeout is enforced inside connect).
		var client *Client
		client, err = s.connect(context.Background(), def, root, h)
		if err != nil {
			killGroup(h) // handshake failed: tear down the process
		} else {
			h.client = client
		}
	}

	s.mu.Lock()
	delete(s.spawning, key)
	publish := false
	switch {
	case err != nil:
		s.broken[key] = err.Error()
	case s.disposed:
		// Disposed mid-spawn: shut down the freshly built client + process.
		if h.client != nil {
			h.client.Shutdown()
		}
		killGroup(h)
		err = nil
		h = nil
	default:
		s.running[key] = h
		publish = true
	}
	so.h, so.err = h, err
	close(so.done)
	s.mu.Unlock()

	// lsp.updated fires after a new client spawns successfully (lsp.ts:294).
	if publish && s.bus != nil {
		s.bus.Publish("lsp.updated", map[string]any{})
	}

	if err != nil {
		return "", err
	}
	if h == nil {
		return "", nil
	}
	return def.ID, nil
}

// spawn starts the server process in its own process group (Setpgid) so the
// whole tree can be signalled on cleanup (plan 03 risk #5; lsp/server.ts spawn).
// It sets up the stdin/stdout pipes (held on the handle as an rwc) that the
// JSON-RPC client attaches to in connect; stderr goes to the daemon's stderr.
func (s *Service) spawn(def ServerDef, root string) (*handle, error) {
	argv, err := def.Command(root, s.resolver)
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, errBinaryUnavailable(def.ID)
	}

	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // argv is from the built-in server table / resolved binaries
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Establish the stdio pipes for the JSON-RPC stream (must be created before
	// Start). stdin must not be the inherited tty.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp %s stdin pipe: %w", def.ID, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp %s stdout pipe: %w", def.ID, err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn lsp %s: %w", def.ID, err)
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Fall back to the pid as its own group (Setpgid should make pgid == pid).
		pgid = cmd.Process.Pid
	}
	return &handle{
		serverID: def.ID,
		root:     root,
		cmd:      cmd,
		pgid:     pgid,
		rwc:      &stdioRWC{r: stdout, w: stdin},
	}, nil
}

// StatusItem is one running server's wire status, matching opencode's LSPStatus
// (lsp.ts:53-59 / openapi LSPStatus): id, name, root (relative to the instance
// directory), and a connected/error status. Forge only retains connected
// clients (a failed handshake lands the server in the broken set, not here), so
// Status is always "connected" today.
type StatusItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Root   string `json:"root"`
	Status string `json:"status"` // "connected" | "error"
}

// Status returns the wire status for every running client, sorted by id. Ports
// LSP.status (lsp.ts:315-328): root is relative to the instance directory.
func (s *Service) Status() []StatusItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]StatusItem, 0, len(s.running))
	for _, h := range s.running {
		root := h.root
		if rel, err := filepath.Rel(s.directory, h.root); err == nil {
			// opencode uses Node path.relative, which returns "" (not ".") when the
			// root IS the instance directory. Match that exact wire value.
			if rel == "." {
				rel = ""
			}
			root = rel
		}
		out = append(out, StatusItem{
			ID:     h.serverID,
			Name:   h.serverID,
			Root:   root,
			Status: "connected",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Root < out[j].Root
	})
	return out
}

// runningIDs reports the server ids currently running, sorted (testing helper).
func (s *Service) runningIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.running))
	for _, h := range s.running {
		out = append(out, h.serverID)
	}
	sort.Strings(out)
	return out
}

// TouchFile lazily spawns the matching servers for file (triggering the
// handshake + lsp.updated), opens the file on each client, and — when mode is
// non-empty — waits for that file's diagnostics. Ports LSP.touchFile
// (lsp.ts:346-366). Errors opening/spawning are swallowed (best-effort, like
// opencode's catch) so a flaky server doesn't fail the caller.
func (s *Service) TouchFile(ctx context.Context, file string, mode DiagnosticsMode) {
	_, _ = s.EnsureClients(file)
	for _, c := range s.clientsForFile(file) {
		if _, err := c.Open(ctx, file); err != nil {
			continue
		}
		if mode != "" {
			c.WaitForDiagnostics(ctx, file, mode)
		}
	}
}

// Diagnostics aggregates every running client's merged diagnostics, keyed by
// absolute file path. Ports LSP.diagnostics (lsp.ts:368-379).
func (s *Service) Diagnostics() map[string][]protocol.Diagnostic {
	out := map[string][]protocol.Diagnostic{}
	for _, c := range s.allClients() {
		for p, diags := range c.Diagnostics() {
			out[p] = append(out[p], diags...)
		}
	}
	return out
}

// clientsForFile returns the running clients whose server matches file's
// extension and whose root contains the file. It does NOT spawn (callers spawn
// via EnsureClients first). Mirrors opencode's run()/getClients filtering by
// extension + root (lsp.ts:260-271, 301-304).
func (s *Service) clientsForFile(file string) []*Client {
	ext := filepath.Ext(file)
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(s.directory, abs)
	}
	abs = filepath.Clean(abs)

	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Client
	for _, h := range s.running {
		if h.client == nil {
			continue
		}
		def, ok := s.servers[h.serverID]
		if !ok || !def.matchesExtension(ext) {
			continue
		}
		if !underRoot(abs, h.root) {
			continue
		}
		out = append(out, h.client)
	}
	return out
}

// underRoot reports whether abs is root itself or a descendant of root.
func underRoot(abs, root string) bool {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

// allClients returns every running client (nil-client handles skipped).
func (s *Service) allClients() []*Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Client
	for _, h := range s.running {
		if h.client != nil {
			out = append(out, h.client)
		}
	}
	return out
}

// brokenSnapshot returns a copy of the broken set (testing/diagnostics).
func (s *Service) brokenSnapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.broken))
	for k, v := range s.broken {
		out[k] = v
	}
	return out
}

// DisposeAll terminates every running server by SIGTERM'ing its process group
// (POSIX, plan 03 risk #5: syscall.Kill(-pgid, SIGTERM)). It is idempotent and
// marks the service disposed so no further spawns occur.
func (s *Service) DisposeAll() {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	s.disposed = true
	handles := make([]*handle, 0, len(s.running))
	for _, h := range s.running {
		handles = append(handles, h)
	}
	s.running = make(map[string]*handle)
	s.mu.Unlock()

	for _, h := range handles {
		if h.client != nil {
			h.client.Shutdown()
		}
		killGroup(h)
	}
}

// killGroup SIGTERMs the process group of a spawned server (negative pgid), then
// reaps it so no zombie remains. A nil handle is a no-op.
func killGroup(h *handle) {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return
	}
	if h.pgid > 0 {
		_ = syscall.Kill(-h.pgid, syscall.SIGTERM)
	} else {
		_ = h.cmd.Process.Signal(syscall.SIGTERM)
	}
	_ = h.cmd.Wait()
}
