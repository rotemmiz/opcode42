package tui

import (
	"context"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/mdns"
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
	modalConnect // mDNS server picker + manual URL entry (plan 08e §D2)
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
	paConnect       // open the connect overlay (plan 08e §D2)
	paUndo          // messages_undo (08f H1b)
	paRedo          // messages_redo (08f H1b)
	paTerminalTitle // terminal.title.toggle (08f H6)
	paToggleOsc52   // clipboard.osc52.toggle (08f H11 / plan G.13)

	// Display toggles (08f H7 / plan G.11 — opencode app.toggle.*).
	paToggleAnimations       // app.toggle.animations
	paToggleFileContext      // app.toggle.file_context (no render consumer yet)
	paToggleDiffTree         // closest Opcode42 surface to app.toggle.diffwrap (reuses diffTreeHidden)
	paTogglePasteSummary     // app.toggle.paste_summary
	paToggleSessionDirFilter // app.toggle.session_directory_filter (no render consumer yet)

	// Theme mode + lock (08f H7 / plan G.12).
	paThemeSwitchMode // theme.switch_mode
	paThemeModeLock   // theme.mode.lock
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
	{"Undo last turn", paUndo},
	{"Redo (unrevert)", paRedo},
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
	{"Connect to daemon", paConnect},
	{"Keybindings / help", paHelp},
	{"Refresh sessions", paRefresh},
	{"Toggle terminal title", paTerminalTitle},
	{"Toggle OSC 52 clipboard", paToggleOsc52},    // label resolved dynamically — paletteLabel
	{"Disable animations", paToggleAnimations},    // label resolved dynamically — paletteLabel
	{"Disable file context", paToggleFileContext}, // label resolved dynamically — paletteLabel
	{"Toggle diff file tree", paToggleDiffTree},
	{"Disable paste summary", paTogglePasteSummary},                   // label resolved dynamically — paletteLabel
	{"Disable session directory filtering", paToggleSessionDirFilter}, // label resolved dynamically — paletteLabel
	{"Switch to light mode", paThemeSwitchMode},                       // label resolved dynamically — paletteLabel
	{"Lock theme mode", paThemeModeLock},                              // label resolved dynamically — paletteLabel
}

