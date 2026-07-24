package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Sub-agent navigation (plan 08b §9). Sub-agent runs are child sessions: a
// session with a ParentID. opencode surfaces them with a footer (label + "i of
// n") and parent/prev/next navigation; this mirrors that over the same wire data
// (the store already mirrors every session via SSE, and GET /session/{id}/children
// keeps the parent's child set fresh when we descend into it).

// childrenLoadedMsg carries a session's children (GET /session/{id}/children).
type childrenLoadedMsg struct {
	children []Session
	err      error
}

// childMessagesLoadedMsg carries a child session's message stream, fetched on
// first expand of a task card (plan 08e §C1). The items mirror the
// GET /session/{id}/message shape; the reducer (model.go) ingests them into
// the store via ingestHistory so taskTranscript can render them inline.
type childMessagesLoadedMsg struct {
	childID string
	items   []wireWithParts
	err     error
}

// loadChildrenCmd fetches a session's sub-agent children. Exercises the frozen
// GET /session/{id}/children endpoint and tops up any child the store missed.
func loadChildrenCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		var children []Session
		err := c.GetJSON(ctx, "/session/"+sessionID+"/children", &children)
		return childrenLoadedMsg{children: children, err: err}
	}
}

// loadChildMessagesCmd fetches a child session's message stream on first
// expand of a task card (plan 08e §C1). Uses the frozen GET /session/{id}/message
// endpoint (the same one loadMessagesCmd uses for the open session); the
// items are ingested into the store via ingestHistory so taskTranscript can
// render them inline. No wire divergence — this is a plain GET against an
// existing endpoint, just scoped to the child id.
func loadChildMessagesCmd(ctx context.Context, c *opcode42client.Opcode42Client, childID string) tea.Cmd {
	return func() tea.Msg {
		var items []wireWithParts
		err := c.GetJSON(ctx, "/session/"+childID+"/message", &items)
		return childMessagesLoadedMsg{childID: childID, items: items, err: err}
	}
}

// childrenOf returns the sub-agent child sessions of sid, in store order
// (ascending id == chronological).
func (m Model) childrenOf(sid string) []Session {
	if sid == "" {
		return nil
	}
	var out []Session
	for _, s := range m.store.sessions {
		if s.ParentID == sid {
			out = append(out, s)
		}
	}
	return out
}

// indexOfSession returns the position of id in ss, or -1.
func indexOfSession(ss []Session, id string) int {
	for i := range ss {
		if ss[i].ID == id {
			return i
		}
	}
	return -1
}

// subagentTitleRe extracts the agent name from an opencode sub-agent title
// ("@review subagent: …" → "review"); other titles fall back to "Subagent".
var subagentTitleRe = regexp.MustCompile(`@(\w+) subagent`)

// subagentLabel is the display name for a child session (the spawning agent's
// name when the title encodes it, else "Subagent").
func subagentLabel(s Session) string {
	if mm := subagentTitleRe.FindStringSubmatch(s.Title); mm != nil {
		return titlecase(mm[1])
	}
	return "Subagent"
}

