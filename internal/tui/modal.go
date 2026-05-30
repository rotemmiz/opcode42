package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/forge/internal/tui/theme"
	forgeclient "github.com/rotemmiz/forge/sdk/go"
)

// modalKind is the active command overlay (none = the normal screen).
type modalKind int

const (
	modalNone modalKind = iota
	modalPalette
	modalSessions
	modalModels
	modalAgents
	modalThemes
)

// paletteAction identifies a command-palette entry (dispatched by id, not index,
// so the list order can change without remapping).
type paletteAction int

const (
	paNewSession paletteAction = iota
	paSwitchSession
	paSwitchModel
	paSwitchAgent
	paSwitchTheme
	paRefresh
)

type paletteCmd struct {
	label  string
	action paletteAction
}

// paletteItems are the command-palette entries, in display order.
var paletteItems = []paletteCmd{
	{"New session", paNewSession},
	{"Switch session", paSwitchSession},
	{"Switch model", paSwitchModel},
	{"Switch agent", paSwitchAgent},
	{"Switch theme", paSwitchTheme},
	{"Refresh sessions", paRefresh},
}

// Modal action results.
type (
	sessionOpenedMsg struct {
		session Session
		err     error
	}
	sessionDeletedMsg struct {
		id  string
		err error
	}
)

// newSessionCmd creates a session and opens it (no prompt).
func newSessionCmd(ctx context.Context, c *forgeclient.ForgeClient) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.PostJSON(ctx, "/session", map[string]any{}, &ss)
		return sessionOpenedMsg{session: ss, err: err}
	}
}

// deleteSessionCmd deletes a session by id.
func deleteSessionCmd(ctx context.Context, c *forgeclient.ForgeClient, id string) tea.Cmd {
	return func() tea.Msg {
		return sessionDeletedMsg{id: id, err: c.Delete(ctx, "/session/"+id)}
	}
}

// orderedSessions returns the sessions newest-first (the store keeps them
// ascending by id; descending id == newest-first).
func (m Model) orderedSessions() []Session {
	out := make([]Session, len(m.store.sessions))
	for i, s := range m.store.sessions {
		out[len(out)-1-i] = s
	}
	return out
}

// modalItems returns the visible rows + an optional footer hint for the modal.
func (m Model) modalItems() (title string, rows []string, footer string) {
	switch m.modal {
	case modalPalette:
		for _, it := range paletteItems {
			rows = append(rows, it.label)
		}
		return "Commands", rows, "↑↓ move · enter select · esc close"
	case modalSessions:
		for _, s := range m.orderedSessions() {
			rows = append(rows, sessionRowLabel(s))
		}
		if len(rows) == 0 {
			rows = []string{"(no sessions — ctrl+n to create)"}
		}
		return "Sessions", rows, "enter open · ctrl+n new · ctrl+d delete · esc close"
	case modalModels:
		for _, ch := range m.choices {
			mark := "  "
			if ch.Provider == m.model.Provider && ch.Model == m.model.Model {
				mark = "● " // the active model
			}
			rows = append(rows, mark+ch.label())
		}
		if len(rows) == 0 {
			rows = []string{"(no connected providers — set a provider API key)"}
		}
		return "Models", rows, "enter select · esc close"
	case modalAgents:
		for _, a := range m.agents {
			mark := "  "
			if a.name == m.agent {
				mark = "● " // the active agent
			}
			row := mark + a.name
			if a.mode != "" {
				row += "  " + a.mode
			}
			rows = append(rows, row)
		}
		if len(rows) == 0 {
			rows = []string{"(no agents)"}
		}
		return "Agents", rows, "enter select · esc close"
	case modalThemes:
		for _, n := range theme.Palettes() {
			mark := "  "
			if n.Name == m.themeName {
				mark = "● " // the active theme
			}
			rows = append(rows, mark+n.Name)
		}
		return "Themes", rows, "enter select · esc close"
	default:
		return "", nil, ""
	}
}

func sessionRowLabel(s Session) string {
	if s.Title != "" {
		return s.Title
	}
	return s.ID
}

// modalCount is the number of selectable rows in the active modal.
func (m Model) modalCount() int {
	_, rows, _ := m.modalItems()
	return len(rows)
}

// modalSelect dispatches the highlighted row.
func (m Model) modalSelect() (tea.Model, tea.Cmd) {
	switch m.modal {
	case modalPalette:
		m.modal = modalNone
		if m.modalSel >= len(paletteItems) {
			return m, nil
		}
		switch paletteItems[m.modalSel].action {
		case paNewSession:
			return m, newSessionCmd(m.ctx, m.client)
		case paSwitchSession:
			m.modal, m.modalSel = modalSessions, 0
			return m, loadSessionsCmd(m.ctx, m.client)
		case paSwitchModel: // open pre-highlighted on the active model, refresh
			m.modal, m.modalSel = modalModels, m.modelSelIndex()
			return m, loadProvidersCmd(m.ctx, m.client)
		case paSwitchAgent:
			m.modal, m.modalSel = modalAgents, m.agentSelIndex()
			return m, loadAgentsCmd(m.ctx, m.client)
		case paSwitchTheme:
			m.modal, m.modalSel = modalThemes, m.themeSelIndex()
			return m, nil
		case paRefresh:
			return m, loadSessionsCmd(m.ctx, m.client)
		}
	case modalSessions:
		ss := m.orderedSessions()
		m.modal = modalNone
		if m.modalSel < len(ss) {
			m.cfg.SessionID = ss[m.modalSel].ID
			m.screen = ScreenSession
			return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)
		}
	case modalModels:
		m.modal = modalNone
		if m.modalSel < len(m.choices) {
			m.model = promptModel(m.choices[m.modalSel])
			m.status = "model · " + m.model.label()
		}
	case modalAgents:
		m.modal = modalNone
		if m.modalSel < len(m.agents) {
			m.agent = m.agents[m.modalSel].name
			m.status = "agent · " + m.agent
		}
	case modalThemes:
		m.modal = modalNone
		if ps := theme.Palettes(); m.modalSel < len(ps) {
			m = m.applyTheme(ps[m.modalSel].Name, ps[m.modalSel].Palette)
			m.status = "theme · " + m.themeName
		}
	}
	return m, nil
}

// modalView renders the active modal as a centered panel over the background.
func (m Model) modalView() string {
	s := m.styles
	title, rows, footer := m.modalItems()

	width := 56

	// Window long lists around the selection so a provider with hundreds of
	// models (or many sessions) can't overflow the panel. Scroll is Phase 3;
	// this keeps the modal bounded until then.
	const maxRows = 12
	start := 0
	if len(rows) > maxRows {
		start = m.modalSel - maxRows/2
		if start < 0 {
			start = 0
		}
		if hi := len(rows) - maxRows; start > hi {
			start = hi
		}
	}
	end := start + maxRows
	if end > len(rows) {
		end = len(rows)
	}

	var lines []string
	lines = append(lines, s.Section.Render(title), "")
	if start > 0 {
		lines = append(lines, s.Faint.Render("  ↑ more"))
	}
	for i := start; i < end; i++ {
		if i == m.modalSel {
			lines = append(lines, s.Selection.Width(width-2).Render(" "+rows[i]))
		} else {
			lines = append(lines, s.Base.Render("  "+rows[i]))
		}
	}
	if end < len(rows) {
		lines = append(lines, s.Faint.Render("  ↓ more"))
	}
	lines = append(lines, "", s.Faint.Render(footer))

	panel := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(s.P.Border).
		Background(s.P.BgElev).
		Padding(1, 2).
		Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))

	if m.width == 0 || m.height == 0 {
		return panel
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}
