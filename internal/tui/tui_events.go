package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Server→TUI control events (plan 08f H16 / G.18): tui.toast.show,
// tui.session.select, tui.command.execute, tui.prompt.append.
// Mirrors opencode app.tsx:971-992 + prompt/index.tsx:233-244.
// Workspace scoping is a no-op here (Opcode42 has no multi-workspace TUI).

// handleTUIControlEvent applies a server-pushed tui.* event. Returns ok=false
// when ev.Type is not a control event (caller continues normal Reduce).
func (m Model) handleTUIControlEvent(ev opcode42client.SSEEvent) (Model, tea.Cmd, bool) {
	switch ev.Type {
	case "tui.toast.show":
		var p struct {
			Title   string `json:"title"`
			Message string `json:"message"`
			Variant string `json:"variant"`
		}
		if !decode(ev.Properties, &p) || strings.TrimSpace(p.Message) == "" {
			return m, nil, true
		}
		text := p.Message
		if t := strings.TrimSpace(p.Title); t != "" {
			text = t + ": " + text
		}
		kind := toastInfo
		switch p.Variant {
		case "success":
			kind = toastSuccess
		case "error", "warning":
			kind = toastError
		}
		cmd := m.pushToast(kind, text)
		m = m.rerenderChrome()
		return m, cmd, true

	case "tui.session.select":
		var p struct {
			SessionID string `json:"sessionID"`
		}
		if !decode(ev.Properties, &p) || p.SessionID == "" {
			return m, nil, true
		}
		nm, cmd := m.openSession(p.SessionID)
		nm = nm.rerenderFull()
		return nm, cmd, true

	case "tui.prompt.append":
		var p struct {
			Text string `json:"text"`
		}
		if !decode(ev.Properties, &p) || p.Text == "" {
			return m, nil, true
		}
		// insert-at-end (opencode insertText then gotoBufferEnd).
		m.input.SetValue(m.input.Value() + p.Text)
		m.input.CursorEnd()
		m = m.resizeComposer()
		m = m.rerenderChrome()
		return m, nil, true

	case "tui.command.execute":
		var p struct {
			Command string `json:"command"`
		}
		if !decode(ev.Properties, &p) || p.Command == "" {
			return m, nil, true
		}
		nm, cmd := m.executeTUICommand(p.Command)
		return nm, cmd, true
	}
	return m, nil, false
}

// executeTUICommand dispatches a small registry of server-pushed command names
// (schema TuiEvent.CommandExecute literals + a few Opcode42 palette aliases).
func (m Model) executeTUICommand(name string) (Model, tea.Cmd) {
	page := m.scrollBodyHeight()
	if page < 1 {
		page = 1
	}
	step := m.scrollLines()
	switch name {
	case "session.list":
		m.modal, m.modalSel = modalSessions, 0
		m = m.rerenderChrome()
		return m, loadSessionsCmd(m.ctx, m.client)
	case "session.new":
		return m, newSessionCmd(m.ctx, m.client)
	case "session.share":
		return m.shareOrCopyLink()
	case "session.interrupt":
		if m.cfg.SessionID == "" {
			return m, nil
		}
		m.status = "interrupting…"
		m = m.rerenderChrome()
		return m, abortSessionCmd(m.ctx, m.client, m.cfg.SessionID)
	case "session.compact":
		return m.compactSession()
	case "session.page.up":
		m.scroll.Back(page)
		m = m.rerenderFull()
		return m, nil
	case "session.page.down":
		m.scroll.Forward(page)
		m = m.rerenderFull()
		return m, nil
	case "session.line.up":
		m.scroll.Back(step)
		m = m.rerenderFull()
		return m, nil
	case "session.line.down":
		m.scroll.Forward(step)
		m = m.rerenderFull()
		return m, nil
	case "session.half.page.up":
		m.scroll.Back(page / 2)
		m = m.rerenderFull()
		return m, nil
	case "session.half.page.down":
		m.scroll.Forward(page / 2)
		m = m.rerenderFull()
		return m, nil
	case "session.first":
		m.scroll.Offset = 1 << 30
		m = m.rerenderFull()
		return m, nil
	case "session.last":
		m.scroll.ToTail()
		m = m.rerenderFull()
		return m, nil
	case "prompt.clear":
		m.input.SetValue("")
		m = m.resizeComposer()
		m = m.rerenderChrome()
		return m, nil
	case "prompt.submit":
		next, cmd := m.submit()
		return next.(Model), cmd
	case "agent.cycle":
		if len(m.agents) == 0 {
			return m, loadAgentsCmd(m.ctx, m.client)
		}
		i := m.agentSelIndex()
		next := (i + 1) % len(m.agents)
		m.agent = m.agents[next].name
		m = m.rerenderChrome()
		return m, nil
	default:
		m.status = "unknown tui command: " + name
		m = m.rerenderChrome()
		return m, nil
	}
}