func titlecase(s string) string {
	if s == "" {
		return s
	}
	b := []rune(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}

// openSession switches the open session to id (loading its stream + children).
// A no-op when id is empty or already current. Saves/restores the composer
// draft per session (plan 08f H19 / G.22; opencode prompt stash on route change).
func (m Model) openSession(id string) (Model, tea.Cmd) {
	if id == "" || id == m.cfg.SessionID {
		return m, nil
	}
	m = m.saveSessionDraft(m.cfg.SessionID)
	m.cfg.SessionID = id
	m.screen = ScreenSession
	m.scroll.ToTail() // snap to the live tail of the new stream
	m = m.restoreSessionDraft(id)
	// loadMessagesCmd's completion also fetches this session's children, so the
	// sub-agent footer is fresh without a second call here.
	return m, loadMessagesCmd(m.ctx, m.client, id)
}

// saveSessionDraft parks the current composer text under sessionID
// (including empty, so a previously-saved draft is cleared on switch away
// after the user emptied the composer).
func (m Model) saveSessionDraft(sessionID string) Model {
	if sessionID == "" {
		return m
	}
	if m.sessionDrafts == nil {
		m.sessionDrafts = map[string]string{}
	}
	m.sessionDrafts[sessionID] = m.input.Value()
	return m
}

// restoreSessionDraft loads a parked draft for sessionID into the composer
// (or clears it when none was saved).
func (m Model) restoreSessionDraft(sessionID string) Model {
	text := ""
	if m.sessionDrafts != nil {
		text = m.sessionDrafts[sessionID]
	}
	m.input.SetValue(text)
	m.input.CursorEnd()
	return m.resizeComposer()
}

// enterFirstChild descends into the current session's first sub-agent child.
func (m Model) enterFirstChild() (tea.Model, tea.Cmd) {
	kids := m.childrenOf(m.cfg.SessionID)
	if len(kids) == 0 {
		m.status = "no sub-agents in this session"
		return m, nil
	}
	nm, cmd := m.openSession(kids[0].ID)
	return nm, cmd
}

// gotoParent returns from a sub-agent child to its parent session.
func (m Model) gotoParent() (tea.Model, tea.Cmd) {
	cur := m.currentSession()
	if cur == nil || cur.ParentID == "" {
		m.status = "not in a sub-agent session"
		return m, nil
	}
	nm, cmd := m.openSession(cur.ParentID)
	return nm, cmd
}

// cycleSibling moves between sibling sub-agents of the current child session
// (dir +1 = next, -1 = previous), wrapping around.
func (m Model) cycleSibling(dir int) (tea.Model, tea.Cmd) {
	cur := m.currentSession()
	if cur == nil || cur.ParentID == "" {
		m.status = "not in a sub-agent session"
		return m, nil
	}
	sib := m.childrenOf(cur.ParentID)
	if len(sib) < 2 {
		return m, nil
	}
	i := indexOfSession(sib, cur.ID)
	if i < 0 {
		return m, nil
	}
	next := ((i+dir)%len(sib) + len(sib)) % len(sib)
	nm, cmd := m.openSession(sib[next].ID)
	return nm, cmd
}

// subagentFooterView renders the sub-agent context strip above the composer:
// when in a child session, its label + position among siblings + nav hints;
// when in a parent that spawned sub-agents, an active/recent count + an
// invitation to descend. Empty otherwise.
//
// The parent case mirrors opencode's two-count model
// (run/footer.command.tsx:356,374): activeCount = children with status
// "running"; totalCount = all children. Label: "N active" when activeCount > 0,
// else "N recent" when totalCount > 0, else hidden. opencode's tabs Map is a
// historical, never-pruned set (run/subagent-data.ts:333-356 syncTaskTab only
// sets, never deletes), so completed children stay in totalCount — matching
// opencode's "N recent" after a run completes (plan 17 §E1, §E5).
func (m Model) subagentFooterView(width int) string {
	cur := m.currentSession()
	if cur == nil {
		return ""
	}
	switch {
	case cur.ParentID != "":
		sib := m.childrenOf(cur.ParentID)
		info := subagentLabel(*cur)
		if n := len(sib); n > 0 {
			info += fmt.Sprintf(" (%d of %d)", indexOfSession(sib, cur.ID)+1, n)
		}
		hint := "⌃x↑ parent"
		if len(sib) > 1 {
			hint += " · ⌃x[ prev · ⌃x] next"
		}
		return m.subagentBar(width, info, hint)
	default:
		kids := m.childrenOf(cur.ID)
		if len(kids) == 0 {
			return ""
		}
		activeCount := 0
		for _, k := range kids {
			if m.childStatus(k.ID) == "running" {
				activeCount++
			}
		}
		var info string
		switch {
		case activeCount > 0:
			info = fmt.Sprintf("%d active", activeCount)
		default:
			info = fmt.Sprintf("%d recent", len(kids))
		}
		return m.subagentBar(width, info, "⌃x↓ enter")
	}
}

// subagentBar draws a single accent-barred strip: a purple label on the left,
// faint key hints on the right, bounded to width.
func (m Model) subagentBar(width int, info, hint string) string {
	s := m.styles
	if width <= 0 {
		width = m.contentWidth()
	}
	label := lipgloss.NewStyle().Foreground(s.P.Purple).Bold(true).Render("⦿ " + info)
	keys := s.Faint.Render(hint)
	gap := width - lipgloss.Width(label) - lipgloss.Width(keys)
	if gap < 1 {
		return lipgloss.NewStyle().Width(width).Render(label)
	}
	return label + strings.Repeat(" ", gap) + keys
}

// childStatus derives a child session's run status and returns one of:
// "running", "completed", "error", "cancelled", or "" (unknown).
//
// Plan 20 §1a: reads from m.childStatusMap first (computed once per store
// change by recomputeChildStatuses). Falls back to the per-child
// taskChildStatusFromParent + child-stream scan when the child is not in
// the map (e.g. before the first recompute during bootstrap). The fallback
// preserves correctness during the gap between store mutation and the
// recompute call.
func (m Model) childStatus(childID string) string {
	if s, ok := m.childStatusMap[childID]; ok {
		return s
	}
	if s := m.taskChildStatusFromParent(childID); s != "" {
		return s
	}
	msgs := m.store.messages[childID]
	if len(msgs) == 0 {
		return ""
	}
	hasAssistant := false
	for _, msg := range msgs {
		if msg.Role == "assistant" {
			hasAssistant = true
		}
		for _, p := range m.store.parts[msg.ID] {
			if p.Type != "tool" {
				continue
			}
			var st toolState
			if !decode(p.State, &st) {
				continue
			}
			switch st.Status {
			case "running", "pending":
				return "running"
			case "error":
				if taskCancelled(st) {
					return "cancelled"
				}
				return "error"
			}
		}
	}
	if hasAssistant {
		return "completed"
	}
	return ""
}

// recomputeChildStatuses builds m.childStatusMap in ONE pass over all
// sessions (plan 20 §1a). For each session with a ParentID, the parent's
// task tool parts are scanned once to derive the status via the existing
// taskStatusFromState logic. The result is stored in m.childStatusMap and
// read by childStatus on subsequent frames — zero JSON decodes per frame.
//
// The scan groups child→parent once (via the sessions slice) then iterates
// each parent's task parts once, matching the per-child taskChildStatusFromParent
// derivation but with the outer loop amortised: O(sessions + parent-msgs ×
// parent-parts) total, vs O(children × parent-msgs × parent-parts) for the
// per-child call.
func (m Model) recomputeChildStatuses() Model {
	// Skip the O(sessions × msgs × parts) scan when the store hasn't
	// changed since the last recompute. anim ticks, PTY output, composer
	// keypresses etc. don't change child statuses — only store mutations
	// (SSE events, direct mutations) do, and those bump store.version.
	if m.childStatusMap != nil && m.childStatusVersion == m.store.version {
		return m
	}
	if m.childStatusMap == nil {
		m.childStatusMap = make(map[string]string)
	}
	// Clear the map: a recompute reflects the current store state, so any
	// child not matched below stays absent (and falls back to the per-child
	// scan in childStatus). This is cheaper than allocating a new map each
	// call — the map's capacity stays stable across recomputes.
	for k := range m.childStatusMap {
		delete(m.childStatusMap, k)
	}
	// First pass: collect parents that have children, so we can iterate
	// each parent's task parts once.
	parents := map[string]bool{}
	for _, s := range m.store.sessions {
		if s.ParentID != "" {
			parents[s.ParentID] = true
		}
	}
	// For each parent, scan its task tool parts and derive the child's
	// status. A task part whose childSessionID matches a known child sets
	// that child's status via taskStatusFromState.
	for parentID := range parents {
		for _, msg := range m.store.messages[parentID] {
			for _, p := range m.store.parts[msg.ID] {
				if p.Type != "tool" || p.Tool != "task" {
					continue
				}
				var st toolState
				if !decode(p.State, &st) {
					continue
				}
				cid := childSessionID(st)
				if cid == "" {
					continue
				}
				// Only record children that exist in the store (the
				// session slice is the source of truth for "is this id a
				// child of this parent"). This keeps the map keyed by
				// known sessions; an unknown childSessionID (e.g. a stale
				// task part) is ignored.
				if !m.sessionIsChildOf(cid, parentID) {
					continue
				}
				if _, exists := m.childStatusMap[cid]; !exists {
					m.childStatusMap[cid] = taskStatusFromState(st)
				}
			}
		}
	}
	m.childStatusVersion = m.store.version
	return m
}

// sessionIsChildOf reports whether cid is a session in the store with
// ParentID == parentID. Used by recomputeChildStatuses to confirm a task
// part's childSessionID refers to a known child before recording its
// status.
func (m Model) sessionIsChildOf(cid, parentID string) bool {
	for _, s := range m.store.sessions {
		if s.ID == cid {
			return s.ParentID == parentID
		}
	}
	return false
}

// taskChildStatusFromParent scans the parent session's task tool parts for the
// one spawned childID and derives its status the same way opencode's taskStatus
// does (run/subagent-data.ts:295-309). Returns "" when no matching task part
// is found in any parent message stream (e.g. the child's parent isn't in the
// store, or the task part predates metadata.sessionId and has no <task id>
// wrapper). Used as the preferred source by childStatus so the count is
// correct before the child's own messages are lazily loaded (plan 17 §E2a).
func (m Model) taskChildStatusFromParent(childID string) string {
	if childID == "" {
		return ""
	}
	parentID := ""
	for _, s := range m.store.sessions {
		if s.ID == childID && s.ParentID != "" {
			parentID = s.ParentID
			break
		}
	}
	if parentID == "" {
		return ""
	}
	for _, msg := range m.store.messages[parentID] {
		for _, p := range m.store.parts[msg.ID] {
			if p.Type != "tool" || p.Tool != "task" {
				continue
			}
			var st toolState
			if !decode(p.State, &st) {
				continue
			}
			if childSessionID(st) != childID {
				continue
			}
			return taskStatusFromState(st)
		}
	}
	return ""
}

// taskStatusFromState mirrors opencode's taskStatus derivation
// (run/subagent-data.ts:295-309): completed → "completed"; error with
// interrupted metadata OR "Tool execution aborted" error text → "cancelled";
// other error → "error"; anything else (pending/running/empty) → "running".
func taskStatusFromState(st toolState) string {
	switch st.Status {
	case "completed":
		return "completed"
	case "error":
		if taskCancelled(st) {
			return "cancelled"
		}
		return "error"
	default:
		return "running"
	}
}

// taskCancelled reports whether an errored task tool part represents a
// cancelled (interrupted/aborted) run vs a genuine failure. Matches opencode's
// check (run/subagent-data.ts:301-303): metadata.interrupted === true OR
// state.error === "Tool execution aborted".
func taskCancelled(st toolState) bool {
	if st.Error == "Tool execution aborted" {
		return true
	}
	if len(st.Metadata) > 0 {
		var meta struct {
			Interrupted bool `json:"interrupted"`
		}
		if json.Unmarshal(st.Metadata, &meta) == nil && meta.Interrupted {
			return true
		}
	}
	return false
}
