package tui

// diffrender.go — Plan 17 Workstream C: shared inline diff renderer.
//
// Extracted from diff.go so the same guttered, syntax-highlighted, bg-tinted
// unified-diff render path is reused by:
//   - the full-screen reviewer (diff.go diffPatchPane), and
//   - the in-chat inline diff rendered at tool completion (toolrender.go), and
//   - the footer permission panel (Workstream B, when it lands).
//
// The helper is palette-pure (no Model dependency) so callers can use it from
// any render path that has a theme.Palette in hand. The Model methods in
// diff.go (renderGutter, renderDiffCodeLine) stay as 1-line wrappers around
// the free functions here so existing tests keep compiling.

import (
	"encoding/json"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// diffCacheKey is the composite cache key for a rendered inline diff. All
// four fields are included:
//   - partID: identifies the tool part (one diff per completed edit/apply_patch).
//   - patchHash: a SHA-256 prefix of the rendered payload (the metadata blob
//     that determines the diff body). A different patch → different hash →
//     cache miss. Defends against the rare case of a part's metadata arriving
//     after the first render.
//   - width: the diff wraps at this column; a resize must re-render.
//   - themeName: a theme switch changes all colors; cached output is stale.
type diffCacheKey struct {
	partID    string
	patchHash string
	width     int
	themeName string
}

// diffRenderCache is the model-level cache for rendered inline diffs. A plain
// map (not LRU) — bounded by the number of completed edit/apply_patch tool
// parts in a session (dozens, not thousands). Under a theme switch the old
// entries become unreachable and are GC'd.
type diffRenderCache map[diffCacheKey]string

// ensureDiffCache initialises the inline-diff render cache if nil. Called
// from New() so all Model copies share a non-nil map from birth, mirroring
// ensureMDCache.
func (m *Model) ensureDiffCache() {
	if m.diffCache == nil {
		m.diffCache = make(diffRenderCache)
	}
}

// cachedInlineDiff returns the cached rendered inline-diff string for the given
// (partID, payload, width, theme), computing it via renderInlineDiff on a miss.
// The payload is the part's State + Metadata bytes; its hash is part of the
// cache key so a metadata update after the first render triggers a re-render.
// The map is a reference type so writes are visible through all Model copies
// that share it.
func (m Model) cachedInlineDiff(p Part, st toolState, inp toolInput) string {
	width := m.contentWidth()
	if m.diffCache == nil {
		return renderInlineDiff(p.Tool, st, inp, m.styles.P, width)
	}
	hash := hashText(string(p.State) + string(st.Metadata))
	key := diffCacheKey{
		partID:    p.ID,
		patchHash: hash,
		width:     width,
		themeName: m.themeName,
	}
	if cached, ok := m.diffCache[key]; ok {
		return cached
	}
	rendered := renderInlineDiff(p.Tool, st, inp, m.styles.P, width)
	if rendered != "" {
		m.diffCache[key] = rendered
	}
	return rendered
}

// renderUnifiedDiff renders a unified patch with line-number gutter, per-row
// bg tints, +/- sign markers, and syntax highlighting keyed by filename. It is
// the shared helper extracted for Workstream C — used by the in-chat diff
// (toolrender.go) and reusable by the permission panel (Workstream B).
//
// Divergence from opencode's run mini-TUI: opencode uses transparent diff
// backgrounds; Opcode42 uses the theme's DiffPalette tints (AddedBg/RemovedBg/
// ContextBg) for readability and colorblind-safety (the +/- sign carries the
// color cue the bg alone doesn't). Logged in the known-divergence registry.
//
// The patch is rendered in full — no truncation. opencode renders the whole
// <diff> for every item (scrollback.writer.tsx:188-225); the terminal scrollback
// is the viewport. C matches this (no maxPanelLines cap).
func renderUnifiedDiff(patch string, filename string, p theme.Palette, width int) string {
	patch = strings.TrimRight(patch, "\n")
	if patch == "" || width <= gutterTotalWidth+1 {
		return ""
	}

	raw := strings.Split(patch, "\n")
	codeWidth := width - gutterTotalWidth
	if codeWidth < 1 {
		codeWidth = 1
	}

	// First pass: walk the patch to compute line numbers at each row. Hunk
	// headers reset the counters; other lines advance them. We build an
	// aligned slice of (oldLine, newLine) per output row so the render loop
	// below doesn't re-scan.
	oldLine, newLine := 0, 0
	lineNos := make([][2]int, len(raw))
	for i, line := range raw {
		if strings.HasPrefix(line, "@@") {
			if sub := hunkRe.FindStringSubmatch(line); sub != nil {
				o, _ := strconv.Atoi(sub[1])
				n, _ := strconv.Atoi(sub[2])
				oldLine, newLine = o-1, n-1
			}
			lineNos[i] = [2]int{-1, -1}
			continue
		}
		oldLine, newLine = advanceDiffLineNumbers(line, oldLine, newLine)
		lineNos[i] = [2]int{oldLine, newLine}
	}

	out := make([]string, 0, len(raw))
	for i, line := range raw {
		switch {
		case strings.HasPrefix(line, "@@"):
			// Hunk header — reset counters are already reflected in lineNos
			// (this row carries (-1,-1) so the gutter blanks both cells).
			gutterStr := renderGutterP(p, -1, -1, diffLineHunk)
			bodyStr := lipgloss.NewStyle().
				Foreground(p.Diff.HunkHeader).
				Background(p.Diff.ContextBg).
				Render(truncate(line, codeWidth))
			row := gutterStr + bodyStr
			out = append(out, padRow(row, width, p.Diff.ContextBg))

		default:
			kind := classifyDiffLine(line)
			ol, nl := lineNos[i][0], lineNos[i][1]
			gutterStr := renderGutterP(p, ol, nl, kind)
			bodyStr := renderDiffCodeLineP(p, line, kind, codeWidth, filename)
			row := gutterStr + bodyStr
			var rowBg theme.Color
			switch kind {
			case diffLineAdded:
				rowBg = p.Diff.AddedBg
			case diffLineRemoved:
				rowBg = p.Diff.RemovedBg
			default:
				rowBg = p.Diff.ContextBg
			}
			out = append(out, padRow(row, width, rowBg))
		}
	}
	return strings.Join(out, "\n")
}

// renderGutterP is the palette-pure counterpart of Model.renderGutter. The
// Model method delegates here so the inline-diff path and the full-screen
// reviewer share one implementation.
func renderGutterP(p theme.Palette, oldLine, newLine int, kind diffLineKind) string {
	d := p.Diff

	var oldBg, newBg, sepBg theme.Color
	switch kind {
	case diffLineAdded:
		oldBg, newBg, sepBg = d.AddedLineNumberBg, d.AddedLineNumberBg, d.AddedLineNumberBg
	case diffLineRemoved:
		oldBg, newBg, sepBg = d.RemovedLineNumberBg, d.RemovedLineNumberBg, d.RemovedLineNumberBg
	default:
		oldBg, newBg, sepBg = p.BgPanel, p.BgPanel, p.BgPanel
	}

	gutterStyle := func(bg theme.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(d.LineNumber).Background(bg)
	}

	var oldStr string
	if oldLine > 0 && (kind == diffLineRemoved || kind == diffLineContext) {
		oldStr = padLeft(strconv.Itoa(oldLine), gutterNumWidth)
	} else {
		oldStr = strings.Repeat(" ", gutterNumWidth)
	}

	var newStr string
	if newLine > 0 && (kind == diffLineAdded || kind == diffLineContext) {
		newStr = padLeft(strconv.Itoa(newLine), gutterNumWidth)
	} else {
		newStr = strings.Repeat(" ", gutterNumWidth)
	}

	oldCell := gutterStyle(oldBg).Render(oldStr)
	spaceCell := gutterStyle(sepBg).Render(" ")
	newCell := gutterStyle(newBg).Render(newStr)
	sepCell := lipgloss.NewStyle().Foreground(p.BorderSoft).Background(sepBg).Render(gutterSep)
	trailCell := gutterStyle(sepBg).Render(" ")
	return oldCell + spaceCell + newCell + sepCell + trailCell
}

// padLeft right-aligns s in a field of width by left-padding with spaces.
func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// renderDiffCodeLineP is the palette-pure counterpart of
// Model.renderDiffCodeLine. The Model method delegates here so the inline-diff
// path and the full-screen reviewer share one implementation.
func renderDiffCodeLineP(p theme.Palette, line string, kind diffLineKind, codeWidth int, filename string) string {
	d := p.Diff

	var rowBg, signFg theme.Color
	switch kind {
	case diffLineAdded:
		rowBg = d.AddedBg
		signFg = d.HighlightAdded
	case diffLineRemoved:
		rowBg = d.RemovedBg
		signFg = d.HighlightRemoved
	case diffLineMeta:
		return lipgloss.NewStyle().
			Foreground(p.FgFaint).
			Background(p.Bg).
			Render(truncate(line, codeWidth))
	default:
		rowBg = d.ContextBg
	}

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
			if strings.HasPrefix(line, " ") {
				code = line[1:]
			}
		}
	}
	signCell := lipgloss.NewStyle().Foreground(signFg).Background(rowBg).Render(sign)

	bodyWidth := codeWidth - signWidth
	if bodyWidth < 1 {
		bodyWidth = 1
	}
	code = truncate(code, bodyWidth)

	return signCell + highlightCodeBg(code, filename, p, rowBg)
}

