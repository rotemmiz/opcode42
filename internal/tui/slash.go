package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Slash commands: typing "/" in the composer opens an autocomplete popup of
// built-in TUI actions plus the daemon's commands (GET /command). Up/down move,
// tab completes the name, enter runs, esc dismisses (design app.jsx:249).

type slashKind int

const (
	slashBuiltin slashKind = iota // maps to a client action (a modal/new-session)
	slashDaemon                   // a daemon command, run via POST /session/:id/command
)

type slashItem struct {
	name string // "/new", "/init" (leading slash included)
	desc string
	kind slashKind
}

// builtinCommands are client-side actions surfaced as slash commands. They reuse
// the existing modals so "/models" == ctrl+p → Switch model, etc.
var builtinCommands = []slashItem{
	{name: "/new", desc: "Start a new session", kind: slashBuiltin},
	{name: "/sessions", desc: "Switch session", kind: slashBuiltin},
	{name: "/models", desc: "Switch model", kind: slashBuiltin},
	{name: "/agents", desc: "Switch agent", kind: slashBuiltin},
	{name: "/themes", desc: "Switch theme", kind: slashBuiltin},
	{name: "/timeline", desc: "Revert to a turn", kind: slashBuiltin},
	{name: "/diff", desc: "Review session changes", kind: slashBuiltin},
	{name: "/terminal", desc: "Open an embedded terminal", kind: slashBuiltin},
	{name: "/variant", desc: "Pick a model variant", kind: slashBuiltin},
	{name: "/stash", desc: "Stashed prompt drafts", kind: slashBuiltin},
	{name: "/status", desc: "Connection status", kind: slashBuiltin},
}

// acMode is what the composer popup is completing.
type acMode int

const (
	acSlash   acMode = iota // "/" commands (slash items)
	acMention               // "@" file references (file paths)
)

// autocomplete is the composer's slash/@-mention popup state (value type —
// copied with the Model, no shared pointer to alias across Bubble Tea updates).
type autocomplete struct {
	open  bool
	mode  acMode
	items []slashItem // acSlash
	files []string    // acMention
	sel   int
}

// count is the number of selectable rows in the active mode.
func (a autocomplete) count() int {
	if a.mode == acMention {
		return len(a.files)
	}
	return len(a.items)
}

const (
	maxSlashRows   = 8
	maxMentionRows = 8
)

// commandsLoadedMsg carries the daemon command list (GET /command).
type commandsLoadedMsg struct {
	items []slashItem
	err   error
}

// loadCommandsCmd fetches the daemon's commands and tags them as slashDaemon.
func loadCommandsCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		var raw []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := c.GetJSON(ctx, "/command", &raw); err != nil {
			return commandsLoadedMsg{err: err}
		}
		items := make([]slashItem, 0, len(raw))
		for _, r := range raw {
			if r.Name == "" {
				continue
			}
			items = append(items, slashItem{name: "/" + r.Name, desc: r.Description, kind: slashDaemon})
		}
		return commandsLoadedMsg{items: items}
	}
}

// commandBody is the POST /session/:id/command request (command + arguments are
// required; model/agent are left to the daemon/agent default).
type commandBody struct {
	Command   string `json:"command"`
	Arguments string `json:"arguments"`
}

// runCommandCmd runs a daemon command in an existing session.
func runCommandCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID, command, arguments string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/session/"+sessionID+"/command", commandBody{Command: command, Arguments: arguments}, nil)
		return promptSentMsg{err: err}
	}
}

// createSessionForCommandCmd creates a session, carrying a command to run next.
func createSessionForCommandCmd(ctx context.Context, c *opcode42client.Opcode42Client, command, arguments string) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.PostJSON(ctx, "/session", map[string]any{}, &ss)
		return sessionCreatedMsg{session: ss, command: command, arguments: arguments, err: err}
	}
}

// filterSlash returns the built-in + daemon commands whose name (minus the
// leading slash) starts with q, built-ins first.
func filterSlash(q string, daemon []slashItem) []slashItem {
	q = strings.ToLower(q)
	var out []slashItem
	for _, group := range [][]slashItem{builtinCommands, daemon} {
		for _, it := range group {
			if strings.HasPrefix(strings.ToLower(it.name[1:]), q) {
				out = append(out, it)
			}
		}
	}
	return out
}

// slashArguments is the text after the command word (the $ARGUMENTS payload).
func slashArguments(text string) string {
	text = strings.TrimPrefix(text, "/")
	if i := strings.IndexAny(text, " \t"); i >= 0 {
		return strings.TrimSpace(text[i+1:])
	}
	return ""
}

// refreshAutocomplete recomputes the popup from the composer text: a single "/…"
// token opens the (synchronous) slash list; a trailing "@token" opens the
// (asynchronous) file picker by dispatching a search; otherwise it closes.
func (m Model) refreshAutocomplete() (Model, tea.Cmd) {
	v := m.input.Value()

	// Slash: the whole value is one "/command" token.
	if strings.HasPrefix(v, "/") && !strings.ContainsAny(v, "\n") {
		q := v[1:]
		if i := strings.IndexAny(q, " \t"); i >= 0 {
			q = q[:i] // only the command word filters; args come after a space
		}
		if items := filterSlash(q, m.commands); len(items) > 0 {
			if len(items) > maxSlashRows {
				items = items[:maxSlashRows]
			}
			m.ac = autocomplete{open: true, mode: acSlash, items: items, sel: clampSel(m.ac.sel, len(items))}
			return m, nil
		}
	}

	// Mention: a trailing "@token" anywhere in the text → async file search. Stay
	// open only while we have results to show (no invisible key-capturing popup);
	// filesFoundMsg opens/closes it when the search returns.
	if q, ok := mentionQuery(v); ok {
		m.ac = autocomplete{open: m.ac.mode == acMention && len(m.ac.files) > 0, mode: acMention, files: m.ac.files, sel: clampSel(m.ac.sel, len(m.ac.files))}
		return m, findFilesCmd(m.ctx, m.client, q)
	}

	m.ac = autocomplete{}
	return m, nil
}

