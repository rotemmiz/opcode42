package resource

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"

	"github.com/rotemmiz/opcode42/internal/engine/permission"
)

// Agent is the wire shape served by GET /agent (openapi Agent). Permission and
// Options are always serialized (required by the spec); the rest are optional.
type Agent struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Mode        string             `json:"mode"`
	Native      bool               `json:"native,omitempty"`
	Hidden      bool               `json:"hidden,omitempty"`
	TopP        *float64           `json:"topP,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Color       string             `json:"color,omitempty"`
	Permission  permission.Ruleset `json:"permission"`
	Model       *AgentModel        `json:"model,omitempty"`
	Variant     string             `json:"variant,omitempty"`
	Prompt      string             `json:"prompt,omitempty"`
	Options     map[string]any     `json:"options"`
	Steps       *int               `json:"steps,omitempty"`
}

// AgentModel is the resolved provider/model for an agent.
type AgentModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// allowAll is the default permission for built-in agents (the build agent runs
// every tool; finer rules come from config/agent frontmatter).
func allowAll() permission.Ruleset {
	return permission.Ruleset{{Permission: "*", Pattern: "*", Action: permission.ActionAllow}}
}

// builtinAgents are the agents opencode ships (agent/agent.ts:129-281). Opcode42
// serves their metadata so the TUI's switcher matches; the full system prompts
// live with the engine, not here.
func builtinAgents() map[string]*Agent {
	return map[string]*Agent{
		"build": {
			Name: "build", Mode: "primary", Native: true,
			Description: "The default agent. Executes tools based on configured permissions.",
			Permission:  allowAll(), Options: map[string]any{},
		},
		"plan": {
			Name: "plan", Mode: "primary", Native: true,
			Description: "Read-only planning agent that proposes changes without editing.",
			Permission:  allowAll(), Options: map[string]any{},
		},
		"general": {
			Name: "general", Mode: "subagent", Native: true,
			Description: "General-purpose agent for researching complex questions and multi-step tasks.",
			Permission:  allowAll(), Options: map[string]any{},
		},
		"explore": {
			Name: "explore", Mode: "subagent", Native: true,
			Description: "Read-only search agent for broad fan-out searches across the codebase.",
			Permission:  allowAll(), Options: map[string]any{},
		},
		"compaction": {
			Name: "compaction", Mode: "primary", Native: true, Hidden: true,
			Permission: allowAll(), Options: map[string]any{},
		},
		"title": {
			Name: "title", Mode: "primary", Native: true, Hidden: true,
			Permission: allowAll(), Options: map[string]any{},
		},
		"summary": {
			Name: "summary", Mode: "primary", Native: true, Hidden: true,
			Permission: allowAll(), Options: map[string]any{},
		},
	}
}

// LoadAgents returns the agents for dir: the built-ins overlaid with config
// (`agent` key) and the .opencode/agent(s)/**/*.md files (nearest dir wins),
// sorted by name. cfg is the merged opencode config (config.Load).
func LoadAgents(dir string, cfg map[string]any) []Agent {
	agents := builtinAgents()
	mergeConfigAgents(agents, cfg)
	for _, cd := range ConfigDirs(dir) {
		for name, a := range loadAgentDir(cd) {
			applyAgent(agents, name, a)
		}
	}
	out := make([]Agent, 0, len(agents))
	for _, a := range agents {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// applyAgent merges a loaded/config agent over any existing one of the same
// name; a disabled entry removes it (agent.ts:283-310).
func applyAgent(agents map[string]*Agent, name string, in *Agent) {
	if in == nil {
		delete(agents, name)
		return
	}
	in.Name = name
	if in.Options == nil {
		in.Options = map[string]any{}
	}
	if in.Permission == nil {
		in.Permission = permission.Ruleset{}
	}
	if in.Mode == "" {
		in.Mode = "all"
	}
	agents[name] = in
}

// agentFrontmatter is the YAML frontmatter of an .opencode/agent/*.md file
// (config/agent.ts:21-50).
type agentFrontmatter struct {
	Model       string          `yaml:"model"`
	Variant     string          `yaml:"variant"`
	Temperature *float64        `yaml:"temperature"`
	TopP        *float64        `yaml:"top_p"`
	Prompt      string          `yaml:"prompt"`
	Tools       map[string]bool `yaml:"tools"`
	Disable     bool            `yaml:"disable"`
	Description string          `yaml:"description"`
	Mode        string          `yaml:"mode"`
	Hidden      bool            `yaml:"hidden"`
	Color       string          `yaml:"color"`
	Steps       *int            `yaml:"steps"`
	MaxSteps    *int            `yaml:"maxSteps"`
	Options     map[string]any  `yaml:"options"`
	// Permission is the modern replacement for tools: a single action string
	// (shorthand for {"*": action}) or a {key: action | {pattern: action}} map
	// (config/permission.ts). Decoded as `any` because of the union shape.
	Permission any `yaml:"permission"`
}

// loadAgentDir parses every {agent,agents}/**/*.md under configDir into a
// name→Agent map (a nil value means "disabled", to be removed on merge).
func loadAgentDir(configDir string) map[string]*Agent {
	out := map[string]*Agent{}
	for _, file := range globMarkdown(configDir, []string{"agent", "agents"}) {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(configDir, file)
		name := entryName(rel, []string{"agent/", "agents/"})
		out[name] = parseAgent(data)
	}
	return out
}

// parseAgent builds an Agent from a markdown file; returns nil when disabled.
func parseAgent(data []byte) *Agent {
	yamlBytes, body := splitFrontmatter(data)
	var fm agentFrontmatter
	if len(yamlBytes) > 0 {
		_ = yaml.Unmarshal(yamlBytes, &fm)
	}
	if fm.Disable {
		return nil
	}
	a := &Agent{
		Description: fm.Description, Mode: fm.Mode, Hidden: fm.Hidden,
		Temperature: fm.Temperature, TopP: fm.TopP, Color: fm.Color,
		Variant: fm.Variant, Options: fm.Options,
	}
	a.Steps = fm.Steps
	if a.Steps == nil {
		a.Steps = fm.MaxSteps // deprecated alias (agent.ts:98-103)
	}
	// The markdown body is the prompt; it overrides any frontmatter `prompt`
	// key (opencode spreads md.data then sets prompt: md.content; agent.ts:124).
	a.Prompt = body
	if a.Prompt == "" {
		a.Prompt = fm.Prompt
	}
	if m := parseModelRef(fm.Model); m != nil {
		a.Model = m
	}
	// tools (deprecated) is rewritten to permission rules first; an explicit
	// permission block then layers on top (last match wins in Evaluate).
	a.Permission = append(toolsToPermission(fm.Tools), parsePermissionConfig(fm.Permission)...)
	return a
}

// parsePermissionConfig translates the `permission` frontmatter (config/
// permission.ts Info) into rules. It accepts a bare action string (→ "*"),
// a {key: action} map, or a {key: {pattern: action}} map. Keys and patterns are
// emitted in sorted order for deterministic output.
func parsePermissionConfig(v any) permission.Ruleset {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if a := toAction(t); a != "" {
			return permission.Ruleset{{Permission: "*", Pattern: "*", Action: a}}
		}
	case map[string]any:
		var rs permission.Ruleset
		for _, key := range sortedKeys(t) {
			switch val := t[key].(type) {
			case string:
				if a := toAction(val); a != "" {
					rs = append(rs, permission.Rule{Permission: key, Pattern: "*", Action: a})
				}
			case map[string]any:
				for _, pat := range sortedKeys(val) {
					if a := toAction(val[pat]); a != "" {
						rs = append(rs, permission.Rule{Permission: key, Pattern: pat, Action: a})
					}
				}
			}
		}
		return rs
	}
	return nil
}

// toAction validates an action value; returns "" for anything not in the enum.
func toAction(v any) permission.Action {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	switch permission.Action(s) {
	case permission.ActionAsk, permission.ActionAllow, permission.ActionDeny:
		return permission.Action(s)
	}
	return ""
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseModelRef splits a "provider/model" reference; returns nil when empty.
func parseModelRef(s string) *AgentModel {
	if s == "" {
		return nil
	}
	if i := strings.Index(s, "/"); i >= 0 {
		return &AgentModel{ProviderID: s[:i], ModelID: s[i+1:]}
	}
	return &AgentModel{ModelID: s}
}

// toolsToPermission translates the deprecated tools:{name:bool} map into the
// permission ruleset (agent.ts:86-91): write/edit/patch fold into "edit"; other
// names pass through; true→allow, false→deny.
func toolsToPermission(tools map[string]bool) permission.Ruleset {
	if len(tools) == 0 {
		return permission.Ruleset{}
	}
	// Stable order so output is deterministic.
	names := make([]string, 0, len(tools))
	for n := range tools {
		names = append(names, n)
	}
	sort.Strings(names)
	rs := make(permission.Ruleset, 0, len(names))
	for _, n := range names {
		key := n
		switch n {
		case "write", "edit", "patch":
			key = "edit"
		}
		action := permission.ActionDeny
		if tools[n] {
			action = permission.ActionAllow
		}
		rs = append(rs, permission.Rule{Permission: key, Pattern: "*", Action: action})
	}
	return rs
}

// mergeConfigAgents overlays the config `agent` map (provider-neutral overrides)
// onto the built-ins. Only the fields the TUI/list surface are honored.
func mergeConfigAgents(agents map[string]*Agent, cfg map[string]any) {
	raw, ok := cfg["agent"].(map[string]any)
	if !ok {
		return
	}
	for name, v := range raw {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if disable, _ := entry["disable"].(bool); disable {
			delete(agents, name)
			continue
		}
		a := agents[name]
		if a == nil {
			a = &Agent{Name: name, Mode: "all", Permission: permission.Ruleset{}, Options: map[string]any{}}
		}
		if d, ok := entry["description"].(string); ok {
			a.Description = d
		}
		if m, ok := entry["mode"].(string); ok && m != "" {
			a.Mode = m
		}
		if h, ok := entry["hidden"].(bool); ok {
			a.Hidden = h
		}
		if mr, ok := entry["model"].(string); ok {
			if model := parseModelRef(mr); model != nil {
				a.Model = model
			}
		}
		agents[name] = a
	}
}

// globMarkdown returns the .md files under any of roots within configDir.
func globMarkdown(configDir string, roots []string) []string {
	var files []string
	for _, root := range roots {
		matches, err := doublestar.Glob(os.DirFS(configDir), root+"/**/*.md")
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
