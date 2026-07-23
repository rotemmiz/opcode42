package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Session-operation commands (plan 08a §A). Each is a thin wrapper over the SDK;
// most also emit a session.updated/deleted SSE the store reducer already handles,
// so the returned message mainly carries errors + immediate state for the UI.

type (
	renamedMsg struct {
		session Session
		err     error
	}
	sharedMsg struct {
		session Session
		shared  bool // true = shared (POST), false = unshared (DELETE)
		err     error
	}
	summarizedMsg struct{ err error }
	abortedMsg    struct{ err error }
	forkedMsg     struct {
		session Session
		err     error
	}
)

// renameSessionCmd sets a session's title (PATCH /session/{id}).
func renameSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id, title string) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.PatchJSON(ctx, "/session/"+id, map[string]any{"title": title}, &ss)
		return renamedMsg{session: ss, err: err}
	}
}

// shareSessionCmd publishes a share link (POST /session/{id}/share → Session).
func shareSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.PostJSON(ctx, "/session/"+id+"/share", map[string]any{}, &ss)
		return sharedMsg{session: ss, shared: true, err: err}
	}
}

// unshareSessionCmd revokes the share link (DELETE /session/{id}/share → Session).
func unshareSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.DeleteJSON(ctx, "/session/"+id+"/share", &ss)
		return sharedMsg{session: ss, shared: false, err: err}
	}
}

// summarizeSessionCmd compacts the context (POST /session/{id}/summarize). The
// endpoint requires a model; pass the effective prompt model.
func summarizeSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string, pm promptModel) tea.Cmd {
	return func() tea.Msg {
		body := map[string]any{"providerID": pm.Provider, "modelID": pm.Model}
		return summarizedMsg{err: c.PostJSON(ctx, "/session/"+id+"/summarize", body, nil)}
	}
}

// abortSessionCmd interrupts a running turn (POST /session/{id}/abort).
func abortSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string) tea.Cmd {
	return func() tea.Msg {
		return abortedMsg{err: c.PostJSON(ctx, "/session/"+id+"/abort", map[string]any{}, nil)}
	}
}

// forkSessionCmd branches a session (POST /session/{id}/fork → new Session).
func forkSessionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id string) tea.Cmd {
	return func() tea.Msg {
		var ss Session
		err := c.PostJSON(ctx, "/session/"+id+"/fork", map[string]any{}, &ss)
		return forkedMsg{session: ss, err: err}
	}
}

// openRename opens the rename text-input overlay for the current session
// (plan 08f H1a — ctrl+r / palette Rename / future slash).
func (m Model) openRename() (Model, tea.Cmd) {
	if m.cfg.SessionID == "" {
		m.status = "no session to rename"
		m = m.rerenderChrome()
		return m, nil
	}
	m.modal = modalRename
	if cur := m.currentSession(); cur != nil {
		m.renameInput.SetValue(cur.Title)
	}
	m.renameInput.CursorEnd()
	m.renameInput.Focus()
	return m, nil
}

// confirmDeleteSession implements the two-press ctrl+d delete guard
// (plan 08f H1a). First press arms; second deletes.
func (m Model) confirmDeleteSession() (Model, tea.Cmd) {
	if m.cfg.SessionID == "" {
		m.status = "no session to delete"
		m = m.rerenderChrome()
		return m, nil
	}
	if m.deleting {
		m.deleting = false
		m.status = "deleting…"
		m = m.rerenderChrome()
		return m, deleteSessionCmd(m.ctx, m.client, m.cfg.SessionID)
	}
	m.deleting = true
	m.status = "press ctrl+d again to delete session"
	m = m.rerenderChrome()
	return m, exitTickCmd() // auto-cancel after 5s (same timer as exit guard)
}

// compactSession summarizes/compacts the open session (opencode <leader>c /
// plan 08f H1a). Requires a resolved model.
func (m Model) compactSession() (Model, tea.Cmd) {
	if m.cfg.SessionID == "" || !m.model.ok() {
		m.status = "summarize needs an open session + model"
		m = m.rerenderChrome()
		return m, nil
	}
	m.status = "summarizing…"
	m = m.rerenderChrome()
	return m, summarizeSessionCmd(m.ctx, m.client, m.cfg.SessionID, m.model)
}

// shareOrCopyLink shares the session (or copies an existing share URL).
func (m Model) shareOrCopyLink() (Model, tea.Cmd) {
	if m.cfg.SessionID == "" {
		m.status = "no session to share"
		m = m.rerenderChrome()
		return m, nil
	}
	cur := m.currentSession()
	if cur != nil && cur.Share != nil && cur.Share.URL != "" {
		m.status = "shared · " + cur.Share.URL + " (copied)"
		m = m.rerenderChrome()
		return m, copyClipboardCmd(cur.Share.URL, m.osc52Enabled)
	}
	m.status = "sharing…"
	m = m.rerenderChrome()
	return m, shareSessionCmd(m.ctx, m.client, m.cfg.SessionID)
}

// unshareCurrent revokes the share link for the open session.
func (m Model) unshareCurrent() (Model, tea.Cmd) {
	if m.cfg.SessionID == "" {
		return m, nil
	}
	return m, unshareSessionCmd(m.ctx, m.client, m.cfg.SessionID)
}

// copyTranscript copies the full open-session transcript to the clipboard
// (plan 08f H1a /export + /copy).
func (m Model) copyTranscript() (Model, tea.Cmd) {
	txt := m.formatTranscript()
	if txt == "" {
		m.status = "nothing to copy"
		m = m.rerenderChrome()
		return m, nil
	}
	m.status = "copied transcript"
	m = m.rerenderChrome()
	return m, copyClipboardCmd(txt, m.osc52Enabled)
}
