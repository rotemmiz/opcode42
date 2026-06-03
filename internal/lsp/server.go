// Package lsp is Forge's per-instance LSP integration. This foundation slice
// (plan 03 M3-3, focused subset) ports the built-in server table and lazy-spawn
// lifecycle for three servers — gopls, typescript-language-server, and pyright —
// plus gopls auto-install. The JSON-RPC client and query operations (hover,
// definition, references, …) are follow-ups (M3-4/M3-5). Mirrors opencode's
// lsp/server.ts + lsp/lsp.ts. POSIX only (Windows is out of scope; masterplan
// "Decisions locked" #5).
package lsp

import (
	"os"
	"path/filepath"
)

// ServerDef describes one built-in language server: the file extensions it
// attaches to, how to find a file's project root, and how to spawn it. Mirrors
// lsp/server.ts interface Info (id, extensions, root, spawn).
type ServerDef struct {
	// ID is the server's stable identifier (matches opencode's server ids so
	// config overrides line up).
	ID string
	// Extensions is the set of file extensions (with leading dot) the server
	// handles.
	Extensions []string
	// Root resolves the project root for a file by walking up to the instance
	// directory. It returns "" when no root is found (the caller skips the
	// server for that file). instanceDir bounds the upward walk.
	Root func(file, instanceDir string) string
	// Command builds the argv to spawn for a given root. resolveBin locates (and,
	// for gopls, may auto-install) the server binary; it returns "" when the
	// binary is unavailable, in which case the server is added to the broken set.
	Command func(root string, resolveBin BinResolver) ([]string, error)
}

// BinResolver locates a server binary on PATH (and, for gopls, may auto-install
// it). It returns the absolute path, or "" if the binary is unavailable.
type BinResolver interface {
	// Which returns the path to name on PATH, or "" if absent.
	Which(name string) string
	// EnsureGopls returns a path to gopls, installing it via `go install` when
	// absent and auto-install is enabled. Returns "" if it cannot be made
	// available.
	EnsureGopls() string
}

// nearestRoot walks up from filepath.Dir(file) to instanceDir (inclusive),
// returning the directory of the first ancestor that contains any of includes.
// When excludes is non-empty and an excluded marker is found first (scanning
// from the start dir upward), it returns "" — this lets typescript bail out on
// Deno projects. When no include marker is found, it returns instanceDir
// (matching opencode's NearestRoot, which falls back to ctx.directory).
// Ports lsp/server.ts:34-56 + Filesystem.up (filesystem.ts:214).
func nearestRoot(file, instanceDir string, includes, excludes []string) string {
	start := filepath.Dir(file)

	if len(excludes) > 0 && walkUpFind(start, instanceDir, excludes) != "" {
		return ""
	}
	if found := walkUpFind(start, instanceDir, includes); found != "" {
		return filepath.Dir(found)
	}
	return instanceDir
}

// walkUpFind walks from start up to stop (inclusive), returning the full path of
// the first existing target found, or "" if none. Mirrors Filesystem.up: at each
// level it checks targets in order, then ascends; it stops after checking stop
// (or at the filesystem root). When stop is not an ancestor of start, the walk
// still terminates at the filesystem root.
func walkUpFind(start, stop string, targets []string) string {
	cur := start
	for {
		for _, t := range targets {
			p := filepath.Join(cur, t)
			if pathExists(p) {
				return p
			}
		}
		if cur == stop {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ""
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// goplsRoot resolves the gopls root: the go.work workspace root if one exists,
// otherwise the nearest go.mod/go.sum (lsp/server.ts:347-351, "go.work then
// go.mod/go.sum"). nearestRoot falls back to instanceDir when no include marker
// is found, so we probe for the actual go.work file first to decide which branch
// is authoritative rather than letting the instanceDir fallback always win (the
// way opencode's chained NearestRoot does).
func goplsRoot(file, instanceDir string) string {
	if walkUpFind(filepath.Dir(file), instanceDir, []string{"go.work"}) != "" {
		return nearestRoot(file, instanceDir, []string{"go.work"}, nil)
	}
	return nearestRoot(file, instanceDir, []string{"go.mod", "go.sum"}, nil)
}

// Servers is the focused subset of built-in LSP servers for the foundation
// slice: gopls, typescript-language-server, pyright. The other ~32 servers from
// opencode's table are post-v1 (plan 03 review pass). Keyed by server id.
var Servers = map[string]ServerDef{
	"gopls": {
		ID:         "gopls",
		Extensions: []string{".go"},
		Root:       goplsRoot,
		Command: func(_ string, r BinResolver) ([]string, error) {
			bin := r.EnsureGopls()
			if bin == "" {
				return nil, errBinaryUnavailable("gopls")
			}
			return []string{bin}, nil
		},
	},
	"typescript": {
		ID: "typescript",
		// Excludes deno.json/deno.jsonc so this server does not attach to Deno
		// projects (lsp/server.ts:96-100).
		Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir,
				[]string{"package-lock.json", "bun.lockb", "bun.lock", "pnpm-lock.yaml", "yarn.lock"},
				[]string{"deno.json", "deno.jsonc"})
		},
		Command: func(_ string, r BinResolver) ([]string, error) {
			bin := r.Which("typescript-language-server")
			if bin == "" {
				return nil, errBinaryUnavailable("typescript-language-server")
			}
			return []string{bin, "--stdio"}, nil
		},
	},
	"pyright": {
		ID:         "pyright",
		Extensions: []string{".py", ".pyi"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir,
				[]string{"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt", "Pipfile", "pyrightconfig.json"},
				nil)
		},
		Command: func(_ string, r BinResolver) ([]string, error) {
			bin := r.Which("pyright-langserver")
			if bin == "" {
				return nil, errBinaryUnavailable("pyright-langserver")
			}
			return []string{bin, "--stdio"}, nil
		},
	},
}

// BuiltinIDs returns the set of built-in server ids for config validation
// (config.ParseLSP treats these as not requiring an `extensions` field).
func BuiltinIDs() map[string]bool {
	out := make(map[string]bool, len(Servers))
	for id := range Servers {
		out[id] = true
	}
	return out
}

// matchesExtension reports whether def handles the given file extension.
func (d ServerDef) matchesExtension(ext string) bool {
	for _, e := range d.Extensions {
		if e == ext {
			return true
		}
	}
	return false
}
