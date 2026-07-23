package tui

import (
	"context"
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// permission.go — Plan 17 §B: the permission footer panel.
//
// opencode renders permission as a footer-region panel (bottom of screen),
// NOT a centered modal (run/footer.view.tsx:778-794). The panel body carries
// the theme's surface background (BgElev, Opcode42's equivalent of opencode's
// `surface`); the outer footer container is transparent when a panel is
// active (footer.view.tsx:663) and the left accent border is removed
// (footer.view.tsx:645).
//
// The panel has a 3-stage state machine (permission.shared.ts:22-23):
//
//	permission → Allow once / Allow always / Reject options
//	always     → confirmation step (Confirm / Cancel) showing the patterns
//	             that will be allowed until OpenCode is restarted
//	reject     → text input for the rejection message
//
// Keyboard (footer.permission.tsx:214-257):
//
//	tab / shift+tab / left / h / right / l — shift selection
//	return / enter                         — confirm selected
//	esc                                    — escape (→ reject stage, or cancel
//	                                         from always stage)
//
// No y/n shortcuts, no 1/2/3 digit shortcuts. Selection is tab/arrows + enter
// (plan 17 §B2 — matching opencode, replacing the prior a/s/r shortcuts).
//
// For edit/apply_patch permissions, the panel shows the unified diff inline
// (footer.permission.tsx:373-391) using the shared renderUnifiedDiff helper
// from Workstream C (diffrender.go).

// pendingScopeIDs returns the session IDs whose pending permission/question
// prompts surface in the current view (plan 08f §PC.3 / G.21).
//
// Mirrors opencode routes/session/index.tsx:207-236:
//   - no open session → empty (splash/home shows no overlay)
//   - open session is a child (ParentID != "") → empty (child views do not
//     aggregate; prompts are answered from the parent)
//   - open session is a parent/root → {open} ∪ direct children (flat, one level)
func (m Model) pendingScopeIDs() map[string]bool {
	sid := m.cfg.SessionID
	if sid == "" {
		return nil
	}
	for _, s := range m.store.sessions {
		if s.ID == sid && s.ParentID != "" {
			return nil
		}
	}
	out := map[string]bool{sid: true}
	for _, c := range m.childrenOf(sid) {
		out[c.ID] = true
	}
	return out
}

// pendingPermission is the oldest in-scope permission awaiting a reply, or
// nil when there is none. Scope is parent+direct-children when viewing a
// parent (plan 08f H18); see pendingScopeIDs.
func (m Model) pendingPermission() *Permission {
	scope := m.pendingScopeIDs()
	if len(scope) == 0 {
		return nil
	}
	for i := range m.store.permissions {
		if scope[m.store.permissions[i].SessionID] {
			return &m.store.permissions[i]
		}
	}
	return nil
}

// permissionRepliedMsg is the result of replying to a permission.
type permissionRepliedMsg struct {
	id  string
	err error
}

// replyPermissionCmd answers a permission request. The message is included
// when non-empty (the reject stage's textarea content); opencode's wire
// accepts an optional `message` field (PermissionReplyJSONBody.message).
func replyPermissionCmd(ctx context.Context, c *opcode42client.Opcode42Client, id, reply, message string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]string{"reply": reply}
		if strings.TrimSpace(message) != "" {
			body["message"] = strings.TrimSpace(message)
		}
		err := c.PostJSON(ctx, "/permission/"+id+"/reply", body, nil)
		return permissionRepliedMsg{id: id, err: err}
	}
}

// handlePermissionKey drives the 3-stage footer panel. The state machine
// lives in m.permState (permission_state.go); the key path applies pure
// transitions and dispatches the reply when a final option is confirmed.
//
// Keys (matching opencode footer.permission.tsx:214-257):
//   - tab / shift+tab / left / h / right / l — shift selection
//   - return / enter — confirm selected (permRun)
//   - esc — escape (permEscape): always stage → permission; otherwise → reject stage
//   - in the reject stage the textarea owns the keys (only esc cancels)
//
// The reject stage's textarea content is captured in m.permState.message;
// enter from the reject stage sends the reject reply with the message.
func (m Model) handlePermissionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.pendingPermission()
	if p == nil || m.permState.replying {
		return m, nil
	}
	// Reset the state when the active pending permission changed (so a stale
	// state from a prior request doesn't bleed into a new one).
	if p.ID != m.permRequestID {
		m.permState = newPermissionState()
		m.permRequestID = p.ID
	}

	if m.permState.stage == permStageReject {
		return m.handlePermissionRejectKey(msg, p)
	}

	switch msg.String() {
	case "tab":
		m.permState = permShift(m.permState, +1)
		return m, nil
	case "shift+tab":
		m.permState = permShift(m.permState, -1)
		return m, nil
	case "left", "h":
		m.permState = permShift(m.permState, -1)
		return m, nil
	case "right", "l":
		m.permState = permShift(m.permState, +1)
		return m, nil
	case "enter":
		return m.permissionConfirm(p, m.permState.selected)
	case "esc":
		m.permState = permEscape(m.permState)
		return m, nil
	}
	return m, nil
}

