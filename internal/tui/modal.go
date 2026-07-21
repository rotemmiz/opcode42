package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
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
	modalRename  // text-input overlay (rename the current session)
	modalMCP     // read-only: configured MCP servers (GET /mcp)
	modalSkills  // read-only: available skills (GET /skill)
	modalHelp    // read-only: keybindings / commands reference
	modalVariant // model-variant picker (plan 08b §7)
	modalStash   // stashed prompt drafts (plan 08b §6)
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
	paVariant
	paStash
	paStashList
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
	{"Model variant", paVariant},
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
	{"Stash draft", paStash},
	{"Stashed drafts", paStashList},
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
func newSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.PostJSON(ctx, "/session", map[string]any{}, &ss)
		return sessionOpenedMsg{session: ss, err: err}
	}
}

// deleteSessionCmd deletes a session by id.
func deleteSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string) tea.Cmd {
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
	case modalVariant:
		for _, v := range m.activeVariants() {
			mark := "  "
			if v == m.model.Variant {
				mark = "● " // the active variant
			}
			rows = append(rows, mark+v)
		}
		if len(rows) == 0 {
			rows = []string{"(this model has no variants)"}
		}
		return "Model variant", rows, "enter select · esc close"
	case modalStash:
		for _, d := range m.stash {
			rows = append(rows, truncate(firstLine(d), 52))
		}
		if len(rows) == 0 {
			rows = []string{"(no stashed drafts)"}
		}
		return "Stashed drafts", rows, "enter restore · ctrl+d delete · esc close"
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
		case paVariant:
			m.modal, m.modalSel = modalVariant, m.variantSelIndex()
			return m, nil
		case paStash:
			return m.stashDraft(), nil
		case paStashList:
			m.modal, m.modalSel = modalStash, 0
			return m, nil
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
			ch := m.choices[m.modalSel]
			m.model = promptModel{Provider: ch.Provider, Model: ch.Model} // switching model resets the variant
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
	case modalVariant:
		vs := m.activeVariants()
		m.modal = modalNone
		if m.modalSel < len(vs) {
			m.model.Variant = vs[m.modalSel]
			m.status = "variant · " + m.model.Variant
			m.persist()
		}
	case modalStash:
		i := m.modalSel
		m.modal, m.modalSel = modalNone, 0
		return m.popStash(i), nil
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
//
// Border: rounded border with BorderActive color (brighter than Border) to
// signal an "owned surface" — mirrors opencode's dialog-select.tsx which uses a
// themed border.
//
// Surface fill: every row is rendered through Surface(BgElev) padded to the
// inner content width so the panel background is uniform — no transparent
// trailing cells on light terminals. (plan 08c M8 Tier 0 fill rule)
//
// Filter affordance: a "Search  /" hint below the title signals that typing
// filters the list — mirrors opencode's dialog-select.tsx filter input rendering.
//
// Selected row: s.Selection already provides the amber selection bar;
// Surface(BgElev) is applied to non-selected rows so they too have a fill.
func (m Model) modalView() string {
	s := m.styles

	// innerWidth is the usable content width inside Padding(1,2): width - 2*2 = width-4.
	// All rows are padded/truncated to innerWidth for uniform background fill.
	const (
		width      = 56
		innerWidth = width - 4 // width minus 2×horizontal padding (Padding(1,2) → 2 cols each side)
	)

	// surfaceRow renders a plain (non-selected) row with the panel surface
	// background so every trailing cell is painted. Each call returns a string
	// whose visible width == innerWidth. (plan 08c M8)
	surfaceRow := func(content string) string {
		return s.Surface(s.P.BgElev).Width(innerWidth).Render(content)
	}

	// The rename overlay is a single text field, not a list.
	if m.modal == modalRename {
		body := lipgloss.JoinVertical(lipgloss.Left,
			surfaceRow(s.Section.Render("Rename session")),
			surfaceRow(""),
			m.renameInput.View(),
			surfaceRow(""),
			surfaceRow(s.Faint.Render("enter save · esc cancel")),
		)
		panel := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(s.P.BorderActive).
			BorderBackground(s.P.BgElev).
			Background(s.P.BgElev).
			Padding(1, 2).Width(width + 2).Render(body) // v2: +2 for the border cols Width now includes
		return centerScreen(m.width, m.height, panel)
	}

	title, rows, footer := m.modalItems()

	// Window long lists around the selection so a provider with hundreds of
	// models (or many sessions) can't overflow the panel.
	const maxRows = 12
	start, end := windowAround(m.modalSel, len(rows), maxRows)

	var lines []string

	// Title row + a blank gap line — matches opencode dialog-select.tsx layout
	// which renders a bold title above the filter input and list body.
	lines = append(lines, surfaceRow(s.Section.Render(title)))
	lines = append(lines, surfaceRow(""))

	// Filter affordance: a "/" hint that signals the list is filterable by
	// typing — mirrors opencode's dialog-select.tsx filter input affordance
	// (lines 363–389).
	if isFilterableModal(m.modal) {
		lines = append(lines, surfaceRow(s.Faint.Render("Search  /")))
		lines = append(lines, surfaceRow(""))
	}

	if start > 0 {
		lines = append(lines, surfaceRow(s.Faint.Render("↑ more")))
	}
	for i := start; i < end; i++ {
		if i == m.modalSel {
			// Selection bar: amber bg, dark bold text — full inner width so the
			// highlight extends to the right edge of the panel.
			lines = append(lines, s.Selection.Width(innerWidth).Render(" "+rows[i]))
		} else {
			// Non-selected rows: surface-filled so no transparent trailing cells.
			lines = append(lines, surfaceRow(s.Base.Render(" "+rows[i])))
		}
	}
	if end < len(rows) {
		lines = append(lines, surfaceRow(s.Faint.Render("↓ more")))
	}
	lines = append(lines, surfaceRow(""))
	lines = append(lines, surfaceRow(s.Faint.Render(footer)))

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.P.BorderActive).
		BorderBackground(s.P.BgElev).
		Background(s.P.BgElev).
		Padding(1, 2).
		Width(width + 2). // v2: +2 for the border cols Width now includes
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))

	return centerScreen(m.width, m.height, panel)
}

// isFilterableModal returns true for dialogs where typing filters the list —
// matches the subset of opencode dialogs that render a filter input
// (dialog-model, dialog-theme-list, dialog-agent, dialog-session-list,
// dialog-stash). Read-only or single-action modals (status, help, MCP,
// skills, timeline, rename, variant) don't benefit from a search hint.
func isFilterableModal(k modalKind) bool {
	switch k {
	case modalPalette, modalModels, modalThemes, modalAgents, modalSessions, modalStash:
		return true
	default:
		return false
	}
}
