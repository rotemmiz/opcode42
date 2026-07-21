package tui

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Diff viewer (plan 08b §1). A full-screen reviewer over GET /session/{id}/diff:
// a file-tree pane on the left and the selected file's unified patch on the
// right (foldable, scrollable). v1 is unified + single-patch + file tree;
// split/side-by-side and a VCS working-tree source are follow-ups.

// SnapshotFileDiff is one file's change in a session diff (wire shape
// SnapshotFileDiff: file path, unified patch text, line counts, status).
type SnapshotFileDiff struct {
	File      string `json:"file"`
	Patch     string `json:"patch"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Status    string `json:"status"` // added | deleted | modified
}

// diffState is the full-screen diff reviewer's state (zero value = closed).
type diffState struct {
	open     bool
	loading  bool
	err      error
	files    []SnapshotFileDiff // sorted by path on load
	treeRows []diffRow          // flattened tree, cached on load (files-only dependency)
	sel      int                // selected file index (into files)
	scroll   int                // patch-pane scroll offset (lines)
	folded   map[int]bool       // file index -> patch collapsed
	showTree bool               // file-tree pane visible
}

// diffLoadedMsg carries a session diff (GET /session/{id}/diff).
type diffLoadedMsg struct {
	files []SnapshotFileDiff
	err   error
}

// loadDiffCmd fetches the session's accumulated diff (no messageID anchor in v1
// — a message cursor would let "diff this turn"; that's a follow-up).
func loadDiffCmd(ctx context.Context, c *opcode42client.Opcode42Client, sessionID string) tea.Cmd {
	return func() tea.Msg {
		var files []SnapshotFileDiff
		err := c.GetJSON(ctx, "/session/"+sessionID+"/diff", &files)
		return diffLoadedMsg{files: files, err: err}
	}
}

// openDiff opens the reviewer for the current session and kicks off the fetch.
func (m Model) openDiff() (Model, tea.Cmd) {
	if m.cfg.SessionID == "" {
		m.status = "open a session to review its diff"
		return m, nil
	}
	m.diff = diffState{open: true, loading: true, showTree: !m.diffTreeHidden, folded: map[int]bool{}}
	return m, loadDiffCmd(m.ctx, m.client, m.cfg.SessionID)
}

// handleDiffKey routes keys while the diff reviewer is open.
func (m Model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.diff = diffState{} // close + clear
		return m, nil
	case "down", "j", "n", "]":
		return m.diffMove(+1), nil
	case "up", "k", "p", "[":
		return m.diffMove(-1), nil
	case "g", "home":
		m.diff.sel, m.diff.scroll = 0, 0
		return m, nil
	case "G", "end":
		m.diff.sel, m.diff.scroll = len(m.diff.files)-1, 0
		if m.diff.sel < 0 {
			m.diff.sel = 0
		}
		return m, nil
	case "ctrl+d", "pgdown", "pgdn":
		m.diff.scroll += scrollStep
		return m, nil
	case "ctrl+u", "pgup":
		m.diff.scroll -= scrollStep
		if m.diff.scroll < 0 {
			m.diff.scroll = 0
		}
		return m, nil
	case " ", "space", "enter": // bubbletea v2: the space bar stringifies as "space"
		if len(m.diff.files) > 0 {
			m.diff.folded[m.diff.sel] = !m.diff.folded[m.diff.sel]
		}
		return m, nil
	case "t":
		m.diff.showTree = !m.diff.showTree
		m.diffTreeHidden = !m.diff.showTree
		m.persist()
		return m, nil
	}
	return m, nil
}

// diffMove changes the selected file by d (clamped) and resets the patch scroll.
func (m Model) diffMove(d int) Model {
	n := len(m.diff.files)
	if n == 0 {
		return m
	}
	m.diff.sel += d
	if m.diff.sel < 0 {
		m.diff.sel = 0
	}
	if m.diff.sel >= n {
		m.diff.sel = n - 1
	}
	m.diff.scroll = 0
	return m
}

// diffRow is one rendered tree line: a directory header (fileIdx < 0) or a file.
type diffRow struct {
	indent  int
	text    string
	fileIdx int
}

// diffTreeRows returns the flattened tree, preferring the cache built on load
// (falls back to building it for directly-constructed states, e.g. tests).
func (m Model) diffTreeRows() []diffRow {
	if m.diff.treeRows != nil {
		return m.diff.treeRows
	}
	return buildDiffTreeRows(m.diff.files)
}

// buildDiffTreeRows flattens the (path-sorted) files into directory headers +
// file rows, mirroring opencode's buildFileTree/flattenFileTree shape.
func buildDiffTreeRows(files []SnapshotFileDiff) []diffRow {
	var rows []diffRow
	var prev []string
	for fi, f := range files {
		parts := strings.Split(f.File, "/")
		dirs, base := parts[:len(parts)-1], parts[len(parts)-1]
		common := 0
		for common < len(dirs) && common < len(prev) && dirs[common] == prev[common] {
			common++
		}
		for d := common; d < len(dirs); d++ {
			rows = append(rows, diffRow{indent: d, text: dirs[d] + "/", fileIdx: -1})
		}
		rows = append(rows, diffRow{indent: len(dirs), text: base, fileIdx: fi})
		prev = dirs
	}
	return rows
}

const (
	diffTreeWidth = 34 // file-tree pane width (opencode uses 33)
	diffMinSplit  = 76 // below this, the tree pane is dropped even if enabled
)

// statusSign returns a one-char colored marker for a file's status.
func (m Model) statusSign(status string) string {
	s := m.styles
	switch status {
	case "added":
		return lipgloss.NewStyle().Foreground(s.P.Green).Render("A")
	case "deleted":
		return lipgloss.NewStyle().Foreground(s.P.Red).Render("D")
	default:
		return lipgloss.NewStyle().Foreground(s.P.Amber).Render("M")
	}
}

// diffView renders the full-screen reviewer.
func (m Model) diffView() string {
	s := m.styles
	switch {
	case m.diff.loading:
		return m.diffCenter(s.Faint.Render("loading diff…"))
	case m.diff.err != nil:
		return m.diffCenter(lipgloss.NewStyle().Foreground(s.P.Red).Render("diff failed: "+m.diff.err.Error()) + "\n\n" + s.Faint.Render("esc close"))
	case len(m.diff.files) == 0:
		return m.diffCenter(s.Base.Render("No changes in this session.") + "\n\n" + s.Faint.Render("esc close"))
	}

	width, height := m.width, m.height
	if width <= 0 {
		width = maxContentWidth
	}
	if height <= 0 {
		height = 24
	}

	// Summary header + key hints, then the panes fill the rest.
	var adds, dels int
	for _, f := range m.diff.files {
		adds, dels = adds+f.Additions, dels+f.Deletions
	}
	summary := s.Section.Render("Diff") + s.Faint.Render(fmt.Sprintf(" · %d files · ", len(m.diff.files))) +
		lipgloss.NewStyle().Foreground(s.P.Green).Render(fmt.Sprintf("+%d", adds)) + " " +
		lipgloss.NewStyle().Foreground(s.P.Red).Render(fmt.Sprintf("-%d", dels))
	hints := s.Faint.Render("j/k file · space fold · ctrl+d/u scroll · t tree · esc close")
	bodyH := height - 2 // summary + hints
	if bodyH < 1 {
		bodyH = 1
	}

	showTree := m.diff.showTree && width >= diffMinSplit
	patchW := width
	var panes string
	if showTree {
		patchW = width - diffTreeWidth - 1
		tree := m.diffTreePane(diffTreeWidth, bodyH)
		sep := strings.TrimRight(strings.Repeat(lipgloss.NewStyle().Foreground(s.P.BorderSoft).Render("│")+"\n", bodyH), "\n")
		patch := m.diffPatchPane(patchW, bodyH)
		panes = lipgloss.JoinHorizontal(lipgloss.Top, tree, sep, patch)
	} else {
		panes = m.diffPatchPane(patchW, bodyH)
	}

	return summary + "\n" + panes + "\n" + hints
}

// diffCenter centers a short message on the full screen.
func (m Model) diffCenter(body string) string {
	return centerScreen(m.width, m.height, body)
}

// diffTreePane renders the file-tree pane windowed around the selection.
func (m Model) diffTreePane(width, height int) string {
	s := m.styles
	rows := m.diffTreeRows()
	selRow := 0
	for i, r := range rows {
		if r.fileIdx == m.diff.sel {
			selRow = i
			break
		}
	}
	start, end := windowAround(selRow, len(rows), height)

	var lines []string
	for i := start; i < end; i++ {
		r := rows[i]
		pad := strings.Repeat("  ", r.indent)
		if r.fileIdx < 0 { // directory header
			lines = append(lines, s.Faint.Render(truncate(pad+r.text, width)))
			continue
		}
		f := m.diff.files[r.fileIdx]
		label := pad + m.statusSign(f.Status) + " " + r.text
		if r.fileIdx == m.diff.sel { // selection bar spans the row (stats omitted)
			lines = append(lines, s.Selection.Width(width).Render(truncate(label, width)))
			continue
		}
		stats := lipgloss.NewStyle().Foreground(s.P.Green).Render(fmt.Sprintf("+%d", f.Additions)) + " " +
			lipgloss.NewStyle().Foreground(s.P.Red).Render(fmt.Sprintf("-%d", f.Deletions))
		name := truncate(label, width-lipgloss.Width(stats)-1)
		gap := width - lipgloss.Width(name) - lipgloss.Width(stats)
		if gap < 1 {
			gap = 1
		}
		lines = append(lines, name+strings.Repeat(" ", gap)+stats)
	}
	for len(lines) < height { // pad to full height so the separator aligns
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// hunkRe matches the @@ -oldStart[,oldLen] +newStart[,newLen] @@ header.
// Capture groups: (1) oldStart, (2) newStart.
var hunkRe = regexp.MustCompile(`^@@\s+-(\d+)(?:,\d+)?\s+\+(\d+)(?:,\d+)?\s+@@`)

// gutterWidth is the number of visible columns reserved for the line-number
// gutter (two numbers + a separator, e.g. "123 456 │ ").  The format is
// right-aligned in a fixed field of gutterNumWidth digits each.
const (
	gutterNumWidth = 4   // digits per side (handles up to 9999-line files)
	gutterSep      = "│" // vertical bar between gutter and code body
	// gutterTotalWidth is gutterNumWidth*2 + 1 (space) + 1 (sep) + 1 (space) = 11
	gutterTotalWidth = gutterNumWidth*2 + 3
	// signWidth is the column between the gutter and the code body that carries
	// the +/-/space marker (colored with Diff.HighlightAdded/Removed, mirroring
	// opencode's addedSignColor/removedSignColor). It is 1 visible char.
	signWidth = 1
)

// diffLineKind categorises a unified-diff source line.
type diffLineKind int

const (
	diffLineContext diffLineKind = iota // space-prefixed or bare context
	diffLineAdded                       // +
	diffLineRemoved                     // -
	diffLineHunk                        // @@
	diffLineMeta                        // ---/+++/diff /index
)

// classifyDiffLine returns the kind of a unified-diff line.
func classifyDiffLine(line string) diffLineKind {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "):
		return diffLineMeta
	case strings.HasPrefix(line, "@@"):
		return diffLineHunk
	case strings.HasPrefix(line, "+"):
		return diffLineAdded
	case strings.HasPrefix(line, "-"):
		return diffLineRemoved
	default:
		return diffLineContext
	}
}

// diffPatchPane renders the selected file's unified patch (folded → just a hint),
// full-row background-tinted with a line-number gutter, syntax-highlighted code
// bodies, and windowed to height by the scroll offset.
//
// M6 changes vs the old implementation:
//  1. Full-row background tint: each diff line row is painted end-to-end with
//     Diff.AddedBg / Diff.RemovedBg / Diff.ContextBg. The old implementation
//     only colored the text spans.
//  2. Line-number gutter: parsed from @@ hunk headers; old/new numbers tracked
//     per line and rendered in a left gutter with its own bg.
//  3. Hunk headers: rendered in Diff.HunkHeader color on Diff.ContextBg.
//  4. Syntax-highlighted code bodies: each code line (sign stripped) is passed
//     through highlightCodeBg() with the row background, so syntax fg is
//     composed with the diff bg without transparent gaps.
//  5. Colored +/- sign marker: renderDiffCodeLine prefixes each code body with a
//     one-column +/-/space marker colored with Diff.HighlightAdded/Removed
//     (opencode's addedSignColor/removedSignColor) — a colorblind-safe cue the
//     background tint alone doesn't give.
//
// Anti-bleed: highlightCodeBg sets Background(rowBg) on every token style.
// After the syntax-highlighted body we post-pad each row to exactly width with
// the row background so trailing cells are painted and lipgloss.Width(row)==width.
func (m Model) diffPatchPane(width, height int) string {
	s := m.styles
	if m.diff.sel >= len(m.diff.files) {
		return padLines(nil, height)
	}
	f := m.diff.files[m.diff.sel]

	if m.diff.folded[m.diff.sel] {
		body := []string{
			s.Base.Bold(true).Render(truncate(f.File, width)),
			"",
			s.Faint.Render("(folded — space to expand)"),
		}
		return padLines(padDiffLines(body, width, m.styles.P.Bg), height)
	}

	// Build raw line list (header + blank + patch lines) and window before styling.
	noPatch := strings.TrimSpace(f.Patch) == ""
	raw := []string{f.File, ""}
	if noPatch {
		raw = append(raw, "(no textual patch — "+f.Status+")")
	} else {
		raw = append(raw, strings.Split(f.Patch, "\n")...)
	}
	start, end := windowFrom(m.diff.scroll, len(raw), height)

	// Scan from the beginning of the patch to compute line numbers at the
	// window start. This is O(patch lines) but patches are short in practice.
	// Hunk headers reset the counters; other lines advance them.
	oldLine, newLine := 0, 0
	for i := 2; i < start && i < len(raw); i++ {
		l := raw[i]
		if strings.HasPrefix(l, "@@") {
			if sub := hunkRe.FindStringSubmatch(l); sub != nil {
				o, _ := strconv.Atoi(sub[1])
				n, _ := strconv.Atoi(sub[2])
				oldLine = o - 1
				newLine = n - 1
			}
		} else {
			oldLine, newLine = advanceDiffLineNumbers(l, oldLine, newLine)
		}
	}

	// Code width = total pane width minus the gutter.
	codeWidth := width - gutterTotalWidth
	if codeWidth < 1 {
		codeWidth = 1
	}

	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		line := raw[i]
		switch {
		case i == 0: // file header — no gutter
			row := s.Base.Bold(true).Render(truncate(line, width))
			out = append(out, padRow(row, width, s.P.Bg))

		case i == 1: // blank spacer — no gutter
			out = append(out, padRow("", width, s.P.Bg))

		case noPatch: // sentinel — no gutter
			out = append(out, padRow(s.Faint.Render(line), width, s.P.Bg))

		case strings.HasPrefix(line, "@@"): // hunk header
			// Parse to reset line-number counters.
			if sub := hunkRe.FindStringSubmatch(line); sub != nil {
				oldLine, _ = strconv.Atoi(sub[1])
				newLine, _ = strconv.Atoi(sub[2])
				// Hunk headers use 1-based numbers; subtract 1 so the first
				// advanceDiffLineNumbers call on the next content line yields the
				// correct starting number.
				oldLine--
				newLine--
			}
			gutterStr := m.renderGutter(-1, -1, diffLineHunk)
			bodyStr := lipgloss.NewStyle().
				Foreground(s.P.Diff.HunkHeader).
				Background(s.P.Diff.ContextBg).
				Render(truncate(line, codeWidth))
			row := gutterStr + bodyStr
			out = append(out, padRow(row, width, s.P.Diff.ContextBg))
			// Hunk header does not consume a source line.

		default: // +, -, or context code line
			kind := classifyDiffLine(line)
			oldLine, newLine = advanceDiffLineNumbers(line, oldLine, newLine)
			gutterStr := m.renderGutter(oldLine, newLine, kind)
			bodyStr := m.renderDiffCodeLine(line, kind, codeWidth, f.File)
			row := gutterStr + bodyStr
			var rowBg theme.Color
			switch kind {
			case diffLineAdded:
				rowBg = s.P.Diff.AddedBg
			case diffLineRemoved:
				rowBg = s.P.Diff.RemovedBg
			default:
				rowBg = s.P.Diff.ContextBg
			}
			out = append(out, padRow(row, width, rowBg))
		}
	}
	// Pad remaining lines to height with empty bg-filled rows so the pane is
	// exactly height lines tall and every line is exactly width visible chars.
	return padLinesWidth(out, height, width, s.P.Bg)
}

// advanceDiffLineNumbers updates the old/new line number counters based on the
// type of unified-diff line. Called in patch-line order.
//
// Convention:
//   - '+' lines (not "+++") advance newLine only.
//   - '-' lines (not "---") advance oldLine only.
//   - context lines (space or bare) advance both.
//   - meta (---/+++/diff/index) and hunk (@@) lines: no advance (caller handles @@).
//
// IMPORTANT: meta patterns (---/+++) must be checked BEFORE the single-char
// +/- patterns because "---" has prefix "-" and "+++" has prefix "+".
func advanceDiffLineNumbers(line string, oldLine, newLine int) (int, int) {
	switch {
	// Meta lines — must be checked before single-char +/- to avoid misclassifying
	// "---" as a removed line and "+++" as an added line.
	case strings.HasPrefix(line, "@@"),
		strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "+++"),
		strings.HasPrefix(line, "diff "),
		strings.HasPrefix(line, "index "):
		return oldLine, newLine
	case strings.HasPrefix(line, "+"):
		return oldLine, newLine + 1
	case strings.HasPrefix(line, "-"):
		return oldLine + 1, newLine
	default: // context (space-prefixed or bare)
		return oldLine + 1, newLine + 1
	}
}

// renderGutter renders the fixed-width left gutter showing old/new line numbers.
// For added lines: blank old number, newLine shown. For removed: oldLine shown,
// blank new number. For context: both numbers. For hunk/meta: both blank.
//
// Gutter format (gutterTotalWidth = 11 visible chars):
//
//	"OOOO NNNN │ "
//	 old  new  sep
//
// where OOOO/NNNN are right-aligned in gutterNumWidth=4. A value of -1 means
// "do not show" → rendered as spaces.
func (m Model) renderGutter(oldLine, newLine int, kind diffLineKind) string {
	s := m.styles
	d := s.P.Diff

	// Determine background colors for old-number and new-number cells.
	var oldBg, newBg, sepBg theme.Color
	switch kind {
	case diffLineAdded:
		oldBg = d.AddedLineNumberBg
		newBg = d.AddedLineNumberBg
		sepBg = d.AddedLineNumberBg
	case diffLineRemoved:
		oldBg = d.RemovedLineNumberBg
		newBg = d.RemovedLineNumberBg
		sepBg = d.RemovedLineNumberBg
	default:
		// context, hunk header, meta
		oldBg = s.P.BgPanel
		newBg = s.P.BgPanel
		sepBg = s.P.BgPanel
	}

	gutterStyle := func(bg theme.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(d.LineNumber).Background(bg)
	}

	// Format old-line number cell.
	var oldStr string
	if oldLine > 0 && (kind == diffLineRemoved || kind == diffLineContext) {
		oldStr = fmt.Sprintf("%*d", gutterNumWidth, oldLine)
	} else {
		oldStr = strings.Repeat(" ", gutterNumWidth)
	}

	// Format new-line number cell.
	var newStr string
	if newLine > 0 && (kind == diffLineAdded || kind == diffLineContext) {
		newStr = fmt.Sprintf("%*d", gutterNumWidth, newLine)
	} else {
		newStr = strings.Repeat(" ", gutterNumWidth)
	}

	oldCell := gutterStyle(oldBg).Render(oldStr)
	spaceCell := gutterStyle(sepBg).Render(" ")
	newCell := gutterStyle(newBg).Render(newStr)
	sepCell := lipgloss.NewStyle().Foreground(s.P.BorderSoft).Background(sepBg).Render(gutterSep)
	trailCell := gutterStyle(sepBg).Render(" ")
	// Gutter: "OOOO NNNN │ " (11 visible chars)
	return oldCell + spaceCell + newCell + sepCell + trailCell
}

// renderDiffCodeLine renders the code body of a single diff line: a colored
// +/-/space sign marker followed by the syntax-highlighted code, all on the
// row's diff background, truncated to codeWidth (sign column included).
//
// The leading +/- sign is colored with Diff.HighlightAdded / HighlightRemoved
// (opencode's addedSignColor / removedSignColor — `<diff>` props in
// routes/session/index.tsx:2212). It is the colorblind-friendly cue that the
// background tint alone does not provide.
//
// For meta lines (---/+++/diff/index), the whole line is rendered in the faint
// color (no syntax — these are not source code).
func (m Model) renderDiffCodeLine(line string, kind diffLineKind, codeWidth int, filename string) string {
	s := m.styles
	d := s.P.Diff

	var rowBg, signFg theme.Color
	switch kind {
	case diffLineAdded:
		rowBg = d.AddedBg
		signFg = d.HighlightAdded
	case diffLineRemoved:
		rowBg = d.RemovedBg
		signFg = d.HighlightRemoved
	case diffLineMeta:
		// meta lines (---/+++/diff/index): faint, no highlighting
		return lipgloss.NewStyle().
			Foreground(s.P.FgFaint).
			Background(s.P.Bg).
			Render(truncate(line, codeWidth))
	default:
		rowBg = d.ContextBg
	}

	// Split the leading marker (+/-/ ) from the raw code. Added/removed lines
	// surface a colored marker; context lines pad the column with a blank space
	// (still on rowBg) so the code body aligns across line kinds.
	sign := " "
	code := line
	if len(line) > 0 {
		switch kind {
		case diffLineAdded:
			sign = "+"
			code = line[1:]
		case diffLineRemoved:
			sign = "-"
			code = line[1:]
		default:
			// Context lines: drop a single leading space marker if present, but
			// keep bare (space-less) context lines intact.
			if strings.HasPrefix(line, " ") {
				code = line[1:]
			}
		}
	}
	signCell := lipgloss.NewStyle().Foreground(signFg).Background(rowBg).Render(sign)

	// Truncate the code to the width left after the sign column before
	// highlighting so we don't syntax-highlight characters that will be clipped.
	// This avoids any off-by-one in ANSI-length accounting after truncation.
	bodyWidth := codeWidth - signWidth
	if bodyWidth < 1 {
		bodyWidth = 1
	}
	code = truncate(code, bodyWidth)

	// Syntax-highlight with the row's diff background color so every token
	// span carries the bg — no transparent cells between tokens.
	return signCell + highlightCodeBg(code, filename, s.P, rowBg)
}

// padRow pads a rendered row string (which may contain ANSI escapes) to exactly
// width visible columns by appending spaces with Background(bg). This ensures
// the rightmost cells are painted with the row background and no transparent
// trailing gap exists.
//
// Mechanism: lipgloss.Width() strips ANSI and returns visible column count.
// We subtract the visible width of row from width to get the pad length, then
// render that many spaces with Background(bg). If row is already >= width we
// truncate using lipgloss's own ansi-aware truncation via Render(Width(width)).
func padRow(row string, width int, bg theme.Color) string {
	visible := lipgloss.Width(row)
	if visible >= width {
		// Truncate to exactly width using a lipgloss Width constraint so ANSI
		// accounting is correct. We pass through a plain style (no extra colors)
		// because the row already has all its colors embedded.
		return lipgloss.NewStyle().MaxWidth(width).Render(row)
	}
	pad := width - visible
	trailing := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
	return row + trailing
}

// padDiffLines pads each element of lines to width using padRow, so that
// non-diff rows (file header, spacer, folded hint) are also background-filled.
func padDiffLines(lines []string, width int, bg theme.Color) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = padRow(l, width, bg)
	}
	return out
}

// TODO(08c M6 split-mode): when split-mode is implemented, add a
// diffLineStyleSimple helper that maps +/-/@@/meta → a lipgloss.Style for the
// split-pane's simpler rendering path (no gutter, no bg-tint compositing).
// For now the unified-mode path (diffPatchPane) handles everything via
// renderDiffCodeLine + renderGutter.

// padLines pads (or trims) a block to exactly height lines.
func padLines(lines []string, height int) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// padLinesWidth is like padLines but additionally ensures that every line is
// exactly width visible characters wide by padding short lines with bg-colored
// spaces. This is used by diffPatchPane to guarantee full-width fill on every
// row including the empty padding lines at the bottom of a short patch.
func padLinesWidth(lines []string, height, width int, bg theme.Color) string {
	if len(lines) > height {
		lines = lines[:height]
	}
	out := make([]string, len(lines), height)
	for i, l := range lines {
		out[i] = padRow(l, width, bg)
	}
	emptyRow := padRow("", width, bg)
	for len(out) < height {
		out = append(out, emptyRow)
	}
	return strings.Join(out, "\n")
}

// sortFileDiffs orders diffs by path so the tree + selection indexes are stable.
func sortFileDiffs(files []SnapshotFileDiff) {
	sort.Slice(files, func(i, j int) bool { return files[i].File < files[j].File })
}