// handlePermissionRejectKey handles keys in the reject stage (the textarea
// owns the keys). enter sends the reject reply with the typed message; esc
// cancels back to the permission stage.
func (m Model) handlePermissionRejectKey(msg tea.KeyMsg, p *Permission) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.permState = permSetReplying(m.permState, true)
		reply := "reject"
		message := m.permState.message
		return m, replyPermissionCmd(m.ctx, m.client, p.ID, reply, message)
	case "esc":
		m.permState = permCancelReject(m.permState)
		return m, nil
	default:
		// Append printable characters to the reject message. This is a
		// minimal textarea; opencode uses a real TextareaRenderable with
		// word-wrap and a 3-row max height (footer.permission.tsx:97-128).
		// Opcode42 keeps a single-line string; backspace deletes the last
		// rune.
		switch msg.String() {
		case "backspace", "backspace2":
			if len(m.permState.message) > 0 {
				r := []rune(m.permState.message)
				m.permState = permSetMessage(m.permState, string(r[:len(r)-1]))
			}
			return m, nil
		}
		k := msg.Key()
		if k.Text != "" && k.Code >= 32 {
			m.permState = permSetMessage(m.permState, m.permState.message+k.Text)
		}
		return m, nil
	}
}

// permSetReplying marks the permState as replying without changing other
// fields (the pure transitions handle it, but the reject-send path is
// dispatched from the key handler directly).
func permSetReplying(s permissionState, replying bool) permissionState {
	s.replying = replying
	return s
}

// permissionConfirm confirms the selected option in the current stage. For
// options that transition (always → always stage; reject → reject stage) the
// state is updated and no reply is sent. For final options (once, confirm in
// always stage) the reply is dispatched.
func (m Model) permissionConfirm(p *Permission, opt permOption) (tea.Model, tea.Cmd) {
	next, reply := permRun(m.permState, opt)
	m.permState = next
	if reply == "" {
		return m, nil
	}
	m.permState = permSetReplying(m.permState, true)
	return m, replyPermissionCmd(m.ctx, m.client, p.ID, reply, "")
}

// permissionView renders the footer panel (plan 17 §B1): the panel body,
// sized leftW × panelH, styled with BgElev background. The panel is positioned
// at the bottom of the screen by the canvas (overlayLayers). The 3-stage
// flow renders different content per stage (permission/always/reject).
func (m Model) permissionView() string {
	p := m.pendingPermission()
	if p == nil {
		return ""
	}
	// Reset the state when the active pending permission changed (mirrors
	// handlePermissionKey's reset, for the render path).
	if p.ID != m.permRequestID {
		m.permState = newPermissionState()
		m.permRequestID = p.ID
	}
	s := m.styles
	leftW := m.leftColumnWidth()
	if leftW < 1 {
		leftW = 1
	}
	// Plan 18 §B2 (review fix): the panel renders at the gutter-reduced
	// innerW so it aligns with the stream surface (canvas places it at
	// X(streamGutter)). The internal stage lines keep their own 1-col pad.
	innerW := leftW - 2*streamGutter
	if innerW < 1 {
		innerW = 1
	}

	var lines []string
	switch m.permState.stage {
	case permStagePermission:
		lines = m.permissionStageLines(p, innerW)
	case permStageAlways:
		lines = m.permissionAlwaysStageLines(p, innerW)
	case permStageReject:
		lines = m.permissionRejectStageLines(innerW)
	}

	// Hint line (matches opencode's footer hint at footer.permission.tsx:457-466).
	hint := "⇆ select   enter confirm   esc " + permEscHint(m.permState.stage)
	if m.permState.replying {
		hint = "Waiting for permission event..."
	}
	lines = append(lines, "")
	lines = append(lines, s.Faint.Render(hint))

	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	// Panel body: innerW width (gutter-reduced), BgElev surface bg. The
	// canvas positions the panel at X(streamGutter) so the panel surface
	// aligns with the stream surface (plan 18 §B2 review fix).
	// Pad to the target height (plan 17 §B1): panelH = base + PERMISSION_ROWS.
	// opencode's renderer reserves a fixed height regardless of content
	// (footer.ts:697-722); Opcode42 pads the body to the same height so
	// the panel is a stable footer region (not content-sized).
	panel := s.Surface(s.P.BgElev).Width(innerW).Render(body)
	if h := m.permissionPanelHeight(); h > lipgloss.Height(panel) {
		panel = padVertical(panel, h, s.P.BgElev)
	}
	return panel
}

