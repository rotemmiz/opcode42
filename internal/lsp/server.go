// Package lsp is Opcode42's per-instance LSP integration. It ports opencode's
// built-in server table and lazy-spawn lifecycle. gopls is auto-installable via
// `go install`; the remaining built-ins are resolved from PATH (the
// download/npm/dotnet-tool auto-install paths opencode has for some servers are
// a follow-up — see SERVERS note in server.go). The JSON-RPC client and query
// operations live in client.go / query.go (M3-4/M3-5). Mirrors opencode's
// lsp/server.ts + lsp/lsp.ts.
//
// Process-group handling (Setpgid / Getpgid / Kill) is split across
// procgroup_unix.go and procgroup_windows.go so the package compiles on Windows
// (where there are no POSIX process groups). POSIX behavior is unchanged.
package lsp

import (
	"os"
	"path/filepath"
	"strings"
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
	// Initialization is the server's LSP initializationOptions /
	// didChangeConfiguration settings (nil for the built-ins, populated for custom
	// config-defined servers — lsp.ts:181). Surfaced to workspace/configuration.
	Initialization map[string]any
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

// denoRoot resolves the Deno project root: the nearest deno.json/deno.jsonc, or
// "" when none exists (Deno only attaches to actual Deno projects, unlike most
// servers which fall back to the instance directory). Ports lsp/server.ts:67-78
// (Deno.root returns undefined when no deno.json is found).
func denoRoot(file, instanceDir string) string {
	if found := walkUpFind(filepath.Dir(file), instanceDir, []string{"deno.json", "deno.jsonc"}); found != "" {
		return filepath.Dir(found)
	}
	return ""
}

// rustRoot resolves the Rust workspace root: starting from the nearest
// Cargo.toml/Cargo.lock crate root, it walks up looking for a Cargo.toml that
// declares [workspace]; if found, that directory is the root, otherwise the
// crate root is used. Ports lsp/server.ts:919-949 (Rust.root). Returns "" when
// no Cargo manifest is found at all.
func rustRoot(file, instanceDir string) string {
	crateMarker := walkUpFind(filepath.Dir(file), instanceDir, []string{"Cargo.toml", "Cargo.lock"})
	if crateMarker == "" {
		return ""
	}
	crateRoot := filepath.Dir(crateMarker)
	cur := crateRoot
	for {
		if data, err := os.ReadFile(filepath.Join(cur, "Cargo.toml")); err == nil {
			if strings.Contains(string(data), "[workspace]") {
				return cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
		// Don't walk above the instance directory (opencode stops at ctx.worktree).
		if rel, err := filepath.Rel(instanceDir, cur); err != nil || strings.HasPrefix(rel, "..") {
			break
		}
	}
	return crateRoot
}

// whichCommand builds a Command func that resolves binary from PATH and appends
// args. When the binary is absent the server is marked broken (opencode logs a
// "please install" message and skips spawning). This is the shared shape for the
// PATH-resolved built-ins.
func whichCommand(binary string, args ...string) func(string, BinResolver) ([]string, error) {
	return func(_ string, r BinResolver) ([]string, error) {
		bin := r.Which(binary)
		if bin == "" {
			return nil, errBinaryUnavailable(binary)
		}
		return append([]string{bin}, args...), nil
	}
}

// Servers is the built-in LSP server table, ported from opencode's lsp/server.ts.
// gopls is auto-installable; the rest resolve their binary from PATH. Servers in
// opencode that require download/build, an npm node_modules resolution, or a
// dotnet-tool install (eslint, vue, oxlint, biome, ty, elixir-ls, zls, csharp,
// razor, fsharp, sourcekit, svelte, astro, jdtls, kotlin-ls, yaml-ls, lua-ls,
// texlab, tinymist, haskell, julials, sourcekit) are follow-ups; their spawn
// machinery does not fit the PATH/auto-install resolver yet. Keyed by server id.
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
	// deno — lsp/server.ts:66-92. Attaches only inside a Deno project (deno.json /
	// deno.jsonc); spawns `deno lsp`.
	"deno": {
		ID:         "deno",
		Extensions: []string{".ts", ".tsx", ".js", ".jsx", ".mjs"},
		Root:       denoRoot,
		Command:    whichCommand("deno", "lsp"),
	},
	// ruby-lsp — lsp/server.ts:384-420. Root: Gemfile; spawns `rubocop --lsp`.
	"ruby-lsp": {
		ID:         "ruby-lsp",
		Extensions: []string{".rb", ".rake", ".gemspec", ".ru"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir, []string{"Gemfile"}, nil)
		},
		Command: whichCommand("rubocop", "--lsp"),
	},
	// rust — lsp/server.ts:918-963. Root walks to the Cargo [workspace] root;
	// spawns `rust-analyzer`.
	"rust": {
		ID:         "rust",
		Extensions: []string{".rs"},
		Root:       rustRoot,
		Command:    whichCommand("rust-analyzer"),
	},
	// clangd — lsp/server.ts:965-1011. Root: compile_commands.json /
	// compile_flags.txt / .clangd; spawns clangd with background-index + clang-tidy.
	"clangd": {
		ID:         "clangd",
		Extensions: []string{".c", ".cpp", ".cc", ".cxx", ".c++", ".h", ".hpp", ".hh", ".hxx", ".h++"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir,
				[]string{"compile_commands.json", "compile_flags.txt", ".clangd"}, nil)
		},
		Command: whichCommand("clangd", "--background-index", "--clang-tidy"),
	},
	// dart — lsp/server.ts:1612-1628. Root: pubspec.yaml / analysis_options.yaml;
	// spawns `dart language-server --lsp`.
	"dart": {
		ID:         "dart",
		Extensions: []string{".dart"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir, []string{"pubspec.yaml", "analysis_options.yaml"}, nil)
		},
		Command: whichCommand("dart", "language-server", "--lsp"),
	},
	// php intelephense — lsp/server.ts:1563-1592. Root: composer.json /
	// composer.lock / .php-version; spawns `intelephense --stdio`. Telemetry off.
	"php intelephense": {
		ID:         "php intelephense",
		Extensions: []string{".php"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir, []string{"composer.json", "composer.lock", ".php-version"}, nil)
		},
		Command:        whichCommand("intelephense", "--stdio"),
		Initialization: map[string]any{"telemetry": map[string]any{"enabled": false}},
	},
	// prisma — lsp/server.ts:1594-1610. Root: schema.prisma / prisma/schema.prisma
	// / prisma (excl. package.json); spawns `prisma language-server`.
	"prisma": {
		ID:         "prisma",
		Extensions: []string{".prisma"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir,
				[]string{"schema.prisma", "prisma/schema.prisma", "prisma"}, []string{"package.json"})
		},
		Command: whichCommand("prisma", "language-server"),
	},
	// ocaml-lsp — lsp/server.ts:1630-1646. Root: dune-project / dune-workspace /
	// .merlin / opam; spawns `ocamllsp`.
	"ocaml-lsp": {
		ID:         "ocaml-lsp",
		Extensions: []string{".ml", ".mli"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir, []string{"dune-project", "dune-workspace", ".merlin", "opam"}, nil)
		},
		Command: whichCommand("ocamllsp"),
	},
	// bash — lsp/server.ts:1647-1671. Root: always the instance directory; spawns
	// `bash-language-server start`.
	"bash": {
		ID:         "bash",
		Extensions: []string{".sh", ".bash", ".zsh", ".ksh"},
		Root:       func(_, instanceDir string) string { return instanceDir },
		Command:    whichCommand("bash-language-server", "start"),
	},
	// terraform — lsp/server.ts:1673-1752. Root: .terraform.lock.hcl /
	// terraform.tfstate (the *.tf glob marker never matches via up(), so it falls
	// back to the instance directory, matching opencode); spawns `terraform-ls serve`.
	"terraform": {
		ID:         "terraform",
		Extensions: []string{".tf", ".tfvars"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir, []string{".terraform.lock.hcl", "terraform.tfstate", "*.tf"}, nil)
		},
		Command: whichCommand("terraform-ls", "serve"),
		Initialization: map[string]any{
			"experimentalFeatures": map[string]any{
				"prefillRequiredFields": true,
				"validateOnSave":        true,
			},
		},
	},
	// dockerfile — lsp/server.ts:1842-1866. Root: always the instance directory;
	// spawns `docker-langserver --stdio`.
	"dockerfile": {
		ID:         "dockerfile",
		Extensions: []string{".dockerfile", "Dockerfile"},
		Root:       func(_, instanceDir string) string { return instanceDir },
		Command:    whichCommand("docker-langserver", "--stdio"),
	},
	// gleam — lsp/server.ts:1868-1884. Root: gleam.toml; spawns `gleam lsp`.
	"gleam": {
		ID:         "gleam",
		Extensions: []string{".gleam"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir, []string{"gleam.toml"}, nil)
		},
		Command: whichCommand("gleam", "lsp"),
	},
	// clojure-lsp — lsp/server.ts:1886-1905. Root: deps.edn / project.clj /
	// shadow-cljs.edn / bb.edn / build.boot; spawns `clojure-lsp listen`.
	"clojure-lsp": {
		ID:         "clojure-lsp",
		Extensions: []string{".clj", ".cljs", ".cljc", ".edn"},
		Root: func(file, instanceDir string) string {
			return nearestRoot(file, instanceDir,
				[]string{"deps.edn", "project.clj", "shadow-cljs.edn", "bb.edn", "build.boot"}, nil)
		},
		Command: whichCommand("clojure-lsp", "listen"),
	},
	// nixd — lsp/server.ts:1907-1936. Root: flake.nix (else instance directory);
	// spawns `nixd`. opencode also falls back to the git worktree root; Opcode42 has
	// no worktree distinct from the instance dir here, so flake.nix-or-instance-dir
	// matches its effective behavior.
	"nixd": {
		ID:         "nixd",
		Extensions: []string{".nix"},
		Root: func(file, instanceDir string) string {
			if found := walkUpFind(filepath.Dir(file), instanceDir, []string{"flake.nix"}); found != "" {
				if dir := filepath.Dir(found); dir != instanceDir {
					return dir
				}
			}
			return instanceDir
		},
		Command: whichCommand("nixd"),
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
