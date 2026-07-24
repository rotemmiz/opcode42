package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Startup-arg helpers (plan 08f H14 / G.16): --continue, --fork, --prompt,
// --agent, --auto. Mirrors opencode app.tsx:496-533 + home.tsx:53-68.

// mostRecentParentSessionID returns the first parent (non-child) session id
// from a newest-first list, matching opencode's
// session.toSorted(...).find(x => x.parentID === undefined) (app.tsx:500-502).
func mostRecentParentSessionID(sessions []Session) string {
	for _, s := range sessions {
		if s.ParentID == "" && s.ID != "" {
			return s.ID
		}
	}
	return ""
}

// applyStartupSessionArgs resolves --continue / --session / --fork against the
// freshly loaded session list. Returns the model plus an optional fork cmd.
// Sets startupSessionReady when navigation is complete (or no fork pending).
func (m Model) applyStartupSessionArgs(sessions []Session) (Model, tea.Cmd) {
	if m.cfg.Continue && m.cfg.SessionID == "" {
		if id := mostRecentParentSessionID(sessions); id != "" {
			m.cfg.SessionID = id
		}
	}
	if m.cfg.Fork && !m.startupForkDone && m.cfg.SessionID != "" {
		m.startupForkDone = true
		// Session route completes on forkedMsg — do not mark ready yet.
		return m, forkSessionCmd(m.ctx, m.client, m.cfg.SessionID, "", "")
	}
	m.startupSessionReady = true
	return m, nil
}

// maybeSubmitStartupPrompt auto-submits Config.Prompt once a model is ready
// and any --continue/--fork/--session resolution has finished (home.tsx:58-68
// waits for sync.ready). Clears the armed flag so it only fires once.
func (m Model) maybeSubmitStartupPrompt() (tea.Model, tea.Cmd, bool) {
	if !m.startupPromptArmed {
		return m, nil, false
	}
	if !m.model.ok() {
		return m, nil, false
	}
	// Wait for session/fork resolution when those flags were requested.
	needsSession := m.cfg.Continue || m.cfg.Fork || m.cfg.SessionID != ""
	if needsSession && !m.startupSessionReady {
		return m, nil, false
	}
	if strings.TrimSpace(m.input.Value()) == "" && len(m.pendingFiles) == 0 {
		m.startupPromptArmed = false
		return m, nil, false
	}
	m.startupPromptArmed = false
	next, cmd := m.submit()
	return next, cmd, true
}
