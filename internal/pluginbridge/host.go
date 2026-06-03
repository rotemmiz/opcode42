package pluginbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// readyTimeout bounds how long Start waits for the host to connect and send
// host.ready before giving up (plan 05 §Startup: 30s).
const readyTimeout = 30 * time.Second

// shutdownGrace is how long Stop waits for the host to exit after host.shutdown
// before sending SIGKILL (plan 05 §Shutdown: 5s).
const shutdownGrace = 5 * time.Second

// hostProcess owns the spawned sidecar and its listening socket.
type hostProcess struct {
	cmd        *exec.Cmd
	listener   net.Listener
	socketPath string
}

// Start spawns the plugin host, waits for it to connect and report host.ready,
// then wires the bridge connection. When the bridge is disabled it is a no-op.
// A startup failure is non-fatal: it is logged and the bridge stays a no-op so
// the daemon runs plugin-free (plan 05 §"Partial-compat fallback strategy").
func (b *Bridge) Start(ctx context.Context) error {
	if b == nil || !b.cfg.Enabled {
		return nil
	}
	b.mu.Lock()
	if b.startedAt {
		b.mu.Unlock()
		return nil
	}
	b.startedAt = true
	b.mu.Unlock()

	if err := b.start(ctx); err != nil {
		b.log.Warn("plugin host failed to start; continuing without plugins", "err", err)
		return err
	}
	return nil
}

