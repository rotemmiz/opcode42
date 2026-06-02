package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
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
	modalTimeline
	modalStatus
	modalRename // text-input overlay (rename the current session)
	modalMCP    // read-only: configured MCP servers (GET /mcp)
	modalSkills // read-only: available skills (GET /skill)
	modalHelp   // read-only: keybindings / commands reference
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
	paTimeline
	paStatus
	paRefresh
	paRename
	paShare
	paUnshare
	paSummarize
	paAbort
	paFork
	paDelete
	paDiff
	paTerminal
	paMCP
	paSkills
	paHelp
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
	{"Timeline", paTimeline},
	{"Status", paStatus},
	{"Rename session", paRename},
	{"Fork session", paFork},
	{"Summarize context", paSummarize},
	{"Interrupt (abort turn)", paAbort},
	{"Review changes (diff)", paDiff},
	{"Terminal (PTY)", paTerminal},
	{"Share session", paShare},
	{"Unshare session", paUnshare},
	{"Delete session", paDelete},
	{"MCP servers", paMCP},
	{"Skills", paSkills},
	{"Keybindings / help", paHelp},
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
	case modalTimeline:
		for _, it := range m.timelineItems() {
			rows = append(rows, truncate(it.title, 52))
		}
		if len(rows) == 0 {
			rows = []string{"(no turns yet)"}
		}
		return "Timeline", rows, "enter revert here · esc close"
	case modalStatus:
		for _, line := range m.statusLines() {
			rows = append(rows, truncate(line, 52)) // keep within the panel
		}
		return "Status", rows, "esc close"
	case modalMCP:
		for _, s := range m.mcpServers {
			row := s.Name
			if s.Status != "" {
				row += "  " + s.Status
			}
			rows = append(rows, truncate(row, 52))
		}
		if len(rows) == 0 {
			rows = []string{"(no MCP servers)"}
		}
		return "MCP servers", rows, "esc close"
	case modalSkills:
		for _, s := range m.skills {
			row := s.Name
			if s.Description != "" {
				row += "  " + s.Description
			}
			rows = append(rows, truncate(row, 52))
		}
		if len(rows) == 0 {
			rows = []string{"(no skills)"}
		}
		return "Skills", rows, "esc close"
	case modalHelp:
		rows = helpRows()
		return "Keybindings", rows, "esc close"
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
		case paTimeline:
			m.modal, m.modalSel = modalTimeline, 0
			return m, nil
		case paStatus:
			m.modal, m.modalSel = modalStatus, 0
			return m, nil
		case paRename:
			if m.cfg.SessionID == "" {
				m.status = "no session to rename"
				return m, nil
			}
			m.modal = modalRename
			if cur := m.currentSession(); cur != nil {
				m.renameInput.SetValue(cur.Title)
			}
			m.renameInput.CursorEnd()
			m.renameInput.Focus()
			return m, nil
		case paShare:
			if m.cfg.SessionID == "" {
				m.status = "no session to share"
				return m, nil
			}
			cur := m.currentSession()
			if cur != nil && cur.Share != nil && cur.Share.URL != "" {
				sh := cur.Share
				m.status = "shared · " + sh.URL + " (copied)"
				return m, copyClipboardCmd(sh.URL)
			}
			m.status = "sharing…"
			return m, shareSessionCmd(m.ctx, m.client, m.cfg.SessionID)
		case paUnshare:
			if m.cfg.SessionID == "" {
				return m, nil
			}
			return m, unshareSessionCmd(m.ctx, m.client, m.cfg.SessionID)
		case paSummarize:
			if m.cfg.SessionID == "" || !m.model.ok() {
				m.status = "summarize needs an open session + model"
				return m, nil
			}
			m.status = "summarizing…"
			return m, summarizeSessionCmd(m.ctx, m.client, m.cfg.SessionID, m.model)
		case paAbort:
			if m.cfg.SessionID == "" {
				return m, nil
			}
			m.status = "interrupting…"
			return m, abortSessionCmd(m.ctx, m.client, m.cfg.SessionID)
		case paFork:
			if m.cfg.SessionID == "" {
				return m, nil
			}
			m.status = "forking…"
			return m, forkSessionCmd(m.ctx, m.client, m.cfg.SessionID)
		case paDelete:
			if m.cfg.SessionID == "" {
				return m, nil
			}
			return m, deleteSessionCmd(m.ctx, m.client, m.cfg.SessionID)
		case paDiff:
			return m.openDiff()
		case paTerminal:
			return m.focusOrOpenPTY()
		case paMCP:
			m.modal, m.modalSel = modalMCP, 0
			return m, loadMCPCmd(m.ctx, m.client)
		case paSkills:
			m.modal, m.modalSel = modalSkills, 0
			return m, loadSkillsCmd(m.ctx, m.client)
		case paHelp:
			m.modal, m.modalSel = modalHelp, 0
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
			m.persist() // remember the model across runs
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
			m.persist() // remember the theme across runs
		}
	case modalTimeline:
		items := m.timelineItems()
		m.modal = modalNone
		if m.modalSel < len(items) {
			m.status = "reverting…"
			return m, revertCmd(m.ctx, m.client, m.cfg.SessionID, items[m.modalSel].messageID)
		}
	case modalStatus, modalMCP, modalSkills, modalHelp:
		m.modal = modalNone // read-only — enter just closes
	case modalRename:
		title := strings.TrimSpace(m.renameInput.Value())
		id := m.cfg.SessionID
		m.modal, m.renameInput = modalNone, blurInput(m.renameInput)
		if title != "" && id != "" {
			return m, renameSessionCmd(m.ctx, m.client, id, title)
		}
	}
	return m, nil
}

// blurInput clears the value + focus of a text input (reset between uses).
func blurInput(ti textinput.Model) textinput.Model {
	ti.Blur()
	ti.SetValue("")
	return ti
}

// modalView renders the active modal as a centered panel over the background.
func (m Model) modalView() string {
	s := m.styles

	width := 56

	// The rename overlay is a single text field, not a list.
	if m.modal == modalRename {
		body := lipgloss.JoinVertical(lipgloss.Left,
			s.Section.Render("Rename session"), "",
			m.renameInput.View(), "",
			s.Faint.Render("enter save · esc cancel"),
		)
		panel := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).BorderForeground(s.P.Border).
			Padding(1, 2).Width(width).Render(body)
		return centerScreen(m.width, m.height, panel)
	}

	title, rows, footer := m.modalItems()

	// Window long lists around the selection so a provider with hundreds of
	// models (or many sessions) can't overflow the panel.
	const maxRows = 12
	start, end := windowAround(m.modalSel, len(rows), maxRows)

	var lines []string
	lines = append(lines, s.Section.Render(title), "")
	if start > 0 {
		lines = append(lines, s.Faint.Render("  ↑ more"))
	}
	for i := start; i < end; i++ {
		if i == m.modalSel {
			lines = append(lines, s.Selection.Width(width-4).Render(" "+rows[i])) // -4: fits inside Padding(1,2)
		} else {
			lines = append(lines, s.Base.Render(" "+rows[i]))
		}
	}
	if end < len(rows) {
		lines = append(lines, s.Faint.Render(" ↓ more"))
	}
	lines = append(lines, "", s.Faint.Render(footer))

	panel := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(s.P.Border).
		Padding(1, 2).
		Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))

	return centerScreen(m.width, m.height, panel)
}
