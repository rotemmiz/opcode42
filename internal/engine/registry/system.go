package registry

import (
	"embed"
	"fmt"
	"runtime"
	"strings"
	"time"
)

//go:embed prompts/*.txt
var promptFS embed.FS

// systemPromptVariant maps a model id to its embedded base prompt, mirroring
// opencode's provider() routing (system.ts:19-33). Forge ships default/gpt/
// gemini/anthropic today; the Anthropic prompt is wired now so the deferred
// Anthropic provider (plan 02 addendum) is drop-in.
func systemPromptVariant(modelID string) string {
	id := strings.ToLower(modelID)
	switch {
	case strings.Contains(id, "gpt"), strings.HasPrefix(id, "o1"), strings.HasPrefix(id, "o3"):
		return "gpt"
	case strings.Contains(id, "gemini-"):
		return "gemini"
	case strings.Contains(id, "claude"):
		return "anthropic"
	default:
		return "default"
	}
}

// SystemPrompt returns the base system prompt text for a model.
func SystemPrompt(modelID string) string {
	name := systemPromptVariant(modelID)
	data, err := promptFS.ReadFile("prompts/" + name + ".txt")
	if err != nil {
		data, _ = promptFS.ReadFile("prompts/default.txt")
	}
	return strings.TrimSpace(string(data))
}

// EnvInfo describes the runtime environment injected into the system prompt.
type EnvInfo struct {
	ModelID       string
	WorkingDir    string
	WorkspaceRoot string
	IsGit         bool
	Now           time.Time
}

// Environment builds the <env> system block (system.ts:47-61).
func Environment(info EnvInfo) string {
	now := info.Now
	if now.IsZero() {
		now = time.Now()
	}
	var b strings.Builder
	b.WriteString("<env>\n")
	fmt.Fprintf(&b, "Working directory: %s\n", info.WorkingDir)
	if info.WorkspaceRoot != "" && info.WorkspaceRoot != info.WorkingDir {
		fmt.Fprintf(&b, "Workspace root: %s\n", info.WorkspaceRoot)
	}
	fmt.Fprintf(&b, "Platform: %s\n", runtime.GOOS)
	fmt.Fprintf(&b, "Is a git repository: %t\n", info.IsGit)
	fmt.Fprintf(&b, "Today's date: %s\n", now.Format("2006-01-02"))
	if info.ModelID != "" {
		fmt.Fprintf(&b, "Model: %s\n", info.ModelID)
	}
	b.WriteString("</env>")
	return b.String()
}

// BuildSystem assembles the system prompt list: base prompt, then env, then any
// instruction overrides (AGENTS.md / agent prompt), dropping empties. Order
// matches opencode's final assembly (prompt.ts:1435).
func BuildSystem(modelID string, env EnvInfo, instructions ...string) []string {
	out := []string{SystemPrompt(modelID), Environment(env)}
	for _, in := range instructions {
		if strings.TrimSpace(in) != "" {
			out = append(out, in)
		}
	}
	return out
}
