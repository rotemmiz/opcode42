package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultReadLimit caps how many lines Read returns when no limit is given.
const defaultReadLimit = 2000

// Read returns a file's contents with 1-based line numbers, optionally windowed.
type Read struct{}

// Info describes the read tool.
func (Read) Info() Info {
	return Info{
		ID:          "read",
		Description: "Read a file's contents (optionally a line window), returned with line numbers.",
		Parameters: obj(map[string]any{
			"filePath": strProp("Path to the file (absolute or relative to the working directory)."),
			"offset":   numProp("0-based line to start from."),
			"limit":    numProp("Maximum number of lines to return."),
		}, "filePath"),
	}
}

type readParams struct {
	FilePath string `json:"filePath"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

// Run reads the file window and formats it as "<line>\t<text>" lines.
func (Read) Run(_ context.Context, input map[string]any, tctx Context) (Result, error) {
	var p readParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if p.FilePath == "" {
		return Result{}, fmt.Errorf("read: filePath is required")
	}
	path := resolve(tctx, p.FilePath)
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read: %w", err)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = defaultReadLimit
	}
	lines := strings.Split(string(data), "\n")
	var b strings.Builder
	end := p.Offset + limit
	if end > len(lines) {
		end = len(lines)
	}
	for i := p.Offset; i < end; i++ {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, lines[i])
	}
	return Result{Title: relTitle(tctx, path), Output: b.String(),
		Metadata: map[string]any{"path": path, "lines": len(lines)}}, nil
}

// Write creates or overwrites a file, making parent directories as needed.
type Write struct{}

// Info describes the write tool.
func (Write) Info() Info {
	return Info{
		ID:          "write",
		Description: "Write content to a file, creating it (and parent directories) or overwriting it.",
		Parameters: obj(map[string]any{
			"filePath": strProp("Path to the file (absolute or relative to the working directory)."),
			"content":  strProp("The full content to write."),
		}, "filePath", "content"),
	}
}

type writeParams struct {
	FilePath string `json:"filePath"`
	Content  string `json:"content"`
}

// Run writes the file.
func (Write) Run(_ context.Context, input map[string]any, tctx Context) (Result, error) {
	var p writeParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if p.FilePath == "" {
		return Result{}, fmt.Errorf("write: filePath is required")
	}
	path := resolve(tctx, p.FilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	if err := os.WriteFile(path, []byte(p.Content), 0o644); err != nil {
		return Result{}, fmt.Errorf("write: %w", err)
	}
	return Result{Title: relTitle(tctx, path),
		Output:   fmt.Sprintf("Wrote %d bytes to %s", len(p.Content), path),
		Metadata: map[string]any{"path": path, "bytes": len(p.Content)}}, nil
}

// Edit replaces an exact substring in a file.
type Edit struct{}

// Info describes the edit tool.
func (Edit) Info() Info {
	return Info{
		ID:          "edit",
		Description: "Replace an exact string in a file. Fails if oldString is absent, or not unique unless replaceAll.",
		Parameters: obj(map[string]any{
			"filePath":   strProp("Path to the file."),
			"oldString":  strProp("The exact text to replace."),
			"newString":  strProp("The replacement text."),
			"replaceAll": boolProp("Replace all occurrences instead of requiring a unique match."),
		}, "filePath", "oldString", "newString"),
	}
}

type editParams struct {
	FilePath   string `json:"filePath"`
	OldString  string `json:"oldString"`
	NewString  string `json:"newString"`
	ReplaceAll bool   `json:"replaceAll"`
}

// Run performs the search-replace edit.
func (Edit) Run(_ context.Context, input map[string]any, tctx Context) (Result, error) {
	var p editParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if p.FilePath == "" || p.OldString == "" {
		return Result{}, fmt.Errorf("edit: filePath and oldString are required")
	}
	path := resolve(tctx, p.FilePath)
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("edit: %w", err)
	}
	content := string(data)
	count := strings.Count(content, p.OldString)
	if count == 0 {
		return Result{}, fmt.Errorf("edit: oldString not found in %s", path)
	}
	if count > 1 && !p.ReplaceAll {
		return Result{}, fmt.Errorf("edit: oldString matches %d times; pass replaceAll or provide a unique string", count)
	}
	var updated string
	if p.ReplaceAll {
		updated = strings.ReplaceAll(content, p.OldString, p.NewString)
	} else {
		updated = strings.Replace(content, p.OldString, p.NewString, 1)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return Result{}, fmt.Errorf("edit: %w", err)
	}
	return Result{Title: relTitle(tctx, path),
		Output:   fmt.Sprintf("Replaced %d occurrence(s) in %s", replaced(count, p.ReplaceAll), path),
		Metadata: map[string]any{"path": path, "replacements": replaced(count, p.ReplaceAll)}}, nil
}

func replaced(count int, all bool) int {
	if all {
		return count
	}
	return 1
}

func relTitle(tctx Context, path string) string {
	if tctx.Directory == "" {
		return path
	}
	if rel, err := filepath.Rel(tctx.Directory, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}
