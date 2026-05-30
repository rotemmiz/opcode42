package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	forgeclient "github.com/rotemmiz/forge/sdk/go"
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
}

// autocomplete is the composer's slash/command popup state (value type — copied
// with the Model, no shared pointer to alias across Bubble Tea updates).
type autocomplete struct {
	open  bool
	items []slashItem
	sel   int
}

const maxSlashRows = 8

// commandsLoadedMsg carries the daemon command list (GET /command).
type commandsLoadedMsg struct {
	items []slashItem
	err   error
}

// loadCommandsCmd fetches the daemon's commands and tags them as slashDaemon.
func loadCommandsCmd(ctx context.Context, c *forgeclient.ForgeClient) tea.Cmd {
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
func runCommandCmd(ctx context.Context, c *forgeclient.ForgeClient, sessionID, command, arguments string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/session/"+sessionID+"/command", commandBody{Command: command, Arguments: arguments}, nil)
		return promptSentMsg{err: err}
	}
}

// createSessionForCommandCmd creates a session, carrying a command to run next.
func createSessionForCommandCmd(ctx context.Context, c *forgeclient.ForgeClient, command, arguments string) tea.Cmd {
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

// refreshAutocomplete recomputes the popup from the composer text: open with the
// matching commands when the text is a single "/…" token, else closed.
func (m Model) refreshAutocomplete() Model {
	v := m.input.Value()
	if strings.HasPrefix(v, "/") && !strings.ContainsAny(v, "\n") {
		q := v[1:]
		if i := strings.IndexAny(q, " \t"); i >= 0 {
			q = q[:i] // only the command word filters; args come after a space
		}
		if items := filterSlash(q, m.commands); len(items) > 0 {
			if len(items) > maxSlashRows {
				items = items[:maxSlashRows]
			}
			sel := m.ac.sel
			if sel >= len(items) {
				sel = len(items) - 1
			}
			if sel < 0 {
				sel = 0
			}
			m.ac = autocomplete{open: true, items: items, sel: sel}
			return m
		}
	}
	m.ac = autocomplete{}
	return m
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
		if m.ac.sel < len(m.ac.items)-1 {
			m.ac.sel++
		}
		return true, m, nil
	case "esc":
		m.ac = autocomplete{}
		return true, m, nil
	case "tab":
		return true, m.completeSlash(), nil
	case "enter":
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
// (empty string when closed).
func (m Model) autocompleteView() string {
	if !m.ac.open || len(m.ac.items) == 0 {
		return ""
	}
	s := m.styles
	var lines []string
	for i, it := range m.ac.items {
		if i == m.ac.sel {
			lines = append(lines, s.Selection.Render(" "+it.name)+"  "+s.Faint.Render(it.desc))
		} else {
			lines = append(lines, "  "+s.Dim.Render(it.name)+"  "+s.Faint.Render(it.desc))
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(s.P.Border).
		Background(s.P.BgElev).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