// padVertical pads a rendered block to the target height by appending
// BgElev-styled blank rows. Used by the footer panels to hit the fixed
// panel height (base + ROWS).
func padVertical(block string, targetH int, bg theme.Color) string {
	h := lipgloss.Height(block)
	if h >= targetH {
		return block
	}
	rows := strings.Split(block, "\n")
	pad := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", lipgloss.Width(block)))
	for h < targetH {
		rows = append(rows, pad)
		h++
	}
	return strings.Join(rows, "\n")
}

// permEscHint returns the esc-action label for the hint line
// (footer.permission.tsx:464): "cancel" from the always stage, "reject"
// otherwise.
func permEscHint(stage permStage) string {
	if stage == permStageAlways {
		return "cancel"
	}
	return "reject"
}

// permissionStageLines renders the initial permission stage: the title +
// detail, the inline diff (when present), and the option buttons.
// panelW is the already-gutter-reduced panel surface width (plan 18 §B2:
// the canvas places the panel at X(streamGutter); the stage content wraps
// at panelW minus the panel's own 1-col internal pad).
func (m Model) permissionStageLines(p *Permission, panelW int) []string {
	s := m.styles
	innerW := panelW - 2 // the panel's own 1-col left/right internal pad
	if innerW < 1 {
		innerW = 1
	}
	var lines []string
	lines = append(lines, "")
	// Title row: △ + title (footer.permission.tsx:271-273).
	title := "Permission required"
	lines = append(lines, " "+lipgloss.NewStyle().Foreground(s.P.Amber).Bold(true).Render("△")+" "+
		lipgloss.NewStyle().Foreground(s.P.Fg).Render(title))
	lines = append(lines, "")

	// Info lines: the tool title + detail (footer.permission.tsx:277-283).
	infoTitle := permissionTitle(*p)
	lines = append(lines, " "+s.Base.Render(truncate(infoTitle, innerW)))
	if detail := permissionDetail(*p); detail != "" {
		lines = append(lines, " "+s.Faint.Render(truncate(detail, innerW)))
	}
	lines = append(lines, "")

	// Inline diff (plan 17 §B4): when the permission metadata carries a
	// diff (edit/apply_patch tools), render it inline via the shared
	// renderUnifiedDiff helper from Workstream C.
	if diff := permissionDiff(*p); diff != "" {
		if rendered := renderUnifiedDiff(diff, permissionDiffFile(*p), s.P, innerW); rendered != "" {
			lines = append(lines, rendered)
			lines = append(lines, "")
		}
	}

	// Option buttons (footer.permission.tsx:438-447): a row of padded cells,
	// the selected one with the highlight bg.
	lines = append(lines, m.permissionButtons(permStageOptions(permStagePermission), innerW))
	return lines
}

// permissionAlwaysStageLines renders the "always" confirmation stage: the
// patterns that will be allowed, and the Confirm / Cancel buttons
// (footer.permission.tsx:401-422, 438-447).
func (m Model) permissionAlwaysStageLines(p *Permission, panelW int) []string {
	s := m.styles
	innerW := panelW - 2 // the panel's own 1-col left/right internal pad
	if innerW < 1 {
		innerW = 1
	}
	var lines []string
	lines = append(lines, "")
	title := "Always allow"
	lines = append(lines, " "+lipgloss.NewStyle().Foreground(s.P.Amber).Bold(true).Render("△")+" "+
		lipgloss.NewStyle().Foreground(s.P.Fg).Render(title))
	lines = append(lines, "")
	for _, l := range permAlwaysLines(p.Always, p.Permission) {
		lines = append(lines, " "+s.Base.Render(truncate(l, innerW)))
	}
	lines = append(lines, "")
	lines = append(lines, m.permissionButtons(permStageOptions(permStageAlways), innerW))
	return lines
}