// clampSel keeps a selection index within [0, n-1] (0 when empty).
func clampSel(sel, n int) int {
	if sel >= n {
		sel = n - 1
	}
	if sel < 0 {
		sel = 0
	}
	return sel
}

// handleAutocompleteKey consumes navigation/accept/dismiss keys while the popup
// is open. It returns handled=false for any other key so typing keeps filtering.
func (m Model) handleAutocompleteKey(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "ctrl+p":
		if m.ac.sel > 0 {
			m.ac.sel--
		}
		return true, m, nil
	case "down", "ctrl+n":
		if m.ac.sel < m.ac.count()-1 {
			m.ac.sel++
		}
		return true, m, nil
	case "esc":
		m.ac = autocomplete{}
		return true, m, nil
	case "tab":
		if m.ac.mode == acMention {
			return true, m.acceptMention(), nil
		}
		return true, m.completeSlash(), nil
	case "enter":
		if m.ac.mode == acMention {
			return true, m.acceptMention(), nil
		}
		nm, cmd := m.acceptSlash()
		return true, nm, cmd
	}
	return false, m, nil
}

// completeSlash fills the composer with the selected command name (keeping the
// popup closed) so the user can type arguments after it.
func (m Model) completeSlash() Model {
	if m.ac.sel >= len(m.ac.items) {
		return m
	}
	m.input.SetValue(m.ac.items[m.ac.sel].name + " ")
	m.input.CursorEnd()
	m.ac = autocomplete{}
	return m.resizeComposer()
}

// acceptSlash runs the selected command: a built-in dispatches its client action;
// a daemon command runs via POST /session/:id/command (creating a session first
// if none is open).
func (m Model) acceptSlash() (tea.Model, tea.Cmd) {
	if m.ac.sel >= len(m.ac.items) {
		m.ac = autocomplete{}
		return m, nil
	}
	it := m.ac.items[m.ac.sel]
	args := slashArguments(m.input.Value())
	m.ac = autocomplete{}
	m.input.SetValue("")
	m = m.resizeComposer()

	if it.kind == slashBuiltin {
		switch it.name {
		case "/new":
			return m, newSessionCmd(m.ctx, m.client)
		case "/sessions":
			m.modal, m.modalSel = modalSessions, 0
			return m, loadSessionsCmd(m.ctx, m.client)
		case "/models":
			m.modal, m.modalSel = modalModels, m.modelSelIndex()
			return m, loadProvidersCmd(m.ctx, m.client)
		case "/agents":
			m.modal, m.modalSel = modalAgents, m.agentSelIndex()
			return m, loadAgentsCmd(m.ctx, m.client)
		case "/themes":
			m.modal, m.modalSel = modalThemes, m.themeSelIndex()
			return m, nil
		case "/timeline":
			m.modal, m.modalSel = modalTimeline, 0
			return m, nil
		case "/diff":
			return m.openDiff()
		case "/terminal":
			return m.focusOrOpenPTY()
		case "/variant":
			m.modal, m.modalSel = modalVariant, m.variantSelIndex()
			return m, nil
		case "/stash":
			m.modal, m.modalSel = modalStash, 0
			return m, nil
		case "/status":
			m.modal, m.modalSel = modalStatus, 0
			return m, nil
		}
		return m, nil
	}

	command := it.name[1:]
	if m.cfg.SessionID == "" {
		return m, createSessionForCommandCmd(m.ctx, m.client, command, args)
	}
	return m, runCommandCmd(m.ctx, m.client, m.cfg.SessionID, command, args)
}

// autocompleteView renders the popup as a left-aligned panel above the composer
// (empty string when closed/empty).
func (m Model) autocompleteView() string {
	if !m.ac.open {
		return ""
	}
	s := m.styles

	// Build (name, desc) rows, then size the panel to the widest so the selection
	// bar spans the full row consistently.
	type acRow struct{ name, desc string }
	var rows []acRow
	switch m.ac.mode {
	case acMention:
		if len(m.ac.files) == 0 {
			return "" // nothing matched (or still searching) — no panel
		}
		for _, f := range m.ac.files {
			rows = append(rows, acRow{name: truncate(f, 58)})
		}
	default: // acSlash
		if len(m.ac.items) == 0 {
			return ""
		}
		for _, it := range m.ac.items {
			rows = append(rows, acRow{name: it.name, desc: it.desc})
		}
	}

	inner := 0
	for _, r := range rows {
		w := lipgloss.Width(r.name)
		if r.desc != "" {
			w += 2 + lipgloss.Width(r.desc)
		}
		if w > inner {
			inner = w
		}
	}
	inner++ // a leading space before each row
	if inner > 60 {
		inner = 60
	}

	var lines []string
	for i, r := range rows {
		if i == m.ac.sel { // full-width amber bar over the whole row
			plain := r.name
			if r.desc != "" {
				plain += "  " + r.desc
			}
			lines = append(lines, s.Selection.Width(inner).Render(" "+plain))
		} else {
			line := " " + s.Base.Render(r.name)
			if r.desc != "" {
				line += "  " + s.Faint.Render(r.desc)
			}
			lines = append(lines, line)
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(s.P.Border).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
