package resource

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rotemmiz/opcode42/internal/worktree"
)

// Skill is the wire shape served by GET /skill (openapi). Name, Location, and
// Content are required; Description is optional.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location"`
	Content     string `json:"content"`
}

// skillFrontmatter is the SKILL.md frontmatter (skill/index.ts:52): only name
// (required) and description. A file without a valid name is skipped.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// LoadSkills resolves the project's skills, sorted by name, mirroring opencode's
// scan order (skill/index.ts:185-225) with last-wins dedupe by name:
//
//  1. external .claude/.agents dirs — `skills/**/SKILL.md` — global (~) then
//     project (found-up to the worktree root);
//  2. .opencode config dirs — `{skill,skills}/**/SKILL.md` — global
//     ~/.config/opencode then project (nearest wins);
//  3. config `skills.paths[]` — `**/SKILL.md` — extra local dirs.
//
// The markdown body is the content. Built-in (embedded) skills and remote
// `skills.urls[]` discovery are not yet implemented (logged divergence).
func LoadSkills(dir string, cfg map[string]any) []Skill {
	byName := map[string]Skill{}
	consume := func(files []string) {
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			yamlBytes, body := splitFrontmatter(data)
			var fm skillFrontmatter
			if len(yamlBytes) > 0 {
				_ = yaml.Unmarshal(yamlBytes, &fm)
			}
			if fm.Name == "" {
				continue // a skill without a name is skipped (skill/index.ts:124)
			}
			byName[fm.Name] = Skill{Name: fm.Name, Description: fm.Description, Location: file, Content: body}
		}
	}

	for _, root := range externalSkillRoots(dir) {
		consume(globIn(root, "skills/**/SKILL.md"))
	}
	for _, cd := range ConfigDirs(dir) {
		consume(globSkills(cd))
	}
	for _, p := range skillPaths(dir, cfg) {
		consume(globIn(p, "**/SKILL.md"))
	}

	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// externalSkillRoots returns the existing .claude/.agents dirs to scan for
// `skills/**/SKILL.md`: the global ~/.claude and ~/.agents, then each found-up
// the project tree from dir to the worktree root (skill/index.ts:185-201).
func externalSkillRoots(dir string) []string {
	names := []string{".claude", ".agents"}
	var roots []string
	if home, err := os.UserHomeDir(); err == nil {
		for _, n := range names {
			if p := filepath.Join(home, n); isDir(p) {
				roots = append(roots, p)
			}
		}
	}
	if dir != "" {
		ancs := ancestors(worktree.Root(dir), dir)
		for i := len(ancs) - 1; i >= 0; i-- { // nearest first
			for _, n := range names {
				if p := filepath.Join(ancs[i], n); isDir(p) {
					roots = append(roots, p)
				}
			}
		}
	}
	return roots
}

// skillPaths resolves config `skills.paths[]` to existing dirs (~/ expanded,
// relative entries resolved against dir).
func skillPaths(dir string, cfg map[string]any) []string {
	skills, ok := cfg["skills"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := skills["paths"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, v := range raw {
		p, ok := v.(string)
		if !ok || p == "" {
			continue
		}
		if strings.HasPrefix(p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				p = filepath.Join(home, p[2:])
			}
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if isDir(p) {
			out = append(out, p)
		}
	}
	return out
}

// globSkills returns the SKILL.md files under {skill,skills}/ within configDir.
func globSkills(configDir string) []string {
	var files []string
	for _, root := range []string{"skill", "skills"} {
		files = append(files, globIn(configDir, root+"/**/SKILL.md")...)
	}
	sort.Strings(files)
	return files
}

// SkillContent returns the content of the named skill for dir, or false when no
// such skill exists (backs the `skill` tool).
func SkillContent(dir string, cfg map[string]any, name string) (string, bool) {
	for _, s := range LoadSkills(dir, cfg) {
		if s.Name == name {
			return s.Content, true
		}
	}
	return "", false
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
