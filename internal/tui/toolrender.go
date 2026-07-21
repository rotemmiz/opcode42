package tui

// toolrender.go — Plan 08c M7: rich per-tool header/output/todo rendering.
//
// Design goals (plan 08c §2d):
//  1. Per-tool headers with the salient argument extracted from the tool state
//     input JSON (e.g. "Read src/x.ts", "Bash npm test") — not a generic status.
//  2. Collapsible output panels on BgPanel background with a ▸/▾ fold affordance.
//  3. Todo lists rendered as checkbox glyphs + status colors (opencode todo-item.tsx).
//  4. Foldable reasoning (handled in render.go thinking / thinkingExpanded).
//  5. Background fill: every panel line is padded to contentWidth so no transparent
//     cell leaks through (same pattern as viewSplash in model.go).
//
// Tool state JSON shape (from openapi.json ToolStatePending/Running/Completed/Error):
//
//	{ "status": "pending|running|completed|error",
//	  "input":  { <tool-specific input fields> },   // may be string or object
//	  "output": "...",
//	  "title":  "...",
//	  "metadata": { ... },
//	  "error":  "..." }
//
// Input field names per tool (opencode tool/*.ts):
//   - bash:       command (string), description (string)
//   - read:       filePath (string)
//   - write:      filePath (string)
//   - edit:       filePath (string)
//   - grep:       pattern (string), path (string)
//   - glob:       pattern (string), path (string)
//   - webfetch:   url (string)
//   - websearch:  query (string)
//   - todowrite:  todos (array of {id,status,content,priority})
//   - task:       description (string), subagent_type (string)
//   - skill:      name (string)
//   - apply_patch: (no salient single field)

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// maxPanelLines is the maximum lines shown in an expanded tool output panel
// before a "… N more lines" truncation hint is appended.
const maxPanelLines = 20

// toolState is the parsed tool part state (the wire ToolState union type).
type toolState struct {
	Status   string          `json:"status"`
	Input    json.RawMessage `json:"input"`
	Output   string          `json:"output"`
	Title    string          `json:"title"`
	Metadata json.RawMessage `json:"metadata"`
	Error    string          `json:"error"`
}

// taskMeta is the metadata block the opencode TaskTool attaches to the tool
// state (task.ts:171-176): it carries the spawned child session id under the
// JSON key "sessionId", alongside the parent session id and the resolved
// model. Android's SubAgentBlock.kt:60-71 reads the same field as the primary
// source of the child id, with a fallback to the <task id="…"> wrapper in the
// output text — childSessionID below mirrors that priority order.
type taskMeta struct {
	ParentSessionID string `json:"parentSessionId"`
	SessionID       string `json:"sessionId"`
	Background      bool   `json:"background,omitempty"`
}

// taskIDRe extracts the child session id from the <task id="…" state="…">
// wrapper that opencode's TaskTool emits around its output (task.ts:72).
// Fallback when the metadata.sessionId field is absent (e.g. an older
// daemon, or a part whose metadata hasn't arrived yet).
var taskIDRe = regexp.MustCompile(`<task id="([^"]+)"`)

// childSessionID extracts the spawned sub-agent session id from a task tool
// part, in priority order (matches Android SubAgentBlock.kt:60-71):
//  1. the tool state's metadata.sessionId field (set by opencode TaskTool), or
//  2. the id attribute of the <task id="…" …> wrapper in the output/error text.
//
// Returns "" when no child id can be recovered (the part predates the
// metadata, or the daemon isn't opencode-compatible).
func childSessionID(st toolState) string {
	if len(st.Metadata) > 0 {
		var tm taskMeta
		if json.Unmarshal(st.Metadata, &tm) == nil && tm.SessionID != "" {
			return tm.SessionID
		}
	}
	haystack := st.Output
	if haystack == "" {
		haystack = st.Error
	}
	if mm := taskIDRe.FindStringSubmatch(haystack); mm != nil {
		return mm[1]
	}
	return ""
}

