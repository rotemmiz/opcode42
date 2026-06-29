package tool

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// grepLimit caps how many matching lines Grep returns.
const grepLimit = 100

// skipDirs are directories Grep never descends into.
var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".opcode42": true}

// Grep searches file contents for a regular expression (pure-Go ripgrep stand-in).
type Grep struct{}

// Info describes the grep tool.
func (Grep) Info() Info {
	return Info{
		ID:          "grep",
		Description: "Search file contents for a regular expression, returning path:line:text matches.",
		Parameters: obj(map[string]any{
			"pattern": strProp("Regular expression to search for."),
			"path":    strProp("Directory to search (defaults to the working directory)."),
			"include": strProp("Optional glob to limit which files are searched, e.g. *.go"),
		}, "pattern"),
	}
}

type grepParams struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Include string `json:"include"`
}

// Run walks the tree and collects matching lines, skipping VCS/dep dirs and
// binary files, capped at grepLimit.
func (Grep) Run(ctx context.Context, input map[string]any, tctx Context) (Result, error) {
	var p grepParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if p.Pattern == "" {
		return Result{}, fmt.Errorf("grep: pattern is required")
	}
	re, err := regexp.Compile(p.Pattern)
	if err != nil {
		return Result{}, fmt.Errorf("grep: invalid pattern: %w", err)
	}
	base := tctx.Directory
	if p.Path != "" {
		base = resolve(tctx, p.Path)
	}

	var matches []string
	truncated := false
	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if p.Include != "" {
			if ok, _ := doublestar.Match(p.Include, d.Name()); !ok {
				return nil
			}
		}
		found, ferr := grepFile(re, path, base, &matches)
		if ferr == nil && found && len(matches) >= grepLimit {
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && ctx.Err() != nil {
		return Result{}, ctx.Err()
	}

	out := strings.Join(matches, "\n")
	if truncated {
		out += "\n(results truncated)"
	}
	if len(matches) == 0 {
		out = "No matches found"
	}
	return Result{Title: p.Pattern, Output: out, Metadata: map[string]any{"matches": len(matches)}}, nil
}

func grepFile(re *regexp.Regexp, path, base string, matches *[]string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false, nil // binary
	}
	rel, relErr := filepath.Rel(base, path)
	if relErr != nil {
		rel = path
	}
	found := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		if re.Match(sc.Bytes()) {
			*matches = append(*matches, fmt.Sprintf("%s:%d:%s", rel, line, sc.Text()))
			found = true
			if len(*matches) >= grepLimit {
				break
			}
		}
	}
	return found, nil
}
