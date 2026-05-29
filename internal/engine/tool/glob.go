package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// globLimit caps how many matches Glob returns.
const globLimit = 100

// Glob finds files matching a glob pattern (supports ** via doublestar).
type Glob struct{}

// Info describes the glob tool.
func (Glob) Info() Info {
	return Info{
		ID:          "glob",
		Description: "Find files matching a glob pattern (supports **), newest first.",
		Parameters: obj(map[string]any{
			"pattern": strProp("Glob pattern, e.g. **/*.go"),
			"path":    strProp("Directory to search in (defaults to the working directory)."),
		}, "pattern"),
	}
}

type globParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

// Run evaluates the glob and returns matching paths sorted by mtime (newest first).
func (Glob) Run(_ context.Context, input map[string]any, tctx Context) (Result, error) {
	var p globParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if p.Pattern == "" {
		return Result{}, fmt.Errorf("glob: pattern is required")
	}
	base := tctx.Directory
	if p.Path != "" {
		base = resolve(tctx, p.Path)
	}
	matches, err := doublestar.Glob(os.DirFS(base), p.Pattern, doublestar.WithFilesOnly())
	if err != nil {
		return Result{}, fmt.Errorf("glob: %w", err)
	}
	type entry struct {
		path  string
		mtime int64
	}
	entries := make([]entry, 0, len(matches))
	for _, m := range matches {
		abs := filepath.Join(base, m)
		var mt int64
		if info, statErr := os.Stat(abs); statErr == nil {
			mt = info.ModTime().UnixNano()
		}
		entries = append(entries, entry{path: abs, mtime: mt})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].mtime > entries[j].mtime })

	truncated := false
	if len(entries) > globLimit {
		entries = entries[:globLimit]
		truncated = true
	}
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.path)
		b.WriteByte('\n')
	}
	if truncated {
		b.WriteString("(results truncated)\n")
	}
	if len(entries) == 0 {
		b.WriteString("No files found")
	}
	return Result{Title: p.Pattern, Output: b.String(), Metadata: map[string]any{"count": len(entries)}}, nil
}