// paletteLabel resolves a palette entry's display label, substituting live
// on/off or mode text for entries whose title reflects current state —
// mirrors opencode's dynamic command titles (app.tsx: kv.get(...) ? "Disable
// …" : "Enable …"). Entries not listed here just use their static label.
func (m Model) paletteLabel(it paletteCmd) string {
	switch it.action {
	case paToggleOsc52:
		if m.osc52Enabled {
			return "Disable OSC 52 clipboard"
		}
		return "Enable OSC 52 clipboard"
	case paToggleAnimations:
		if m.noAnim {
			return "Enable animations"
		}
		return "Disable animations"
	case paToggleFileContext:
		if m.fileContextEnabled {
			return "Disable file context"
		}
		return "Enable file context"
	case paTogglePasteSummary:
		if m.pasteSummaryEnabled {
			return "Disable paste summary"
		}
		return "Enable paste summary"
	case paToggleSessionDirFilter:
		if m.sessionDirFilterEnabled {
			return "Disable session directory filtering"
		}
		return "Enable session directory filtering"
	case paThemeSwitchMode:
		if m.termDark {
			return "Switch to light mode"
		}
		return "Switch to dark mode"
	case paThemeModeLock:
		if m.themeModeLocked {
			return "Unlock theme mode"
		}
		return "Lock theme mode"
	default:
		return it.label
	}
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

// sessionSubtreeRows renders the sessions list as a parent→children subtree
// (plan 08e §C4): top-level (parentID == "") sessions newest-first, each
// followed by its children indented with a ├─/└─ prefix. Unlike the flat
// list (which filters out children), the subtree view shows them — matching
// Android's D1 rail. Returns the row labels in display order; the
// corresponding session IDs are available via sessionSubtreeIDs so
// modalSelect can resolve the highlighted row.
func (m Model) sessionSubtreeRows() []string {
	var rows []string
	for _, s := range m.orderedSessions() {
		if s.ParentID != "" {
			continue
		}
		rows = append(rows, sessionRowLabel(s))
		kids := m.childrenOf(s.ID)
		for i, kid := range kids {
			prefix := "├─ "
			if i == len(kids)-1 {
				prefix = "└─ "
			}
			rows = append(rows, prefix+sessionRowLabel(kid))
		}
	}
	return rows
}

// sessionSubtreeIDs returns the session IDs in the same display order as
// sessionSubtreeRows, so modalSelect can resolve the highlighted row to a
// session id. Children are included (the subtree view doesn't filter them,
// unlike the flat main list).
func (m Model) sessionSubtreeIDs() []string {
	var ids []string
	for _, s := range m.orderedSessions() {
		if s.ParentID != "" {
			continue
		}
		ids = append(ids, s.ID)
		for _, kid := range m.childrenOf(s.ID) {
			ids = append(ids, kid.ID)
		}
	}
	return ids
}

// sessionIDAtModalSel resolves the modal selection to a session id, handling
// both the flat list (orderedSessions) and the subtree view
// (sessionSubtreeIDs). Returns "" when the selection is out of range.
func (m Model) sessionIDAtModalSel() string {
	if m.view.sessionsSubtree {
		ids := m.sessionSubtreeIDs()
		if m.modalSel < len(ids) {
			return ids[m.modalSel]
		}
		return ""
	}
	ss := m.orderedSessions()
	if m.modalSel < len(ss) {
		return ss[m.modalSel].ID
	}
	return ""
}

// modalItems returns the visible rows + an optional footer hint for the modal.
func (m Model) modalItems() (title string, rows []string, footer string) {
	switch m.modal {
	case modalPalette:
		for _, it := range paletteItems {
			rows = append(rows, m.paletteLabel(it))
		}
		return "Commands", rows, "↑↓ move · enter select · esc close"
	case modalSessions:
		if m.view.sessionsSubtree {
			rows = m.sessionSubtreeRows()
		} else {
			for _, s := range m.orderedSessions() {
				rows = append(rows, sessionRowLabel(s))
			}
		}
		if len(rows) == 0 {
			rows = []string{"(no sessions — ctrl+n to create)"}
		}
		return "Sessions", rows, "enter open · ctrl+n new · ctrl+d delete · t subtree · esc close"
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
	case modalConnect:
		// Nearby-servers list populated by D1's mDNS browser. The manual URL
		// field is rendered above the list by modalView's modalConnect branch
		// (the overlay has its own layout — a text input + a list — instead of
		// the plain rows+footer shape). modalItems only carries the selectable
		// server rows here; modalView reads them via this case too.
		for _, svc := range m.discoveredServers {
			rows = append(rows, m.connectRowLabel(svc))
		}
		if len(rows) == 0 {
			rows = []string{"(no nearby daemons — enter a URL above)"}
		}
		return "Connect", rows, "↑↓ move · enter connect · tab edit URL · esc close"
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

// connectRowLabel renders one nearby-daemon row: a colored reachability dot,
// the instance name, and host:port. The dot is best-effort: green ● means a
// recent /global/health probe succeeded, amber ● means unknown/unreachable.
// The probe state lives on m.serverProbe (keyed by host:port); a missing entry
// reads as "unknown" (amber) — the probe is async and may still be in flight.
func (m Model) connectRowLabel(svc mdns.DiscoveredService) string {
	dotColor := m.styles.P.Amber // unknown / unreachable
	if p, ok := m.serverProbe[connectProbeKey(svc)]; ok && p.reachable {
		dotColor = m.styles.P.Green
	}
	dot := lipgloss.NewStyle().Foreground(dotColor).Render("●")
	return dot + "  " + svc.Name + "  " + svc.Host + ":" + strconv.Itoa(svc.Port)
}

// connectProbeKey is the dedupe key for reachability probes (host:port).
func connectProbeKey(svc mdns.DiscoveredService) string {
	return svc.Host + ":" + strconv.Itoa(svc.Port)
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
		case paUndo:
			return m.undoLastTurn()
		case paRedo:
			return m.redoTurn()
		case paStatus:
			m.modal, m.modalSel = modalStatus, 0
			return m, nil
		case paRename:
			return m.openRename()
		case paShare:
			return m.shareOrCopyLink()
		case paUnshare:
			return m.unshareCurrent()
		case paSummarize:
			return m.compactSession()
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
		case paConnect:
			m = m.openConnectModal()
			if m.discoverCtx != nil {
				return m, startDiscoverCmd(m.discoverCtx)
			}
			return m, nil
		case paHelp:
			m.modal, m.modalSel = modalHelp, 0
			return m, nil
		case paRefresh:
			return m, loadSessionsCmd(m.ctx, m.client)
		case paTerminalTitle:
			// terminal.title.toggle (plan 08f H6).
			m.terminalTitleEnabled = !m.terminalTitleEnabled
			if m.terminalTitleEnabled {
				m.status = "terminal title: on"
			} else {
				m.status = "terminal title: off"
			}
			m.persist()
			m = m.rerenderChrome()
			return m, nil
		case paToggleOsc52:
			// clipboard.osc52.toggle (plan 08f H11 / G.13). --no-osc52
			// forces off — the palette entry is a no-op then, mirroring
			// paToggleAnimations / --no-anim.
			if m.cfg.NoOSC52 {
				m.status = "OSC 52 clipboard forced off (--no-osc52)"
				return m, nil
			}
			m.osc52Enabled = !m.osc52Enabled
			if m.osc52Enabled {
				m.status = "OSC 52 clipboard: on"
			} else {
				m.status = "OSC 52 clipboard: off"
			}
			m.persist()
			return m, nil
		case paToggleAnimations:
			// app.toggle.animations (plan 08f H7 / G.11). --no-anim still
			// forces animations off — the palette entry is a no-op then.
			if m.cfg.NoAnim {
				m.status = "animations forced off (--no-anim)"
				return m, nil
			}
			m.noAnim = !m.noAnim
			if m.noAnim {
				m.status = "animations: off"
			} else {
				m.status = "animations: on"
			}
			m.persist()
			m = m.rerenderChrome()
			return m, m.maybeKickAnim()
		case paToggleFileContext:
			// app.toggle.file_context (plan 08f H7 / G.11) — no render
			// consumer yet; toggle+persist ahead of the feature landing.
			m.fileContextEnabled = !m.fileContextEnabled
			if m.fileContextEnabled {
				m.status = "file context: on"
			} else {
				m.status = "file context: off"
			}
			m.persist()
			return m, nil
		case paToggleDiffTree:
			// Closest Opcode42 surface to app.toggle.diffwrap: Opcode42 has no
			// diff line-wrap mode, so this reuses the diff reviewer's
			// file-tree pane preference (same field as the diff 't' key).
			m.diffTreeHidden = !m.diffTreeHidden
			if m.diff.open {
				m.diff.showTree = !m.diffTreeHidden
			}
			if m.diffTreeHidden {
				m.status = "diff file tree: hidden"
			} else {
				m.status = "diff file tree: shown"
			}
			m.persist()
			m = m.rerenderChrome()
			return m, nil
		case paTogglePasteSummary:
			// app.toggle.paste_summary (plan 08f H7 / G.11).
			m.pasteSummaryEnabled = !m.pasteSummaryEnabled
			if m.pasteSummaryEnabled {
				m.status = "paste summary: on"
			} else {
				m.status = "paste summary: off"
			}
			m.persist()
			return m, nil
		case paToggleSessionDirFilter:
			// app.toggle.session_directory_filter (plan 08f H7 / G.11) — no
			// render consumer yet; toggle+persist ahead of the feature landing.
			m.sessionDirFilterEnabled = !m.sessionDirFilterEnabled
			if m.sessionDirFilterEnabled {
				m.status = "session directory filter: on"
			} else {
				m.status = "session directory filter: off"
			}
			m.persist()
			return m, nil
		case paThemeSwitchMode:
			// theme.switch_mode (plan 08f H7 / G.12): flip the dark/light
			// mode and re-resolve the active theme for the other mode.
			// Native opcode42-dark/light are mode-specific names (not dual
			// variants of one name), so also swap the theme name itself.
			m.termDark = !m.termDark
			m = m.applyThemeForMode(m.themeName, m.termDark)
			if m.termDark {
				m.status = "theme mode: dark"
			} else {
				m.status = "theme mode: light"
			}
			m.persist()
			return m, nil
		case paThemeModeLock:
			// theme.mode.lock (plan 08f H7 / G.12): pin/unpin the current
			// dark/light mode across launches.
			m.themeModeLocked = !m.themeModeLocked
			switch {
			case m.themeModeLocked && m.termDark:
				m.status = "theme mode locked · dark"
			case m.themeModeLocked:
				m.status = "theme mode locked · light"
			default:
				m.status = "theme mode unlocked"
			}
			m.persist()
			return m, nil
		}
	case modalSessions:
		m.modal = modalNone
		if id := m.sessionIDAtModalSel(); id != "" {
			m.cfg.SessionID = id
			m.screen = ScreenSession
			// Plan 20: session switch → re-render all (the subsequent
			// messagesLoadedMsg will trigger another re-render once the
			// stream is loaded).
			m = m.rerenderFull()
			return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)
		}
	case modalModels:
		m.modal = modalNone
		if m.modalSel < len(m.choices) {
			ch := m.choices[m.modalSel]
			m.model = promptModel{Provider: ch.Provider, Model: ch.Model} // switching model resets the variant
			m.status = "model · " + m.model.label()
			m.persist() // remember the model across runs
			// Plan 20: model changed → re-render footer (status bar shows the
			// model) + sidebar (context limit reads m.model).
			m = m.rerenderFull()
		}
	case modalAgents:
		m.modal = modalNone
		if m.modalSel < len(m.agents) {
			m.agent = m.agents[m.modalSel].name
			m.status = "agent · " + m.agent
			// Plan 20: agent changed → re-render footer (status bar mode chip).
			m = m.rerenderChrome()
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
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, revertCmd(m.ctx, m.client, m.cfg.SessionID, items[m.modalSel].messageID)
		}
	case modalVariant:
		vs := m.activeVariants()
		m.modal = modalNone
		if m.modalSel < len(vs) {
			m.model.Variant = vs[m.modalSel]
			m.status = "variant · " + m.model.Variant
			m.persist()
			// Plan 20: variant changed → re-render footer (status bar variant
			// chip).
			m = m.rerenderChrome()
		}
	case modalStash:
		i := m.modalSel
		m.modal, m.modalSel = modalNone, 0
		m = m.popStash(i)
		// Plan 20: composer text + status changed → re-render footer.
		m = m.rerenderChrome()
		return m, nil
	case modalStatus, modalMCP, modalSkills, modalHelp:
		m.modal = modalNone // read-only — enter just closes
	case modalConnect:
		// Selecting a discovered server: build http://<host>:<port> and dial
		// it. The connect path is identical to a CLI --url connect: rebuild
		// the client, re-issue the health + SSE bootstrap, and pin the URL.
		if m.modalSel >= len(m.discoveredServers) {
			var cmd tea.Cmd
			m, cmd = m.closeConnectModal()
			return m, cmd
		}
		svc := m.discoveredServers[m.modalSel]
		return m.connectTo("http://" + svc.Host + ":" + strconv.Itoa(svc.Port))
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

// modalPanelBuild is the result of buildModalPanel: the rendered (but not yet
// centered) panel string, plus the row-selection geometry needed to map a
// mouse Y to a modalSel index (plan 08f H4 / G.3). rowFirstLine is the line
// index — 0-based, counted within the panel's JoinVertical content, i.e.
// before the border+padding wrapper is applied — of the first VISIBLE
// selectable row; rowStart/rowEnd is the [start,end) window of modalSel
// values currently shown (from windowAround/the connect list window). ok is
// false when the modal has no selectable rows to hover/click (e.g. the
// rename text-input overlay, or an empty connect server list).
type modalPanelBuild struct {
	panel                          string
	rowFirstLine, rowStart, rowEnd int
	ok                             bool
}

// buildModalPanel builds the active modal's panel content. It is the single
// source of truth for the modal's on-screen layout: modalView() wraps the
// panel with centerScreen for rendering, and modalRowAtY() (H4) uses the same
// row geometry to hit-test a mouse click/hover — so the two can never drift
// out of sync.
func (m Model) buildModalPanel() modalPanelBuild {
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

	// The rename overlay is a single text field, not a list — no selectable
	// rows to hover/click.
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
		return modalPanelBuild{panel: panel}
	}

	// The connect overlay (plan 08e §D2) is a manual URL field + a nearby-
	// servers list. It has its own layout (a header, a focused/unfocused text
	// input, a sub-header, the server list, and a footer hint) rather than the
	// plain title+rows+footer shape — so it's handled here, before the generic
	// list path. Tab toggles focus between the URL field and the server list;
	// m.connectFieldFocus tracks which side owns the cursor.
	if m.modal == modalConnect {
		var lines []string
		lines = append(lines, surfaceRow(s.Section.Render("Connect")))
		lines = append(lines, surfaceRow(""))
		lines = append(lines, surfaceRow(s.Faint.Render("Daemon URL")))
		lines = append(lines, m.connectURLInput.View())
		lines = append(lines, surfaceRow(""))
		lines = append(lines, surfaceRow(s.Faint.Render("Nearby servers")))
		var rowFirstLine, start, end int
		if len(m.discoveredServers) == 0 {
			lines = append(lines, surfaceRow(s.Faint.Render("(browsing…)")))
		} else {
			const maxRows = 10
			start, end = windowAround(m.modalSel, len(m.discoveredServers), maxRows)
			if start > 0 {
				lines = append(lines, surfaceRow(s.Faint.Render("↑ more")))
			}
			rowFirstLine = len(lines)
			for i := start; i < end; i++ {
				row := m.connectRowLabel(m.discoveredServers[i])
				if i == m.modalSel && !m.connectFieldFocus {
					lines = append(lines, s.Selection.Width(innerWidth).Render(" "+row))
				} else {
					lines = append(lines, surfaceRow(s.Base.Render(" "+row)))
				}
			}
			if end < len(m.discoveredServers) {
				lines = append(lines, surfaceRow(s.Faint.Render("↓ more")))
			}
		}
		lines = append(lines, surfaceRow(""))
		lines = append(lines, surfaceRow(s.Faint.Render("enter connect · tab URL/list · esc close")))
		panel := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(s.P.BorderActive).
			BorderBackground(s.P.BgElev).
			Background(s.P.BgElev).
			Padding(1, 2).Width(width + 2).
			Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
		return modalPanelBuild{panel: panel, rowFirstLine: rowFirstLine, rowStart: start, rowEnd: end, ok: end > start}
	}

	title, rows, footer := m.modalItems()

	// Window long lists around the selection so a provider with hundreds of
	// models (or many sessions) can't overflow the panel. The help overlay
	// (plan 08e §F3) is excepted: it lists the full keybind surface (~40
	// rows) and the value is discoverability — windowing would hide most of
	// the keybinds the user wants to discover. The panel grows to fit (up
	// to the screen height; centerScreen clamps it).
	const maxRows = 12
	windowMax := maxRows
	if m.modal == modalHelp {
		windowMax = len(rows)
	}
	start, end := windowAround(m.modalSel, len(rows), windowMax)

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
	rowFirstLine := len(lines)
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

	return modalPanelBuild{panel: panel, rowFirstLine: rowFirstLine, rowStart: start, rowEnd: end, ok: end > start}
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
	return centerScreen(m.width, m.height, m.buildModalPanel().panel)
}

// modalRowAtY maps a mouse position to a modalSel row index for hover/click
// (plan 08f H4 / G.3). It rebuilds the modal panel via buildModalPanel — the
// same code path modalView() renders — so the row geometry always matches
// what's on screen, then locates the panel's on-screen origin the same way
// lipgloss.Place centers it (centeredCardPos mirrors Place's Center math).
// Returns ok=false when the modal has no rows (e.g. the rename overlay) or
// the point falls outside the row band.
func (m Model) modalRowAtY(x, y int) (int, bool) {
	if m.modal == modalNone {
		return 0, false
	}
	b := m.buildModalPanel()
	if !b.ok {
		return 0, false
	}
	px, py, ok := centeredCardPos(m.width, m.height, b.panel)
	if !ok {
		return 0, false
	}
	pw, ph := lipgloss.Width(b.panel), lipgloss.Height(b.panel)
	if x < px || x >= px+pw || y < py || y >= py+ph {
		return 0, false
	}
	const borderAndPadTop = 2 // 1 border row + 1 Padding(1,2) top row
	contentLine := y - py - borderAndPadTop
	if contentLine < 0 {
		return 0, false
	}
	rowOffset := contentLine - b.rowFirstLine
	if rowOffset < 0 {
		return 0, false
	}
	row := b.rowStart + rowOffset
	if row < b.rowStart || row >= b.rowEnd {
		return 0, false
	}
	return row, true
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
