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
	"strings"

	"github.com/charmbracelet/lipgloss"
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
//
// renderMessage gates hideTools before calling this; do not duplicate the check here.
func (m Model) toolRow(p Part) string {
	s := m.styles
	st, inp := parseToolState(p.State)
	bg := s.P.BgPanel

	// Box geometry: a left ┃ bar (1 col, rendered outside Width) + 1 col of
	// horizontal padding on each side. innerW is the usable text width inside the
	// box; every inner line is padded to it so the panel background fills the box.
	cw := m.contentWidth()
	innerW := cw - 3 // 1 border + 2 horizontal padding
	if innerW < 10 {
		innerW = 10
	}

	// ── Header line ────────────────────────────────────────────────────────────
	glyph, gstyle := m.statusGlyph(st.Status)
	hdr := toolHeader(p.Tool, inp)

	// Fold affordance: ▸ collapsed, ▾ expanded.
	hasOutput := strings.TrimSpace(st.Output) != ""
	isTodo := p.Tool == "todowrite" || p.Tool == "todo_write"
	isCollapsed := m.view.isToolCollapsed(p.ID)
	foldIcon := ""
	if hasOutput || isTodo {
		if isCollapsed {
			foldIcon = " ▸"
		} else {
			foldIcon = " ▾"
		}
	}

	// Build header: glyph · header · foldIcon. Every segment carries the panel
	// background (bg does not survive ANSI resets mid-line — plan 08c Tier 0), and
	// the whole row is padded to innerW so its background fills the box width.
	// Running tools sweep the header label with the gradient scanner.
	glyphStr := gstyle.Background(bg).Render(glyph)
	sep := lipgloss.NewStyle().Background(bg).Render(" ")
	truncHdr := truncate(hdr, innerW-lipgloss.Width(glyph)-1-lipgloss.Width(foldIcon))
	var hdrStr string
	if st.Status == "running" {
		hdrStr = scannerFrame(truncHdr, m.animFrame, s.P)
	} else {
		hdrStr = s.Dim.Background(bg).Render(truncHdr)
	}
	headerRow := glyphStr + sep + hdrStr + s.Faint.Background(bg).Render(foldIcon)
	headerRow = lipgloss.NewStyle().Background(bg).Width(innerW).Render(headerRow)

	lines := []string{headerRow}

	// ── Error sub-line ─────────────────────────────────────────────────────────
	if st.Status == "error" && st.Error != "" {
		errLine := lipgloss.NewStyle().Foreground(s.P.Red).Background(bg).Width(innerW).
			Render(truncate(firstLine(st.Error), innerW))
		lines = append(lines, errLine)
	}

	// ── Expanded body: todos or output ─────────────────────────────────────────
	if !isCollapsed {
		switch {
		case isTodo && len(inp.Todos) > 0:
			if todoBlock := m.renderTodos(inp.Todos, innerW); todoBlock != "" {
				lines = append(lines, todoBlock)
			}
		case hasOutput:
			lines = append(lines, m.renderOutputPanel(st.Output, innerW))
		}
	}

	// Wrap the whole tool in one left-bar box with vertical padding on the panel
	// background — matches opencode's BlockTool (left ┃, padding 1, panel bg).
	box := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(s.P.BorderSoft).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(cw - 1) // the left border renders outside Width
	return box.Render(strings.Join(lines, "\n"))
}

// renderOutputPanel renders tool output as BgPanel-filled lines bounded to
// maxPanelLines, padded to innerW so no transparent cells escape. The enclosing
// toolRow box supplies the border + indent; this only fills the content lines.
// plan 08c §2d CRITICAL background fill: single Background(BgPanel) style per
// line padded to width, so every cell is painted even after ANSI resets.
func (m Model) renderOutputPanel(output string, innerW int) string {
	s := m.styles
	panelW := innerW
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
		sb.WriteString(lineStyle.Render(l) + "\n")
	}
	if truncated {
		more := len(allLines) - maxPanelLines
		hint := fmt.Sprintf("… %d more line%s", more, pluralS(more))
		sb.WriteString(lipgloss.NewStyle().Foreground(s.P.FgFaint).
			Background(s.P.BgPanel).Width(panelW).Render(hint))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// renderTodos renders a list of todoItems as checkbox glyphs with status colors
// inside a BgPanel block. Mirrors opencode todo-item.tsx:
//
//	completed  → [✓] green
//	in_progress → [•] amber
//	pending    → [ ] faint
func (m Model) renderTodos(todos []todoItem, innerW int) string {
	s := m.styles
	panelW := innerW
	if panelW < 10 {
		panelW = 10
	}

	var sb strings.Builder
	for _, td := range todos {
		var glyph string
		var fg lipgloss.Color
		switch td.Status {
		case "completed":
			glyph, fg = "[✓]", s.P.Green
		case "in_progress":
			glyph, fg = "[•]", s.P.Amber
		default: // pending
			glyph, fg = "[ ]", s.P.FgFaint
		}
		// Glyph and text share the same color; render the whole row through one
		// fg+bg style padded to panelW so every cell carries the panel background.
		sb.WriteString(lipgloss.NewStyle().Foreground(fg).Background(s.P.BgPanel).
			Width(panelW).Render(glyph+" "+td.Content) + "\n")
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
