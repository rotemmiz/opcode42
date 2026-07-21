package tui

import (
	"context"
	"encoding/json"
	"sort"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Read-only resource dialogs (plan 08a §G): configured MCP servers (GET /mcp)
// and available skills (GET /skill). Plus the static help/keybindings table (§E).

// mcpItem is one configured MCP server (GET /mcp is a name→config map).
type mcpItem struct {
	Name   string
	Status string // best-effort: a status/state/enabled field if the daemon reports one
}

// skillItem is one available skill (GET /skill items: name/description/...).
type skillItem struct {
	Name        string
	Description string
}

type (
	mcpLoadedMsg struct {
		items []mcpItem
		err   error
	}
	skillsLoadedMsg struct {
		items []skillItem
		err   error
	}
)

// loadMCPCmd fetches the configured MCP servers.
func loadMCPCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		// GET /mcp is a loose map: { "<name>": { ... } }. List names; surface a
		// status-ish field if one is present, else leave it blank.
		var raw map[string]json.RawMessage
		if err := c.GetJSON(ctx, "/mcp", &raw); err != nil {
			return mcpLoadedMsg{err: err}
		}
		items := make([]mcpItem, 0, len(raw))
		for name, cfg := range raw {
			items = append(items, mcpItem{Name: name, Status: mcpStatus(cfg)})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		return mcpLoadedMsg{items: items}
	}
}

// mcpStatus best-effort pulls a human status from a server config blob.
func mcpStatus(cfg json.RawMessage) string {
	var m struct {
		Status  string `json:"status"`
		State   string `json:"state"`
		Type    string `json:"type"`
		Enabled *bool  `json:"enabled"`
	}
	_ = json.Unmarshal(cfg, &m)
	switch {
	case m.Status != "":
		return m.Status
	case m.State != "":
		return m.State
	case m.Enabled != nil && !*m.Enabled:
		return "disabled"
	case m.Type != "":
		return m.Type
	default:
		return ""
	}
}

// loadSkillsCmd fetches the available skills.
func loadSkillsCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		var arr []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := c.GetJSON(ctx, "/skill", &arr); err != nil {
			return skillsLoadedMsg{err: err}
		}
		items := make([]skillItem, 0, len(arr))
		for _, s := range arr {
			items = append(items, skillItem{Name: s.Name, Description: s.Description})
		}
		return skillsLoadedMsg{items: items}
	}
}

// helpRows is the static keybinding/command reference for the help overlay (§E).
func helpRows() []string {
	return []string{
		"enter          send prompt",
		"!              shell mode (run a command, output → context)",
		"ctrl+j         newline in composer",
		"ctrl+p         command palette",
		"ctrl+x l       sessions      ctrl+x n  new session",
		"ctrl+x m       model         ctrl+x a  agent",
		"ctrl+x g       timeline      ctrl+x s  status",
		"ctrl+x b       sidebar       ctrl+x t  tasks",
		"j / k          move message cursor (↑/↓ also)",
		"g / G          first / last message",
		"y              copy selected message",
		"ctrl+c         quit",
		"",
		"Palette: rename · fork · summarize · interrupt · share/unshare ·",
		"delete · MCP · skills · themes · refresh",
	}
}
