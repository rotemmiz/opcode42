package tui

import (
	"context"
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	forgeclient "github.com/rotemmiz/forge/sdk/go"
)

// U10 — permission overlay. A `permission.asked` SSE event yields a pending
// Permission in the store; the TUI blocks on it with an allow-once / allow-always
// / reject overlay and replies via POST /permission/:id/reply. The daemon's
// `permission.replied` event clears it (we also clear optimistically).

// permChoices are the reply options, in display order; the value is the wire
// `reply` field for POST /permission/:id/reply.
var permChoices = []struct {
	label string
	reply string
}{
	{"Allow once", "once"},
	{"Allow always", "always"},
	{"Reject", "reject"},
}

// pendingPermission is the permission currently awaiting a reply (the oldest), or
// nil when there is none.
func (m Model) pendingPermission() *Permission {
	if len(m.store.permissions) == 0 {
		return nil
	}
	return &m.store.permissions[0]
}

// permissionRepliedMsg is the result of replying to a permission.
type permissionRepliedMsg struct {
	id  string
	err error
}

// replyPermissionCmd answers a permission request.
func replyPermissionCmd(ctx context.Context, c *forgeclient.ForgeClient, id, reply string) tea.Cmd {
	return func() tea.Msg {
		err := c.PostJSON(ctx, "/permission/"+id+"/reply", map[string]string{"reply": reply}, nil)
		return permissionRepliedMsg{id: id, err: err}
	}
}

// handlePermissionKey drives the blocking overlay: ↑/↓ move, enter sends the
// selection, and a/s/r are shortcuts (allow once / allow always / reject).
func (m Model) handlePermissionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.pendingPermission()
	if p == nil || m.permReplying { // ignore keys while a reply is in flight
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		if m.permSel > 0 {
			m.permSel--
		}
		return m, nil
	case "down", "j":
		if m.permSel < len(permChoices)-1 {
			m.permSel++
		}
		return m, nil
	case "a":
		return m.replyPermission(p.ID, "once")
	case "s":
		return m.replyPermission(p.ID, "always")
	case "r", "esc":
		return m.replyPermission(p.ID, "reject")
	case "enter":
		return m.replyPermission(p.ID, permChoices[m.permSel].reply)
	}
	return m, nil
}

// replyPermission sends the reply. The request is NOT removed yet: the overlay
// stays up through the round-trip so a failed POST can't silently drop a request
// the daemon is still blocked on — it's cleared on success (or by the
// permission.replied SSE event).
func (m Model) replyPermission(id, reply string) (tea.Model, tea.Cmd) {
	m.permReplying = true
	return m, replyPermissionCmd(m.ctx, m.client, id, reply)
}

// permissionView renders the blocking overlay (centered card).
func (m Model) permissionView() string {
	p := m.pendingPermission()
	if p == nil {
		return ""
	}
	s := m.styles
	width := 60

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Foreground(s.P.Amber).Bold(true).Render("Permission required"), "")
	lines = append(lines, s.Base.Render(truncate(permissionTitle(*p), width-2)))
	if detail := permissionDetail(*p); detail != "" {
		lines = append(lines, s.Faint.Render(truncate(detail, width-2)))
	}
	lines = append(lines, "")
	for i, c := range permChoices {
		if i == m.permSel {
			lines = append(lines, s.Selection.Width(width-2).Render(" "+c.label))
		} else {
			lines = append(lines, s.Base.Render("  "+c.label))
		}
	}
	hint := "a allow · s always · r reject · enter select"
	if m.permReplying {
		hint = "sending…"
	}
	lines = append(lines, "", s.Faint.Render(hint))

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.P.Amber).
		Background(s.P.BgElev).
		Padding(1, 2).
		Width(width).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))

	if m.width == 0 || m.height == 0 {
		return card
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

// permissionTitle is a human line for the request (the action + tool).
func permissionTitle(p Permission) string {
	if p.Permission != "" {
		return p.Permission
	}
	return "tool wants to run"
}

// permissionDetail pulls a readable detail out of the metadata (command, path…).
func permissionDetail(p Permission) string {
	if len(p.Metadata) == 0 {
		return ""
	}
	var meta map[string]any
	if json.Unmarshal(p.Metadata, &meta) != nil {
		return ""
	}
	for _, k := range []string{"command", "filePath", "path", "title", "pattern", "url"} {
		if v, ok := meta[k]; ok {
			if str, ok := v.(string); ok && strings.TrimSpace(str) != "" {
				return str
			}
		}
	}
	return ""
}
