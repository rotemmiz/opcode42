package lsp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"syscall"

	"github.com/rotemmiz/forge/internal/config"
)

// Service is one instance's LSP lifecycle manager. This foundation slice owns
// lazy server spawning keyed by serverID+root, a broken set (servers that fail
// to spawn are not retried), spawn dedup (one server is not spawned twice for
// the same root), and process-group cleanup on DisposeAll. The JSON-RPC
// handshake and query operations are deferred to M3-4/M3-5; here a spawned
// server is just a running child process whose stdio is held open.
//
// Mirrors lsp/lsp.ts State (clients/servers/broken/spawning, lsp.ts:116-121) and
// getClients lazy spawn (lsp.ts:211-298).
type Service struct {
	directory string
	servers   map[string]ServerDef
	resolver  BinResolver

	mu       sync.Mutex
	disposed bool
	// running is keyed by serverID+"\x00"+root.
	running map[string]*handle
	// broken records spawn keys that failed; they are never retried.
	broken map[string]string // key → reason
	// spawning dedups concurrent spawns for the same key.
	spawning map[string]*spawnOnce
}

// handle is a spawned server process. In this slice it only tracks the process
// for lifecycle/cleanup; the JSON-RPC connection is added in M3-4.
type handle struct {
	serverID string
	root     string
	cmd      *exec.Cmd
	pgid     int
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
	return &Service{
		directory: directory,
		servers:   activeServers(cfg),
		resolver:  resolver,
		running:   make(map[string]*handle),
		broken:    make(map[string]string),
		spawning:  make(map[string]*spawnOnce),
	}
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

	s.mu.Lock()
	delete(s.spawning, key)
	switch {
	case err != nil:
		s.broken[key] = err.Error()
	case s.disposed:
		// Disposed mid-spawn: kill the freshly spawned process, publish nothing.
		killGroup(h)
		err = nil
		h = nil
	default:
		s.running[key] = h
	}
	so.h, so.err = h, err
	close(so.done)
	s.mu.Unlock()

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
// It does NOT perform the JSON-RPC handshake yet (M3-4). Stdin/stdout pipes are
// established and held so the process stays alive and ready for the future
// client to attach.
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

	// Establish stdio pipes so the process has somewhere to read/write; the
	// JSON-RPC client wires onto these in M3-4. Hold the pipes on the handle is
	// unnecessary for the foundation, but stdin must not be the inherited tty.
	if _, err := cmd.StdinPipe(); err != nil {
		return nil, fmt.Errorf("lsp %s stdin pipe: %w", def.ID, err)
	}
	if _, err := cmd.StdoutPipe(); err != nil {
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
	return &handle{serverID: def.ID, root: root, cmd: cmd, pgid: pgid}, nil
}

// Status reports the server ids currently running, sorted. (The full
// id/name/root/status payload lands with the JSON-RPC client in M3-4.)
func (s *Service) Status() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.running))
	for _, h := range s.running {
		out = append(out, h.serverID)
	}
	sort.Strings(out)
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
