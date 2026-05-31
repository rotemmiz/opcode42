package resource

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
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

// LoadSkills scans {skill,skills}/**/SKILL.md under each config dir (the global
// ~/.config/opencode and the project's .opencode dirs), parses the frontmatter,
// and returns the skills sorted by name. The markdown body is the content;
// duplicate names keep the first (nearest) one. opencode also bundles built-in
// skills and scans .claude/.agents external dirs — those are a logged divergence.
func LoadSkills(dir string) []Skill {
	byName := map[string]Skill{}
	for _, cd := range ConfigDirs(dir) {
		for _, file := range globSkills(cd) {
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
			// ConfigDirs is global→nearest, so last-wins makes a project skill
			// override a same-named global one (matching opencode and the
			// agent/command loaders).
			byName[fm.Name] = Skill{
				Name: fm.Name, Description: fm.Description, Location: file, Content: body,
			}
		}
	}
	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// globSkills returns the SKILL.md files under {skill,skills}/ within configDir.
func globSkills(configDir string) []string {
	var files []string
	for _, root := range []string{"skill", "skills"} {
		matches, err := doublestar.Glob(os.DirFS(configDir), root+"/**/SKILL.md")
		if err != nil {
			continue
		}
		for _, m := range matches {
			files = append(files, filepath.Join(configDir, filepath.FromSlash(m)))
		}
	}
	sort.Strings(files)
	return files
}

// SkillContent returns the content of the named skill for dir, or false when no
// such skill exists (backs the `skill` tool).
func SkillContent(dir, name string) (string, bool) {
	for _, s := range LoadSkills(dir) {
		if s.Name == name {
			return s.Content, true
		}
	}
	return "", false
}
