package lsp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// binUnavailableError marks a server binary as unavailable. The service adds
// such servers to the broken set with a clear message rather than retrying.
type binUnavailableError struct{ name string }

func (e *binUnavailableError) Error() string {
	return fmt.Sprintf("%s not found on PATH and could not be installed", e.name)
}

func errBinaryUnavailable(name string) error { return &binUnavailableError{name: name} }

// binResolver is the default BinResolver. It looks up binaries on PATH and, for
// gopls, auto-installs via `go install` into the Forge cache bin dir when absent
// (unless disabled). typescript-language-server and pyright must already be on
// PATH (matching opencode's behaviour of resolving them via the toolchain rather
// than downloading from this slice). Mirrors lsp/server.ts Gopls.spawn:354-375.
type binResolver struct {
	// disableAutoInstall mirrors opencode's RuntimeFlags.disableLspDownload: when
	// true, gopls is never installed (only resolved from PATH or a prior install).
	disableAutoInstall bool

	once       sync.Once
	goplsPath  string
	goplsErr   error
	binDirOnce sync.Once
	binDir     string
}

// NewBinResolver builds a BinResolver. disableAutoInstall suppresses the gopls
// `go install`.
func NewBinResolver(disableAutoInstall bool) BinResolver {
	return &binResolver{disableAutoInstall: disableAutoInstall}
}

// Which returns the absolute path to name on PATH, or "" if absent. It also
// checks the Forge cache bin dir (where a prior gopls install lives) so an
// installed-but-not-on-PATH binary is still found.
func (r *binResolver) Which(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if dir := r.cacheBinDir(); dir != "" {
		p := filepath.Join(dir, name)
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

// EnsureGopls returns a usable gopls path, installing it once via `go install`
// when absent and auto-install is enabled. The result (path or empty) is cached
// for the resolver's lifetime so concurrent spawns don't trigger parallel
// installs. Ports lsp/server.ts:354-375.
func (r *binResolver) EnsureGopls() string {
	r.once.Do(func() {
		if p := r.Which("gopls"); p != "" {
			r.goplsPath = p
			return
		}
		if r.disableAutoInstall {
			r.goplsErr = errors.New("gopls not found and auto-install disabled")
			return
		}
		// gopls is installed with the Go toolchain; without `go` we cannot install.
		goBin, err := exec.LookPath("go")
		if err != nil {
			r.goplsErr = errors.New("gopls not found and `go` is unavailable to install it")
			return
		}
		dir := r.cacheBinDir()
		if dir == "" {
			r.goplsErr = errors.New("could not resolve a cache bin directory for gopls install")
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			r.goplsErr = fmt.Errorf("create gopls bin dir: %w", err)
			return
		}
		// `go install golang.org/x/tools/gopls@latest` with GOBIN pointed at the
		// Forge cache bin dir (lsp/server.ts:360-361).
		cmd := exec.Command(goBin, "install", "golang.org/x/tools/gopls@latest")
		cmd.Env = append(os.Environ(), "GOBIN="+dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			r.goplsErr = fmt.Errorf("install gopls: %w: %s", err, string(out))
			return
		}
		installed := filepath.Join(dir, "gopls")
		if !isExecutable(installed) {
			r.goplsErr = errors.New("gopls install reported success but binary is missing")
			return
		}
		r.goplsPath = installed
	})
	return r.goplsPath
}

// cacheBinDir is $XDG_CACHE_HOME/forge/bin (falling back to ~/.cache/forge/bin),
// matching the cache convention used elsewhere in Forge (engine/catalog) and
// mirroring opencode's Global.Path.bin (= xdgCache/opencode/bin).
func (r *binResolver) cacheBinDir() string {
	r.binDirOnce.Do(func() {
		cacheHome := os.Getenv("XDG_CACHE_HOME")
		if cacheHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return
			}
			cacheHome = filepath.Join(home, ".cache")
		}
		r.binDir = filepath.Join(cacheHome, "forge", "bin")
	})
	return r.binDir
}

// isExecutable reports whether p exists and is a regular, executable file.
func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}
