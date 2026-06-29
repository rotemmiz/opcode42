package server

import (
	"fmt"

	"github.com/rotemmiz/opcode42/internal/resource"
)

// skillResolver implements tool.SkillSource for the `skill` tool: it loads the
// named skill's content from the request directory's .opencode skills.
type skillResolver struct{ directory string }

func (s skillResolver) Load(name string) (string, error) {
	if content, ok := resource.SkillContent(s.directory, loadConfig(s.directory), name); ok {
		return content, nil
	}
	return "", fmt.Errorf("skill %q not found", name)
}
