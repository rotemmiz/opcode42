package server

import (
	"io/fs"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// findLimitDefault / findLimitMax bound the result count (openapi: 1..200,
// default 100 in opencode's File.search).
const (
	findLimitDefault = 100
	findLimitMax     = 200
)

// findSkipDirs are directories the file finder never descends into, mirroring
// the Grep tool's ignore set. opencode delegates to `rg --files`, which honors
// .gitignore; Opcode42 approximates that with a fixed ignore set plus the
// hidden-entry rule below (logged in conformance/known-divergences.json).
var findSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".opcode42": true,
}

// registerFindRoutes wires GET /find/file (the TUI's @-mention completion and
// file picker; plan 08 U9).
func registerFindRoutes(reg func(method, path string, h http.HandlerFunc)) {
	reg(http.MethodGet, "/find/file", findFileHandler())
}

// findKind selects which entries the search ranks.
type findKind int

const (
	kindAll  findKind = iota // files + directories (opencode's default)
	kindFile                 // files only
	kindDir                  // directories only
)

// findFileHandler fuzzy-searches the request directory and returns repo-relative
// paths, best matches first. Query params mirror opencode's File.search:
// query (required), limit (1..200, default 100), type (file|directory), and
// dirs (true|false). The resolved kind is type ?? (dirs==false ? file : all)
// (file/index.ts:623).
func findFileHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if _, ok := q["query"]; !ok {
			writeError(w, http.StatusBadRequest, "BadRequest", "query is required")
			return
		}
		query := q.Get("query")

		limit := findLimitDefault
		if raw := q.Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > findLimitMax {
				writeError(w, http.StatusBadRequest, "BadRequest", "limit must be between 1 and 200")
				return
			}
			limit = n
		}

		kind, ok := resolveFindKind(q.Get("type"), q.Get("dirs"))
		if !ok {
			writeError(w, http.StatusBadRequest, "BadRequest",
				"type must be file|directory and dirs must be true|false")
			return
		}

		dir := DirectoryFromContext(r.Context())
		results := findPaths(dir, strings.TrimSpace(query), kind, limit)
		writeJSON(w, http.StatusOK, results)
	}
}

// resolveFindKind maps the type/dirs query params to a kind, validating their
// enums. ok is false on an invalid value.
func resolveFindKind(typ, dirs string) (findKind, bool) {
	switch typ {
	case "file":
		return kindFile, true
	case "directory":
		return kindDir, true
	case "":
		// fall through to dirs
	default:
		return 0, false
	}
	switch dirs {
	case "false":
		return kindFile, true
	case "true", "":
		return kindAll, true
	default:
		return 0, false
	}
}

// findPaths walks dir collecting files and/or directories per kind and ranks
// them against query. Hidden entries (names starting with ".") and findSkipDirs
// are skipped, matching ripgrep's defaults. Directory results carry a trailing
// "/", as opencode's do. The result is always a non-nil slice.
func findPaths(dir, query string, kind findKind, limit int) []string {
	files, dirs := walkProject(dir)
	var items []string
	switch kind {
	case kindFile:
		items = files
	case kindDir:
		items = dirs
	default: // kindAll
		items = append(files, dirs...)
	}
	if query == "" {
		// opencode returns the leading slice for an empty query (the TUI never
		// sends one, but keep the contract: no ranking, just truncate).
		sort.Strings(items)
		return capSlice(items, limit)
	}
	return rankFuzzy(query, items, limit)
}

// walkProject returns the project's files and the set of directories that
// contain them (each with a trailing "/"), as repo-relative forward-slash paths.
func walkProject(dir string) (files, dirs []string) {
	if dir == "" {
		return nil, nil
	}
	seenDirs := map[string]bool{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip, don't abort the whole walk
		}
		if path == dir {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if findSkipDirs[name] || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		files = append(files, rel)
		for parent := pathDir(rel); parent != ""; parent = pathDir(parent) {
			if !seenDirs[parent] {
				seenDirs[parent] = true
				dirs = append(dirs, parent+"/")
			}
		}
		return nil
	})
	return files, dirs
}

// pathDir returns the parent directory of a forward-slash relative path, or ""
// when it has no parent (a top-level entry).
func pathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

func capSlice(s []string, limit int) []string {
	if s == nil {
		return []string{} // marshal as [] not null
	}
	if len(s) > limit {
		return s[:limit]
	}
	return s
}