// toolInput is the per-tool input fields we care about.
// Description is shared between bash (optional human-readable label) and
// task (the task body); both tools use the JSON key "description".
type toolInput struct {
	// bash: human-readable description (prefers over command for the header)
	Command     string `json:"command"`
	Description string `json:"description"` // bash description OR task description
	// read / write / edit / apply_patch (lsp too)
	FilePath string `json:"filePath"`
	// grep / glob
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	// webfetch
	URL string `json:"url"`
	// websearch
	Query string `json:"query"`
	// task: subagent type qualifier (e.g. "coding", "research")
	SubagentType string `json:"subagent_type"`
	// skill
	Name string `json:"name"`
	// todowrite
	Todos []todoItem `json:"todos"`
}

// todoItem mirrors opencode's todo shape: {id, status, content, priority}.
type todoItem struct {
	ID       string `json:"id"`
	Status   string `json:"status"` // "pending" | "in_progress" | "completed"
	Content  string `json:"content"`
	Priority string `json:"priority"` // optional
}

// parseToolState unmarshals Part.State into toolState + toolInput.
func parseToolState(raw json.RawMessage) (toolState, toolInput) {
	var st toolState
	var inp toolInput
	if len(raw) == 0 {
		return st, inp
	}
	_ = json.Unmarshal(raw, &st)
	if len(st.Input) > 0 {
		_ = json.Unmarshal(st.Input, &inp)
	}
	return st, inp
}

// toolHeader returns a short, human-readable header line for a tool call,
// extracting the most salient argument from its input JSON.
//
// Mapping table (matches opencode session-v2.tsx InlineTool/BlockTool labels):
//
//	bash        → "Bash <command>"  (or description if present)
//	read        → "Read <filePath>"
//	write       → "Write <filePath>"
//	edit        → "Edit <filePath>"
//	apply_patch → "Patch"
//	grep        → "Grep \"<pattern>\""  [in <path>]
//	glob        → "Glob \"<pattern>\""  [in <path>]
//	webfetch    → "WebFetch <url>"
//	websearch   → "WebSearch \"<query>\""
//	todowrite   → "Todos"
//	task        → "Task — <description>"
//	skill       → "Skill \"<name>\""
//	(fallback)  → "<tool>"
func toolHeader(tool string, inp toolInput) string {
	switch tool {
	case "bash", "shell":
		if inp.Description != "" {
			return "Bash " + inp.Description
		}
		if inp.Command != "" {
			return "Bash " + firstLine(inp.Command)
		}
		return "Bash"
	case "read":
		if inp.FilePath != "" {
			return "Read " + inp.FilePath
		}
		return "Read"
	case "write":
		if inp.FilePath != "" {
			return "Write " + inp.FilePath
		}
		return "Write"
	case "edit", "multiedit":
		if inp.FilePath != "" {
			return "Edit " + inp.FilePath
		}
		return "Edit"
	case "apply_patch":
		return "Patch"
	case "grep":
		h := "Grep"
		if inp.Pattern != "" {
			h += " \"" + inp.Pattern + "\""
		}
		if inp.Path != "" {
			h += " in " + inp.Path
		}
		return h
	case "glob":
		h := "Glob"
		if inp.Pattern != "" {
			h += " \"" + inp.Pattern + "\""
		}
		if inp.Path != "" {
			h += " in " + inp.Path
		}
		return h
	case "webfetch":
		if inp.URL != "" {
			return "WebFetch " + inp.URL
		}
		return "WebFetch"
	case "websearch":
		if inp.Query != "" {
			return "WebSearch \"" + inp.Query + "\""
		}
		return "WebSearch"
	case "todowrite", "todo_write":
		return "Todos"
	case "task":
		if inp.Description != "" {
			t := inp.SubagentType
			if t == "" {
				t = "General"
			}
			return titlecase(t) + " Task — " + inp.Description
		}
		return "Task"
	case "skill":
		if inp.Name != "" {
			return "Skill \"" + inp.Name + "\""
		}
		return "Skill"
	default:
		if tool != "" {
			return tool
		}
		return "tool"
	}
}