// permissionRejectStageLines renders the reject stage: the prompt + a
// textarea-like field for the rejection message
// (footer.permission.tsx:285-342).
func (m Model) permissionRejectStageLines(panelW int) []string {
	s := m.styles
	innerW := panelW - 2 // the panel's own 1-col left/right internal pad
	if innerW < 1 {
		innerW = 1
	}
	var lines []string
	lines = append(lines, "")
	title := "Reject permission"
	lines = append(lines, " "+lipgloss.NewStyle().Foreground(s.P.Red).Bold(true).Render("△")+" "+
		lipgloss.NewStyle().Foreground(s.P.Fg).Render(title))
	lines = append(lines, "")
	lines = append(lines, " "+s.Faint.Render("Tell OpenCode what to do differently"))
	lines = append(lines, "")
	// The reject-message field: a single-line input rendered as a BgPanel-
	// filled row with the current message text (or a placeholder).
	msg := m.permState.message
	style := s.Surface(s.P.BgPanel).Width(innerW)
	if msg == "" {
		lines = append(lines, " "+style.Foreground(s.P.FgGhost).Render("enter feedback (optional)"))
	} else {
		lines = append(lines, " "+style.Foreground(s.P.Fg).Render(truncate(msg, innerW)))
	}
	return lines
}

// permissionButtons renders the option buttons as a single-row, each cell
// padded; the selected one uses the selection style (highlight bg)
// (footer.permission.tsx:37-66).
func (m Model) permissionButtons(opts []permOption, innerW int) string {
	s := m.styles
	cells := make([]string, 0, len(opts))
	for _, o := range opts {
		label := permOptLabel(o)
		if o == m.permState.selected {
			cells = append(cells, s.Selection.Render(" "+label+" "))
		} else {
			cells = append(cells, s.Base.Render(" "+label+" "))
		}
	}
	row := strings.Join(cells, " ")
	return s.Surface(s.P.BgElev).Width(innerW).Render(row)
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

// permissionDiff extracts the inline diff from the permission metadata when
// present (plan 17 §B4). opencode's permEdit puts the diff in
// `metadata.diff` (tool.ts:927); the engine populates it for edit/apply_patch
// permissions. Returns "" when no diff is present.
func permissionDiff(p Permission) string {
	if len(p.Metadata) == 0 {
		return ""
	}
	var meta map[string]any
	if json.Unmarshal(p.Metadata, &meta) != nil {
		return ""
	}
	if v, ok := meta["diff"]; ok {
		if str, ok := v.(string); ok && strings.TrimSpace(str) != "" {
			return str
		}
	}
	return ""
}

// permissionDiffFile extracts the filename for syntax-highlighting the inline
// diff (the file the edit/apply_patch is patching). opencode's permEdit sets
// `file` from input.filePath (tool.ts:922); Opcode42 reads the same field from
// the permission metadata.
func permissionDiffFile(p Permission) string {
	if len(p.Metadata) == 0 {
		return ""
	}
	var meta map[string]any
	if json.Unmarshal(p.Metadata, &meta) != nil {
		return ""
	}
	for _, k := range []string{"filePath", "path", "file"} {
		if v, ok := meta[k]; ok {
			if str, ok := v.(string); ok && strings.TrimSpace(str) != "" {
				return str
			}
		}
	}
	return ""
}

// permissionPanelHeight returns the panel height for the permission footer
// panel (plan 17 §B1): base + PERMISSION_ROWS, where base is the non-textarea
// chrome height (the status bar's rendered height). Mirrors opencode's
// applyHeight (footer.ts:697-722) with PERMISSION_ROWS=12.
func (m Model) permissionPanelHeight() int {
	base := lipgloss.Height(m.statusBarView(m.leftColumnWidth()))
	if base < 1 {
		base = 1
	}
	return base + 12
}