// inlineDiffItem is one rendered title+diff block. opencode renders one per
// file in an apply_patch (scrollback.writer.tsx:190 .map over items). For a
// single-file edit there is one item.
type inlineDiffItem struct {
	title string
	body  string
}

// patchFileMeta is the apply_patch per-file metadata shape
// (apply_patch.ts:194-202): {filePath, relativePath, type, patch, additions,
// deletions, movePath}. We only read a subset.
type patchFileMeta struct {
	FilePath     string `json:"filePath"`
	RelativePath string `json:"relativePath"`
	Type         string `json:"type"` // "add" | "update" | "delete" | "move"
	Patch        string `json:"patch"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	MovePath     string `json:"movePath"`
}

// editMeta is the edit tool's metadata shape (edit.ts:204-208).
type editMeta struct {
	Diff string `json:"diff"`
}

// applyPatchMeta is the apply_patch tool's metadata shape (apply_patch.ts:297-301).
type applyPatchMeta struct {
	Diff  string          `json:"diff"`
	Files []patchFileMeta `json:"files"`
}

// patchTitle mirrors opencode's patchTitle (tool.ts:484-498):
// "# Created|Deleted|Moved|Patched <file>". add → Created; delete → Deleted;
// move → Moved; else → Patched.
func patchTitle(file patchFileMeta) string {
	rel := file.RelativePath
	if rel == "" {
		rel = file.FilePath
	}
	switch file.Type {
	case "add":
		return "# Created " + rel
	case "delete":
		return "# Deleted " + rel
	case "move":
		from := file.FilePath
		to := rel
		if file.MovePath != "" {
			to = file.MovePath
		}
		return "# Moved " + from + " -> " + to
	default:
		return "# Patched " + rel
	}
}

// renderInlineDiff renders the inline diff block(s) for a completed edit or
// apply_patch tool, returning one title+diff block per file. For an edit there
// is one block; for an apply_patch there is one per file in metadata.files.
// When a file has no patch text, the body is the "-N lines" hint (opencode's
// scrollback.writer.tsx:217-221).
//
// The returned string is the fully-rendered inline diff (title + body, joined
// by newlines), suitable for caching as a single string.
//
// Per-file +N -N stats appear in the title row (opencode's diff-viewer.tsx:829-834).
func renderInlineDiff(tool string, st toolState, inp toolInput, p theme.Palette, width int) string {
	if width <= gutterTotalWidth+1 {
		return ""
	}

	var items []inlineDiffItem
	switch tool {
	case "edit", "multiedit":
		var em editMeta
		if len(st.Metadata) == 0 || json.Unmarshal(st.Metadata, &em) != nil || strings.TrimSpace(em.Diff) == "" {
			return ""
		}
		file := inp.FilePath
		title := "# Edited " + file
		body := renderUnifiedDiff(em.Diff, file, p, width)
		items = []inlineDiffItem{{title: title, body: body}}

	case "apply_patch":
		var am applyPatchMeta
		if len(st.Metadata) == 0 || json.Unmarshal(st.Metadata, &am) != nil {
			return ""
		}
		for _, f := range am.Files {
			title := patchTitle(f)
			stats := diffStatsSuffix(f.Additions, f.Deletions, p)
			if stats != "" {
				title += " " + stats
			}
			var body string
			if strings.TrimSpace(f.Patch) == "" {
				body = noPatchHint(f, p, width)
			} else {
				name := f.RelativePath
				if name == "" {
					name = f.FilePath
				}
				body = renderUnifiedDiff(f.Patch, name, p, width)
			}
			items = append(items, inlineDiffItem{title: title, body: body})
		}
		if len(items) == 0 {
			return ""
		}
	default:
		return ""
	}

	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(p.FgFaint).Render(it.title))
		if it.body != "" {
			sb.WriteString("\n")
			sb.WriteString(it.body)
		}
	}
	return sb.String()
}

// diffStatsSuffix returns " +N -N" colored with the palette's Green/Red. When
// both are zero, returns "". Mirrors opencode's per-file stats in the
// diff-viewer file row (diff-viewer.tsx:829-834).
func diffStatsSuffix(adds, dels int, p theme.Palette) string {
	if adds == 0 && dels == 0 {
		return ""
	}
	var sb strings.Builder
	if adds > 0 {
		sb.WriteString(" ")
		sb.WriteString(lipgloss.NewStyle().Foreground(p.Green).Render("+" + strconv.Itoa(adds)))
	}
	if dels > 0 {
		sb.WriteString(" ")
		sb.WriteString(lipgloss.NewStyle().Foreground(p.Red).Render("-" + strconv.Itoa(dels)))
	}
	return sb.String()
}

// noPatchHint renders opencode's "-N lines" hint for a file with no textual
// patch (scrollback.writer.tsx:217-221). Used by apply_patch when a file's
// per-file patch is empty (e.g. a delete with no body).
func noPatchHint(f patchFileMeta, p theme.Palette, width int) string {
	n := f.Deletions
	line := "no textual patch"
	if n > 0 {
		line = "-" + strconv.Itoa(n) + " line" + pluralS(n)
	}
	return lipgloss.NewStyle().Foreground(p.Diff.Removed).Width(width).Render(line)
}