// spinnerFrames are the braille-pattern animation frames, matching opencode's
// component/spinner.tsx SPINNER_FRAMES at 80ms cadence. Our animPeriod is 100ms
// so we map animFrame → braille index to stay consistent with the tick rate.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// statusGlyph returns (glyph, fgStyle) for a tool status.
// Colors mirror opencode session-v2.tsx InlineTool fg logic:
//
//	completed → textMuted (FgDim)
//	error     → error (Red)
//	running   → animated braille spinner (Accent) — opencode component/spinner.tsx
//	pending   → amber dot
//
// For "running" the glyph is a braille frame driven by m.animFrame so it
// animates with the global animTick.  The Accent color keeps it visually distinct.
func (m Model) statusGlyph(status string) (string, lipgloss.Style) {
	s := m.styles
	switch status {
	case "completed":
		return "✓", lipgloss.NewStyle().Foreground(s.P.Green)
	case "error":
		return "✗", lipgloss.NewStyle().Foreground(s.P.Red)
	case "running":
		frame := spinnerFrames[m.animFrame%len(spinnerFrames)]
		return frame, lipgloss.NewStyle().Foreground(s.P.Accent())
	default: // pending or empty
		return "•", lipgloss.NewStyle().Foreground(s.P.Amber)
	}
}

