package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// agentItem is one selectable agent in the agent switcher (GET /agent).
type agentItem struct {
	name string
	mode string // "primary" | "subagent"
	desc string
}

// agentsLoadedMsg carries the (non-hidden) agent list.
type agentsLoadedMsg struct {
	items []agentItem
	err   error
}

// loadAgentsCmd fetches GET /agent, dropping hidden/internal agents (compaction,
// summary, title) so only the user-selectable ones remain.
func loadAgentsCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		var raw []struct {
			Name        string `json:"name"`
			Mode        string `json:"mode"`
			Description string `json:"description"`
			Hidden      bool   `json:"hidden"`
		}
		if err := c.GetJSON(ctx, "/agent", &raw); err != nil {
			return agentsLoadedMsg{err: err}
		}
		items := make([]agentItem, 0, len(raw))
		for _, a := range raw {
			if a.Hidden || a.Name == "" {
				continue
			}
			items = append(items, agentItem{name: a.Name, mode: a.Mode, desc: a.Description})
		}
		return agentsLoadedMsg{items: items}
	}
}

// agentSelIndex is the position of the active agent in the list (0 when unset),
// so the switcher opens pre-highlighted.
func (m Model) agentSelIndex() int {
	for i, a := range m.agents {
		if a.name == m.agent {
			return i
		}
	}
	return 0
}

// themeSelIndex is the position of the active theme in the registry.
func (m Model) themeSelIndex() int {
	for i, n := range theme.Palettes() {
		if n.Name == m.themeName {
			return i
		}
	}
	return 0
}
