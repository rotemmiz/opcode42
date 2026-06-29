package resource

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/rotemmiz/opcode42/internal/worktree"
)

// instructionFilenames is opencode's priority list of project rules files
// (session/instruction.ts:14-18). CONTEXT.md is deprecated but still read.
var instructionFilenames = []string{"AGENTS.md", "CLAUDE.md", "CONTEXT.md"}

// SystemInstructions resolves the project/global rules files and config
// instructions into formatted system-prompt blocks, mirroring opencode's
// Instruction.system (instruction.ts:109-168):
//
//  1. global: ~/.config/opencode/AGENTS.md, then ~/.claude/CLAUDE.md — the first
//     existing one wins.
//  2. project (unless OPENCODE_DISABLE_PROJECT_CONFIG): for each of
//     AGENTS.md/CLAUDE.md/CONTEXT.md, findUp from dir to the worktree root; the
//     first filename with any match wins and all its matches are added.
//  3. config `instructions[]`: local paths (absolute or ~/) are globbed; http(s)
//     URLs are skipped (remote fetch is a logged divergence).
//
// Each resolved file becomes "Instructions from: <path>\n<content>".
func SystemInstructions(dir string, cfg map[string]any) []string {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			paths = append(paths, abs)
		}
	}

	for _, gf := range globalInstructionFiles() {
		if fileExists(gf) {
			add(gf)
			break
		}
	}

	if os.Getenv("OPENCODE_DISABLE_PROJECT_CONFIG") == "" && dir != "" {
		root := worktree.Root(dir)
		for _, name := range instructionFilenames {
			if matches := findUp(name, dir, root); len(matches) > 0 {
				for _, m := range matches {
					add(m)
				}
				break
			}
		}
	}

	for _, raw := range configInstructions(cfg) {
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
			continue // remote instructions not fetched (known divergence)
		}
		inst := raw
		if strings.HasPrefix(raw, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				inst = filepath.Join(home, raw[2:])
			}
		}
		for _, m := range globInstruction(inst, dir) {
			add(m)
		}
	}

	out := make([]string, 0, len(paths))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil || len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		out = append(out, "Instructions from: "+p+"\n"+string(b))
	}
	return out
}

// globalInstructionFiles are the global rules candidates, in priority order.
func globalInstructionFiles() []string {
	files := []string{filepath.Join(configHome(), "AGENTS.md")}
	if home, err := os.UserHomeDir(); err == nil {
		files = append(files, filepath.Join(home, ".claude", "CLAUDE.md"))
	}
	return files
}

// findUp returns every existing <ancestor>/name from dir up to root (inclusive),
// nearest first.
func findUp(name, dir, root string) []string {
	ancs := ancestors(root, dir) // root → dir
	var out []string
	for i := len(ancs) - 1; i >= 0; i-- { // reverse: nearest (dir) first
		p := filepath.Join(ancs[i], name)
		if fileExists(p) {
			out = append(out, p)
		}
	}
	return out
}

// globInstruction resolves a config instruction path to existing files. An
// absolute path globs its basename within its directory; a relative path is
// globbed at every ancestor from dir up to the worktree root (nearest first),
// matching opencode's globUp (filesystem.ts:157-171).
func globInstruction(instruction, dir string) []string {
	if filepath.IsAbs(instruction) {
		return globIn(filepath.Dir(instruction), filepath.Base(instruction))
	}
	root := worktree.Root(dir)
	ancs := ancestors(root, dir)
	var out []string
	for i := len(ancs) - 1; i >= 0; i-- { // dir → root (nearest first)
		out = append(out, globIn(ancs[i], instruction)...)
	}
	return out
}

// globIn returns the existing files matching pattern within base.
func globIn(base, pattern string) []string {
	matches, err := doublestar.Glob(os.DirFS(base), filepath.ToSlash(pattern))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		full := filepath.Join(base, filepath.FromSlash(m))
		if fileExists(full) {
			out = append(out, full)
		}
	}
	return out
}

// configInstructions returns the merged config `instructions` array as strings.
func configInstructions(cfg map[string]any) []string {
	arr, ok := cfg["instructions"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