// toolRow renders a single tool Part richly (plan 08c M7):
//   - Always: per-tool header line (glyph + header + fold affordance).
//   - Expanded (default): output panel on BgPanel, max maxPanelLines, truncation hint.
//   - Collapsed: header only.
//   - TodoWrite: todo items with checkbox glyphs + status colors.
//   - Error: red error line below header.
//   - edit/apply_patch (plan 17 Workstream C): inline unified diff at
//     completion, rendered via the shared renderInlineDiff helper (full hunks,
//     no 20-line cap). The rendered string is cached on the Model so the diff
//     is built once at completion, not every animation tick.
//
// renderMessage gates hideTools before calling this; do not duplicate the check here.
func (m Model) toolRow(p Part) string {
	s := m.styles
	st, inp := parseToolState(p.State)

	// ── Header line ────────────────────────────────────────────────────────────
	glyph, gstyle := m.statusGlyph(st.Status)
	hdr := toolHeader(p.Tool, inp)

	// Plan 17 Workstream C: edit/apply_patch at completion render an inline
	// diff instead of the generic output panel. Compute it once here (cached)
	// so the fold affordance, the panel decision, and the body render all
	// agree on its presence. The cache key is (partID, patchHash, width,
	// themeName); the diff is immutable once the tool completes, so the cache
	// hit rate is ~100% across animation ticks.
	inlineDiff := ""
	isDiffTool := p.Tool == "edit" || p.Tool == "multiedit" || p.Tool == "apply_patch"
	if isDiffTool && st.Status == "completed" {
		inlineDiff = m.cachedInlineDiff(p, st, inp)
	}

	// Fold affordance: ▸ collapsed, ▾ expanded.
	hasOutput := strings.TrimSpace(st.Output) != ""
	hasInlineDiff := inlineDiff != ""
	isTodo := p.Tool == "todowrite" || p.Tool == "todo_write"
	isCollapsed := m.view.isToolCollapsed(p.ID)
	foldIcon := ""
	if hasOutput || isTodo || hasInlineDiff {
		if isCollapsed {
			foldIcon = " ▸"
		} else {
			foldIcon = " ▾"
		}
	}

	// Build header: glyph · header · foldIcon
	// For running tools, color the header text with the gradient scanner so it
	// visually animates alongside the braille glyph.  Completed/error/pending
	// tools use the dim style (static, less visual noise).
	// Background fill is provided by the host row — no extra bg needed here
	// (plan 08c M9 CRITICAL note on background fill).
	cw := m.contentWidth()
	glyphStr := gstyle.Render(glyph)
	truncHdr := truncate(hdr, cw-lipgloss.Width(glyph)-3-len(foldIcon))
	var hdrStr string
	if st.Status == "running" {
		// Scanner frame: sweep the header label with Accent ramp.
		hdrStr = scannerFrame(truncHdr, m.animFrame, s.P)
	} else {
		hdrStr = s.Dim.Render(truncHdr)
	}
	headerLine := glyphStr + " " + hdrStr + s.Faint.Render(foldIcon)

	var lines []string
	lines = append(lines, headerLine)

	// ── Task card (plan 08e §C1) ──────────────────────────────────────────────
	// The `task` tool renders as an in-stream card mirroring Android's
	// SubAgentBlock: header (already rendered above) + a meta line (toolcall
	// count + running/done state) + an inline expandable transcript of the
	// child session's message stream. The expand is toggled by the same
	// view.toggleToolCollapse(partID) key the generic output panel uses
	// (ctrl+x v), so a single chord covers both; ctrl+x > descends into the
	// child as a full chat view (model.go handleLeaderKey). The child id is
	// parsed from the tool state's metadata.sessionId (task.ts:173) with a
	// fallback to the <task id="…"> output wrapper.
	if p.Tool == "task" {
		card := m.taskCardBody(st, cw, isCollapsed)
		if card != "" {
			lines = append(lines, card)
		}
		return strings.Join(lines, "\n")
	}

	// ── Error sub-line ─────────────────────────────────────────────────────────
	if st.Status == "error" && st.Error != "" {
		errLine := "  " + lipgloss.NewStyle().Foreground(s.P.Red).
			Render(truncate(firstLine(st.Error), cw-2))
		lines = append(lines, errLine)
	}

	// ── Collapsed: header only ─────────────────────────────────────────────────
	if isCollapsed {
		return strings.Join(lines, "\n")
	}

	// ── Todo list ──────────────────────────────────────────────────────────────
	if isTodo && len(inp.Todos) > 0 {
		todoBlock := m.renderTodos(inp.Todos, cw)
		if todoBlock != "" {
			lines = append(lines, todoBlock)
		}
		return strings.Join(lines, "\n")
	}

	// ── Inline diff (plan 17 Workstream C) ───────────────────────────────────
	// edit/apply_patch at completion render the unified diff inline (full hunks,
	// bypassing the 20-line renderOutputPanel cap — matches opencode's
	// no-truncation behavior, scrollback.writer.tsx:188-225). The diff is cached
	// on the Model so this branch is a map lookup after the first render. The
	// output panel is NOT shown for the diff itself; if the tool also has an
	// output (e.g. LSP diagnostics appended to the result), it follows below.
	if hasInlineDiff {
		lines = append(lines, inlineDiff)
	}

	// ── Output panel ──────────────────────────────────────────────────────────
	// For diff tools, only show the generic output panel if there's additional
	// output beyond the diff (e.g. LSP diagnostics appended to the result).
	// opencode's snapEdit returns the diff as the structured snapshot and the
	// LSP diagnostics are appended to the output text — both are visible, the
	// diff as the snapshot body, the diagnostics as the trailing text.
	if isDiffTool && hasInlineDiff {
		// Strip the leading diff-success marker that opencode's edit/apply_patch
		// tools prepend to their output ("Edit applied successfully." /
		// "Success. Updated the following files:…"). When the only output is the
		// success line, suppress the panel; when diagnostics follow, show them.
		trimmed := trimDiffSuccessOutput(st.Output, p.Tool)
		if trimmed != "" {
			lines = append(lines, m.renderOutputPanel(trimmed, cw))
		}
	} else if hasOutput {
		panel := m.renderOutputPanel(st.Output, cw)
		lines = append(lines, panel)
	}

	return strings.Join(lines, "\n")
}

