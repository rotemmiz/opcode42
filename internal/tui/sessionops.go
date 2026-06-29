package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

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
