package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	forgeclient "github.com/rotemmiz/forge/sdk/go"
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
func loadDiffCmd(ctx context.Context, c *forgeclient.ForgeClient, sessionID string) tea.Cmd {
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
	case " ", "enter":
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

// diffPatchPane renders the selected file's unified patch (folded → just a hint),
// colorized and windowed to height by the scroll offset.
func (m Model) diffPatchPane(width, height int) string {
	s := m.styles
	if m.diff.sel >= len(m.diff.files) {
		return padLines(nil, height) // keep the column height for separator alignment
	}
	f := m.diff.files[m.diff.sel]

	if m.diff.folded[m.diff.sel] {
		body := []string{s.Base.Bold(true).Render(truncate(f.File, width)), "", s.Faint.Render("(folded — space to expand)")}
		return padLines(body, height)
	}

	// Build the raw (unstyled) line list — header, blank, then the patch — and
	// window it BEFORE styling, so a huge patch only styles its visible rows.
	noPatch := strings.TrimSpace(f.Patch) == ""
	raw := []string{f.File, ""}
	if noPatch {
		raw = append(raw, "(no textual patch — "+f.Status+")")
	} else {
		raw = append(raw, strings.Split(f.Patch, "\n")...)
	}
	start, end := windowFrom(m.diff.scroll, len(raw), height)

	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		switch {
		case i == 0: // file header
			out = append(out, s.Base.Bold(true).Render(truncate(raw[i], width)))
		case i == 1: // blank spacer
			out = append(out, "")
		case noPatch: // the single sentinel line
			out = append(out, s.Faint.Render(raw[i]))
		default: // a unified-diff line
			out = append(out, m.diffLineStyle(raw[i]).Render(truncate(raw[i], width)))
		}
	}
	return padLines(out, height)
}

// diffLineStyle colors a unified-diff line by its leading marker.
func (m Model) diffLineStyle(line string) lipgloss.Style {
	s := m.styles
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "):
		return s.Faint
	case strings.HasPrefix(line, "@@"):
		return lipgloss.NewStyle().Foreground(s.P.Cyan)
	case strings.HasPrefix(line, "+"):
		return lipgloss.NewStyle().Foreground(s.P.Green)
	case strings.HasPrefix(line, "-"):
		return lipgloss.NewStyle().Foreground(s.P.Red)
	default:
		return s.Base
	}
}

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

// sortFileDiffs orders diffs by path so the tree + selection indexes are stable.
func sortFileDiffs(files []SnapshotFileDiff) {
	sort.Slice(files, func(i, j int) bool { return files[i].File < files[j].File })
}