// trimDiffSuccessOutput strips the redundant success-line prefix that
// opencode's edit and apply_patch tools prepend to their output text, so
// the inline diff (which already conveys the change) isn't followed by a
// redundant "Edit applied successfully." or "Success. Updated the following
// files:…" line. opencode's entryLayout (scrollback.writer.tsx:52-79) hides
// the output text entirely when a structured snapshot renders at
// phase=final; Opcode42 keeps any LSP-diagnostics tail (the part of the output
// the user still needs to see) and drops the rest.
//
// For edit: the output is "Edit applied successfully." + optional diagnostics.
// Returns "" when only the success line is present, else the trimmed tail
// (diagnostics). For apply_patch: the output is
// "Success. Updated the following files:\n<file list>" + optional
// diagnostics. The per-file diff titles already convey the file list, so we
// drop everything except the diagnostics block (edit.ts:196-201,
// apply_patch.ts:284-293).
func trimDiffSuccessOutput(output, tool string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	switch tool {
	case "edit", "multiedit":
		const marker = "Edit applied successfully."
		if strings.HasPrefix(trimmed, marker) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
		}
		return trimmed
	case "apply_patch":
		// The apply_patch output is "Success. Updated the following files:\n
		// <file list>" optionally followed by "\n\nLSP errors detected in …".
		// The file list is redundant (the per-file diff titles show it); keep
		// only the LSP diagnostics block when present.
		if i := strings.Index(trimmed, "LSP errors detected in"); i >= 0 {
			return strings.TrimSpace(trimmed[i:])
		}
		return ""
	}
	return trimmed
}

// renderOutputPanel renders tool output in a BgPanel-filled block bounded to
// maxPanelLines. Each line is padded to panelW so no transparent cells escape.
// plan 08c §2d CRITICAL background fill: single Background(BgPanel) style per
// line padded to width, so every cell is painted even after ANSI resets.
func (m Model) renderOutputPanel(output string, contentW int) string {
	s := m.styles
	// Indent the panel 2 columns so it reads as a child of the header.
	panelW := contentW - 2
	if panelW < 10 {
		panelW = 10
	}
	lineStyle := lipgloss.NewStyle().
		Background(s.P.BgPanel).
		Foreground(s.P.Fg).
		Width(panelW) // pad every line to panelW → no transparent cells

	raw := strings.TrimRight(output, "\n")
	allLines := strings.Split(raw, "\n")

	var truncated bool
	shown := allLines
	if len(allLines) > maxPanelLines {
		shown = allLines[:maxPanelLines]
		truncated = true
	}

	var sb strings.Builder
	for _, l := range shown {
		// Render each line through the panel style padded to panelW so every cell
		// carries the BgPanel background (no lipgloss background inheritance across
		// ANSI reset boundaries — plan 08c Tier 0 CRITICAL note).
		sb.WriteString("  " + lineStyle.Render(l) + "\n")
	}
	if truncated {
		more := len(allLines) - maxPanelLines
		hint := fmt.Sprintf("… %d more line%s", more, pluralS(more))
		sb.WriteString("  " + lipgloss.NewStyle().Foreground(s.P.FgFaint).
			Background(s.P.BgPanel).Width(panelW).Render(hint))
	} else if len(shown) > 0 {
		// Remove trailing newline from the last line's "\n"
		result := sb.String()
		return strings.TrimRight(result, "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// renderTodos renders a list of todoItems as checkbox glyphs with status colors
// inside a BgPanel block. Mirrors opencode todo-item.tsx:
//
//	completed  → [✓] green
//	in_progress → [•] amber
//	pending    → [ ] faint
func (m Model) renderTodos(todos []todoItem, contentW int) string {
	s := m.styles
	panelW := contentW - 2
	if panelW < 10 {
		panelW = 10
	}

	var sb strings.Builder
	for _, td := range todos {
		var glyph string
		var fg theme.Color
		switch td.Status {
		case "completed":
			glyph, fg = "[✓]", s.P.Green
		case "in_progress":
			glyph, fg = "[•]", s.P.Amber
		default: // pending
			glyph, fg = "[ ]", s.P.FgFaint
		}
		row := lipgloss.NewStyle().Foreground(fg).Render(glyph) + " " +
			lipgloss.NewStyle().Foreground(fg).Background(s.P.BgPanel).
				Width(panelW-4). // 4 = "[✓] "
				Render(td.Content)
		// wrap each todo row in BgPanel so the full width is painted
		sb.WriteString("  " + lipgloss.NewStyle().Background(s.P.BgPanel).
			Width(panelW).Render(lipgloss.NewStyle().Foreground(fg).Render(glyph)+" "+td.Content) + "\n")
		_ = row // unused; using the combined render above
	}
	return strings.TrimRight(sb.String(), "\n")
}

// pluralS returns "s" when n != 1, for "N more line(s)".
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// lastToolPartID returns the ID of the last tool part in the active session, or
// "" if there are no tool parts. Used by handleLeaderKey to target the fold toggle
// at the most recent tool (the one the user most likely wants to act on).
func (m Model) lastToolPartID() string {
	sid := m.cfg.SessionID
	for _, msg := range m.store.messages[sid] {
		parts := m.store.parts[msg.ID]
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i].Type == "tool" {
				return parts[i].ID
			}
		}
	}
	return ""
}

// lastTaskPart returns the last `task` tool part in the active session, or nil
// when there is none. Used by handleLeaderKey's ctrl+x > chord (descend into
// the most recent sub-agent child) so the chord targets the task the user
// most likely means.
func (m Model) lastTaskPart() *Part {
	sid := m.cfg.SessionID
	for _, msg := range m.store.messages[sid] {
		parts := m.store.parts[msg.ID]
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i].Type == "tool" && parts[i].Tool == "task" {
				p := parts[i]
				return &p
			}
		}
	}
	return nil
}

