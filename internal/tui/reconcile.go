package tui

import (
	"context"
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// E3 — reconcile-on-reconnect for pending permissions/questions. When the
// daemon cancels a question/permission without publishing an SSE event (agent
// finalizer, or another client answers/rejects it), the TUI's store holds a
// stale entry. On reconnect and on session.status→idle for the open session we
// re-fetch GET /permission + GET /question and REPLACE the store's slices with
// the daemon's current pending list (mirrors Android StoreReducer.kt:115-116).
// A transient fetch failure leaves the store unchanged — a flaky GET must not
// wipe the UI.

// permissionsReconciledMsg carries the freshly-fetched pending permissions.
type permissionsReconciledMsg struct {
	permissions []Permission
	err         error
}

// questionsReconciledMsg carries the freshly-fetched pending questions.
type questionsReconciledMsg struct {
	questions []Question
	err       error
}

// reconcilePendingCmd fires GET /permission and GET /question in parallel and
// returns a batch of the two reconciled messages. Each call decodes its own
// response so a failure on one endpoint doesn't blank the other (matches
// Android SessionRepository.reconcilePending's runCatching per call).
func reconcilePendingCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			var perms []Permission
			err := c.GetJSON(ctx, "/permission", &perms)
			return permissionsReconciledMsg{permissions: perms, err: err}
		},
		func() tea.Msg {
			var qs []Question
			err := c.GetJSON(ctx, "/question", &qs)
			return questionsReconciledMsg{questions: qs, err: err}
		},
	)
}

// isHTTPNotFound reports whether err is an HTTP 404 from the SDK's GetJSON /
// PostJSON wrappers (which format non-2xx as "<METHOD> <path>: status <code>").
// Used to swallow stale-tap 404s on the permission/question reply endpoints
// silently (plan 16 Bug 1): the question/permission was already answered or
// cancelled elsewhere, so the optimistic clear already removed it from the UI.
func isHTTPNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "status 404")
}

// isSessionIdleFor reports whether a `session.status` SSE event carries an
// idle transition for sessionID. The wire shape is
// {sessionID, status: {type: "idle"|"busy"|"retry"}} (engine.go emitStatus /
// opencode session-status-event.ts). The plan 08e §E3 text says "session.updated
// with status == idle", but the daemon emits idle via `session.status`, not
// `session.updated` (which carries full session info, no status field) —
// reconciling on the actual idle event is the correct port of Android's
// session.status → idle watcher (plan 16 Bug 3 3b).
func isSessionIdleFor(ev opcode42client.SSEEvent, sessionID string) bool {
	if ev.Type != "session.status" || len(ev.Properties) == 0 {
		return false
	}
	var p struct {
		SessionID string `json:"sessionID"`
		Status    struct {
			Type string `json:"type"`
		} `json:"status"`
	}
	if json.Unmarshal(ev.Properties, &p) != nil {
		return false
	}
	return p.SessionID == sessionID && p.Status.Type == "idle"
}
