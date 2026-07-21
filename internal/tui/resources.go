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

// helpRows is the static keybinding/command reference for the help overlay
// (plan 08a §E, expanded in plan 08e §F3 to cover the full keybind surface).
//
// The rows are grouped by category (Navigation, Sessions & models, Display,
// Tools & output, Terminal & drafts, Subagents, Global) so the ~40 keybinds
// the TUI now has are scannable. The width is bounded by the modal panel
// (truncate(row, 52) in modalItems); the two-column "chord  description"
// format keeps each row within that bound. The chord keys match
// handleLeaderKey (model.go) and whichKeyChords (whichkey.go) — the
// TestHelpModal_ContainsAllKeybinds test pins the major ones.
func helpRows() []string {
	return []string{
		"Navigation",
		"  ↑/↓          scroll stream / recall history",
		"  pgup/pgdn    scroll stream by a page",
		"  g / G        first / last message",
		"  enter        send prompt",
		"  !            shell mode (run, output → context)",
		"  esc          exit shell mode / close overlay",
		"",
		"Sessions & models",
		"  ctrl+p       command palette",
		"  ctrl+t       cycle model variant",
		"  ctrl+x l     sessions list",
		"  ctrl+x n     new session",
		"  ctrl+x m     switch model",
		"  ctrl+x a     switch agent",
		"  ctrl+x p     palette (alt)",
		"",
		"Display",
		"  ctrl+x b     toggle sidebar",
		"  ctrl+x t     toggle tasks dock",
		"  ctrl+x g     timeline (revert to a turn)",
		"  ctrl+x s     status modal",
		"  ctrl+x c     connect to daemon (mDNS)",
		"  ctrl+x y     copy last response",
		"",
		"Thinking & tools",
		"  ctrl+x r     thinking hide/show (default hide)",
		"  ctrl+x f     fold/unfold reasoning body",
		"  ctrl+x o     hide/show tool output",
		"  ctrl+x v     fold/unfold last tool",
		"",
		"Terminal & drafts",
		"  ctrl+x `     embedded terminal (ctrl+] exit)",
		"  ctrl+x e     edit composer in $EDITOR",
		"  ctrl+x d     review session changes (diff)",
		"  ctrl+x w     stash composer draft",
		"",
		"Subagents",
		"  ctrl+x ↓     descend into first child",
		"  ctrl+x ↑     return to parent session",
		"  ctrl+x ]     next sibling subagent",
		"  ctrl+x [     previous sibling subagent",
		"",
		"Help & global",
		"  F1           this help overlay",
		"  ctrl+x h     this help overlay (alt)",
		"  /help        this help overlay (slash)",
		"  ctrl+c       quit",
		"",
		"Slash commands",
		"  /new /sessions /models /agents /themes",
		"  /timeline /diff /terminal /variant /stash",
		"  /status /connect /help",
	}
}