// maybeLoadTaskChildMessages returns a loadChildMessagesCmd when the given tool
// part is a `task` whose child session id is known AND whose child messages
// aren't already mirrored in the store. This is fired on expand (ctrl+x v) so
// the inline transcript populates on first expand rather than showing an empty
// card (plan 08e §C1). Returns nil when the part isn't a task, the child id
// is unknown, or the child's messages are already loaded (idempotent).
func (m Model) maybeLoadTaskChildMessages(partID string) tea.Cmd {
	if partID == "" || m.client == nil {
		return nil
	}
	for _, msg := range m.store.messages[m.cfg.SessionID] {
		for _, p := range m.store.parts[msg.ID] {
			if p.ID != partID || p.Tool != "task" {
				continue
			}
			st, _ := parseToolState(p.State)
			cid := childSessionID(st)
			if cid == "" {
				return nil
			}
			if _, ok := m.store.messages[cid]; ok && len(m.store.messages[cid]) > 0 {
				return nil
			}
			return loadChildMessagesCmd(m.ctx, m.client, cid)
		}
	}
	return nil
}

// taskCardBody renders the body of a `task` tool card (plan 08e §C1): a meta
// line (toolcall count + running/done state) and, when expanded, an indented
// transcript of the child session's message stream. Returns "" when there is
// nothing to show. The header line is rendered by the caller (toolRow) —
// this is the body below it.
//
// The child id is parsed from the tool state's metadata.sessionId (set by
// opencode's TaskTool, task.ts:173) with a fallback to the <task id="…">
// wrapper in the output text (Android SubAgentBlock.kt:60-71 mirrors this).
// The toolcall count comes from the child session's message stream already
// mirrored in the store (loadChildrenCmd populates the child sessions;
// loadChildMessagesCmd populates their messages on first expand).
//
// isCollapsed controls the transcript expand: when true only the meta line is
// shown; when false the transcript is rendered indented under the card.
func (m Model) taskCardBody(st toolState, cw int, isCollapsed bool) string {
	s := m.styles
	childID := childSessionID(st)

	// ── Meta line: N toolcalls · running…/done/error ───────────────────────
	// The toolcall count is the number of assistant messages in the child
	// session's mirrored stream (each assistant turn = one toolcall-bearing
	// message). When the child id is unknown we fall back to a bare label.
	var meta string
	if childID != "" {
		n := m.childToolcallCount(childID)
		meta = fmt.Sprintf("%d toolcall%s", n, pluralS(n))
	} else {
		meta = "subagent"
	}
	switch st.Status {
	case "running":
		meta += " · running…"
	case "completed":
		meta += " · done"
	case "error":
		meta += " · error"
	}
	metaLine := "  " + s.Faint.Render(meta)

	if isCollapsed {
		return metaLine
	}

	var lines []string
	lines = append(lines, metaLine)

	// ── Inline transcript (expandable) ─────────────────────────────────────
	// On expand, the child session's messages are loaded via
	// loadChildMessagesCmd (model.go fires it when the collapse state
	// flips). The transcript renders each message indented under the card,
	// mirroring Android's ChildTranscript but inlined into the terminal
	// scrollback — the TUI's advantage over the mobile card.
	if childID != "" {
		if transcript := m.taskTranscript(childID, cw); transcript != "" {
			lines = append(lines, transcript)
		}
	}

	// ── Output panel fallback ──────────────────────────────────────────────
	// When the child id is unknown OR the transcript is empty (not yet
	// loaded), fall back to the task_result text in the output panel — same
	// render as the generic tool output so a task without a spawnable child
	// still shows its result.
	if childID == "" && strings.TrimSpace(st.Output) != "" {
		lines = append(lines, m.renderOutputPanel(st.Output, cw))
	}

	return strings.Join(lines, "\n")
}