func (b *Bridge) start(ctx context.Context) error {
	script, err := resolveHostScript(b.cfg.HostScript)
	if err != nil {
		return err
	}
	runtimeBin, err := resolveRuntime(b.cfg.Runtime)
	if err != nil {
		return err
	}

	socketPath, err := newSocketPath()
	if err != nil {
		return err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen unix %s: %w", socketPath, err)
	}

	host := &hostProcess{listener: ln, socketPath: socketPath}

	specsJSON, err := json.Marshal(b.cfg.PluginSpecs)
	if err != nil {
		_ = ln.Close()
		return err
	}

	cmd := exec.CommandContext(ctx, runtimeBin, runtimeArgs(runtimeBin, script)...)
	cmd.Env = append(os.Environ(),
		"FORGE_PLUGIN_SOCKET="+socketPath,
		"FORGE_URL="+b.cfg.ServerURL,
		"FORGE_AUTH_HEADER="+b.cfg.AuthHeader,
		"FORGE_DIRECTORY="+b.cfg.Directory,
		"FORGE_PLUGIN_SPECS="+string(specsJSON),
	)
	cmd.Stdout = os.Stderr // host log output to the daemon's stderr
	cmd.Stderr = os.Stderr
	host.cmd = cmd

	// Accept the host's connection in the background; the host connects after it
	// boots and loads plugins.
	type acceptResult struct {
		c   net.Conn
		err error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		c, err := ln.Accept()
		acceptCh <- acceptResult{c, err}
	}()

	if err := cmd.Start(); err != nil {
		_ = ln.Close()
		_ = os.Remove(socketPath)
		return fmt.Errorf("spawn plugin host (%s): %w", runtimeBin, err)
	}

	b.mu.Lock()
	b.host = host
	b.mu.Unlock()

	// Reap the process if it dies; this also unblocks Accept via socket cleanup.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	var c *conn
	select {
	case <-time.After(readyTimeout):
		b.stopHost()
		return fmt.Errorf("plugin host did not connect within %s", readyTimeout)
	case err := <-exited:
		b.stopHost()
		return fmt.Errorf("plugin host exited before connecting: %w", err)
	case res := <-acceptCh:
		if res.err != nil {
			b.stopHost()
			return fmt.Errorf("accept plugin host: %w", res.err)
		}
		c = newConn(res.c, b.log, b.onNotify)
		b.mu.Lock()
		b.conn = c
		b.mu.Unlock()
		// Watch for the host going away so Ready flips back to false.
		go b.watchConn(c)
	}

	// Wait for host.ready (delivered via onNotify) within the remaining budget.
	// Bail out early if the host process exits or the connection drops before
	// ready arrives, rather than spinning for the whole readyTimeout.
	deadline := time.After(readyTimeout)
	for {
		if b.Ready() {
			return nil
		}
		select {
		case <-deadline:
			b.stopHost()
			return errors.New("plugin host connected but never sent host.ready")
		case err := <-exited:
			b.stopHost()
			return fmt.Errorf("plugin host exited before host.ready: %w", err)
		case <-c.Done():
			b.stopHost()
			return errors.New("plugin host disconnected before host.ready")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// watchConn flips ready off and clears the connection when the host disconnects.
func (b *Bridge) watchConn(c *conn) {
	<-c.Done()
	b.mu.Lock()
	if b.conn == c {
		b.conn = nil
		b.ready = false
	}
	b.mu.Unlock()
	b.log.Warn("plugin host disconnected; continuing without plugins")
}

// Stop gracefully shuts the host down: send host.shutdown, give it shutdownGrace
// to drain dispose hooks and exit, then SIGKILL. Safe to call on a disabled or
// never-started bridge.
func (b *Bridge) Stop(_ context.Context) {
	if b == nil || !b.cfg.Enabled {
		return
	}
	b.mu.RLock()
	c := b.conn
	b.mu.RUnlock()
	if c != nil {
		_ = c.notify("host.shutdown", map[string]any{})
	}
	b.stopHost()
}

func (b *Bridge) stopHost() {
	b.mu.Lock()
	host := b.host
	c := b.conn
	b.host = nil
	b.conn = nil
	b.ready = false
	b.mu.Unlock()

	if host == nil {
		if c != nil {
			_ = c.Close()
		}
		return
	}
	if host.cmd != nil && host.cmd.Process != nil {
		done := make(chan struct{})
		go func() { _, _ = host.cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(shutdownGrace):
			_ = host.cmd.Process.Kill()
			<-done
		}
	}
	if c != nil {
		_ = c.Close()
	}
	if host.listener != nil {
		_ = host.listener.Close()
	}
	if host.socketPath != "" {
		_ = os.Remove(host.socketPath)
	}
}

// runtimeArgs builds the argv for spawning the host script. bun runs scripts
// via `bun run <script>`; node executes a script path directly (a leading
// `run` would be treated as a module specifier). Node ≥22 strips TS types from
// the .ts entry natively; bun runs TS without flags.
func runtimeArgs(runtimeBin, script string) []string {
	if filepath.Base(runtimeBin) == "bun" || filepath.Base(runtimeBin) == "bun.exe" {
		return []string{"run", script}
	}
	return []string{script}
}

// resolveRuntime picks the JS runtime per plan 05 §"Bun vs Node": honour an
// explicit choice, else prefer bun, else node.
func resolveRuntime(pref string) (string, error) {
	switch pref {
	case "bun", "node":
		if p, err := exec.LookPath(pref); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("requested plugin runtime %q not found on PATH", pref)
	case "":
		if p, err := exec.LookPath("bun"); err == nil {
			return p, nil
		}
		if p, err := exec.LookPath("node"); err == nil {
			return p, nil
		}
		return "", errors.New("no plugin runtime found: install bun or node, or set FORGE_PLUGIN_RUNTIME")
	default:
		return "", fmt.Errorf("invalid plugin runtime %q (want bun|node)", pref)
	}
}

// resolveHostScript finds the plugin-host entry script. Explicit override wins;
// otherwise FORGE_PLUGIN_HOST_SCRIPT; otherwise the bundled package relative to
// the executable. The file must exist.
func resolveHostScript(override string) (string, error) {
	candidates := []string{override, os.Getenv("FORGE_PLUGIN_HOST_SCRIPT")}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "forge-plugin-host", "src", "index.ts"),
			filepath.Join(dir, "..", "packages", "forge-plugin-host", "src", "index.ts"),
		)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return filepath.Abs(c)
		}
	}
	return "", errors.New("plugin host script not found (set FORGE_PLUGIN_HOST_SCRIPT)")
}

// newSocketPath returns a short, unique unix-socket path. macOS/BSD cap the
// sun_path length (~104 bytes) so we keep it short under the temp dir.
func newSocketPath() (string, error) {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	name := fmt.Sprintf("forge-ph-%s.sock", hex.EncodeToString(buf[:]))
	path := filepath.Join(os.TempDir(), name)
	if runtime.GOOS == "windows" {
		// Unix sockets exist on modern Windows but path semantics differ; the
		// stdio fallback (plan 05) is a future milestone, so surface clearly.
		return path, nil
	}
	if len(path) > 100 {
		path = filepath.Join("/tmp", name)
	}
	return path, nil
}
