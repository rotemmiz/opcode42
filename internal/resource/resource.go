// Package resource loads the opencode-compatible community resources the daemon
// serves: agents (.opencode/agent(s)/**/*.md), commands (.opencode/command(s)/
// **/*.md), and the provider list (models.dev catalog + config overlay + auth
// detection). It mirrors opencode's loaders (packages/opencode/src/config/
// agent.ts, command.ts; provider/provider.ts) closely enough for the same
// .opencode/ directory to surface the same agents/commands. Skills, instructions,
// remote config, and the full provider-catalog codegen from plan 04 are out of
// scope for this slice.
package resource

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rotemmiz/opcode42/internal/worktree"
)

// ConfigDirs returns the directories searched for .opencode resources, in
// low→high merge priority: the global config home first, then each ancestor's
// .opencode from the worktree root down to dir (so the nearest directory wins).
// It mirrors opencode's configDirectories walk (config.ts §2.6), minus the
// home-level and remote layers not needed for agent/command loading.
func ConfigDirs(dir string) []string {
	var dirs []string
	if home := configHome(); home != "" {
		dirs = append(dirs, home)
	}
	if dir == "" {
		return dirs
	}
	root := worktree.Root(dir)
	for _, anc := range ancestors(root, dir) {
		dirs = append(dirs, filepath.Join(anc, ".opencode"))
	}
	return dirs
}

// ancestors lists root, …, dir inclusive (parent-most first). When dir is not
// under root (unexpected), it returns just dir.
func ancestors(root, dir string) []string {
	root = filepath.Clean(root)
	dir = filepath.Clean(dir)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return []string{dir}
	}
	out := []string{root}
	if rel == "." {
		return out
	}
	cur := root
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, seg)
		out = append(out, cur)
	}
	return out
}

// configHome returns ~/.config/opencode, honoring XDG_CONFIG_HOME (opencode
// uses the same path; see internal/config.configHome).
func configHome() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "opencode")
}

// splitFrontmatter splits a markdown file into its YAML frontmatter bytes and
// the body. A file that does not open with a "---" fence has no frontmatter, so
// the whole content is the body (matching gray-matter's behavior).
func splitFrontmatter(data []byte) (yamlBytes []byte, body string) {
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return nil, strings.TrimSpace(s)
	}
	// The opening fence is "---" on its own line.
	rest := s[3:]
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return nil, strings.TrimSpace(s) // "---foo" is not a fence
	}
	// Find the closing fence: a line that is exactly "---".
	lines := strings.Split(rest, "\n")
	var fm []string
	closed := false
	for i, ln := range lines {
		if i == 0 {
			continue // the newline right after the opening fence
		}
		ln = strings.TrimRight(ln, "\r")
		if ln == "---" {
			body = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			closed = true
			break
		}
		fm = append(fm, ln)
	}
	if !closed {
		return nil, strings.TrimSpace(s) // unterminated fence: treat as body
	}
	return []byte(strings.Join(fm, "\n")), body
}

// entryName derives a resource's name from its path relative to a config dir:
// strip a leading prefix (e.g. "agent/"), then drop the file extension.
// E.g. "agent/foo/bar.md" → "foo/bar" (config/entry-name.ts).
func entryName(rel string, prefixes []string) string {
	rel = filepath.ToSlash(rel)
	for _, p := range prefixes {
		if strings.HasPrefix(rel, p) {
			rel = rel[len(p):]
			break
		}
	}
	return strings.TrimSuffix(rel, filepath.Ext(rel))
}