// childToolcallCount returns the number of assistant turns in the child
// session's mirrored message stream — the "toolcalls" count shown in the task
// card's meta line. Each assistant message in the child session corresponds
// to one toolcall-bearing turn (the child agent's reasoning + tool use). When
// the child's messages haven't been loaded yet (expand hasn't fired), this
// returns 0; the count populates once loadChildMessagesCmd resolves.
func (m Model) childToolcallCount(childID string) int {
	n := 0
	for _, msg := range m.store.messages[childID] {
		if msg.Role == "assistant" {
			n++
		}
	}
	return n
}

// taskTranscript renders the child session's message stream indented under
// the task card (plan 08e §C1). Each message shows its role prefix + the
// text of its text parts, truncated to fit the card width. Mirrors Android's
// ChildTranscript but inlined into the terminal scrollback (newest-last, so
// it reads top-down with the parent stream).
func (m Model) taskTranscript(childID string, cw int) string {
	s := m.styles
	msgs := m.store.messages[childID]
	if len(msgs) == 0 {
		return ""
	}
	indent := "    "
	w := cw - len(indent)
	if w < 10 {
		w = 10
	}
	var lines []string
	for _, msg := range msgs {
		var text string
		for _, p := range m.store.parts[msg.ID] {
			if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
				if text != "" {
					text += "\n"
				}
				text += strings.TrimSpace(p.Text)
			}
		}
		if text == "" {
			continue
		}
		role := msg.Role
		if role == "" {
			role = "msg"
		}
		roleTag := s.Faint.Render("[" + role + "]")
		var bodyLines []string
		for _, l := range strings.Split(text, "\n") {
			bodyLines = append(bodyLines, truncate(l, w-lipgloss.Width(roleTag)-1))
		}
		body := s.Base.Render(strings.Join(bodyLines, "\n"))
		lines = append(lines, indent+roleTag+" "+body)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
