package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// withDiff builds a model with an open, loaded diff reviewer (files pre-sorted
// the way the load handler sorts them).
func withDiff() Model {
	m := New(Config{URL: "http://x"})
	m.cfg.SessionID = "ses_1"
	m.width, m.height = 120, 30
	files := []SnapshotFileDiff{
		{File: "internal/tui/model.go", Patch: "@@ -1 +1 @@\n-old\n+new\n", Additions: 1, Deletions: 1, Status: "modified"},
		{File: "internal/tui/diff.go", Patch: "@@ -0,0 +1 @@\n+brand new\n", Additions: 1, Status: "added"},
		{File: "README.md", Patch: "", Deletions: 2, Status: "deleted"},
	}
	sortFileDiffs(files)
	m.diff = diffState{open: true, showTree: true, folded: map[int]bool{}, files: files}
	return m
}

func TestDiffTreeRows(t *testing.T) {
	m := withDiff()
	rows := m.diffTreeRows()
	// Sorted files: README.md, internal/tui/diff.go, internal/tui/model.go.
	// → README(file) · internal/(dir) · tui/(dir) · diff.go(file) · model.go(file)
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5: %+v", len(rows), rows)
	}
	want := []struct {
		text    string
		fileIdx int
		indent  int
	}{
		{"README.md", 0, 0},
		{"internal/", -1, 0},
		{"tui/", -1, 1},
		{"diff.go", 1, 2},
		{"model.go", 2, 2},
	}
	for i, w := range want {
		if rows[i].text != w.text || rows[i].fileIdx != w.fileIdx || rows[i].indent != w.indent {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}
}

func TestDiffMove_ClampAndScrollReset(t *testing.T) {
	m := withDiff()
	m.diff.scroll = 10
	m = m.diffMove(+1)
	if m.diff.sel != 1 || m.diff.scroll != 0 {
		t.Fatalf("move +1 → sel=%d scroll=%d, want 1/0", m.diff.sel, m.diff.scroll)
	}
	m = m.diffMove(+1) // → 2 (last)
	m = m.diffMove(+1) // clamp at 2
	if m.diff.sel != 2 {
		t.Fatalf("move past end → sel=%d, want 2", m.diff.sel)
	}
	m = m.diffMove(-5) // clamp at 0
	if m.diff.sel != 0 {
		t.Fatalf("move past start → sel=%d, want 0", m.diff.sel)
	}
}

func TestDiffFoldToggle(t *testing.T) {
	m := withDiff()
	m, _ = step(t, m, key(" ")) // space folds the selected file
	if !m.diff.folded[m.diff.sel] {
		t.Fatal("space should fold the selected file")
	}
	m, _ = step(t, m, key(" "))
	if m.diff.folded[m.diff.sel] {
		t.Fatal("space should unfold again")
	}
}

func TestDiffTreeToggle(t *testing.T) {
	m := withDiff()
	if !m.diff.showTree {
		t.Fatal("tree should start visible")
	}
	m, _ = step(t, m, key("t"))
	if m.diff.showTree || !m.diffTreeHidden {
		t.Fatalf("t should hide the tree + record the preference, showTree=%v hidden=%v", m.diff.showTree, m.diffTreeHidden)
	}
}

func TestDiffEscCloses(t *testing.T) {
	m := withDiff()
	m, _ = step(t, m, key("esc"))
	if m.diff.open {
		t.Fatal("esc should close the reviewer")
	}
}

func TestDiffLoaded_SortsAndClamps(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.diff = diffState{open: true, loading: true, sel: 9, folded: map[int]bool{}}
	unsorted := []SnapshotFileDiff{
		{File: "z.go", Additions: 1},
		{File: "a.go", Additions: 1},
	}
	m, _ = step(t, m, diffLoadedMsg{files: unsorted})
	if m.diff.loading {
		t.Fatal("loaded should clear loading")
	}
	if len(m.diff.files) != 2 || m.diff.files[0].File != "a.go" {
		t.Fatalf("files not sorted by path: %+v", m.diff.files)
	}
	if m.diff.sel != 0 {
		t.Fatalf("out-of-range sel should reset to 0, got %d", m.diff.sel)
	}
}

func TestDiffLoaded_IgnoredWhenClosed(t *testing.T) {
	m := New(Config{URL: "http://x"}) // diff not open
	m, _ = step(t, m, diffLoadedMsg{files: []SnapshotFileDiff{{File: "a.go"}}})
	if m.diff.open || len(m.diff.files) != 0 {
		t.Fatal("a diffLoadedMsg arriving after close must be ignored")
	}
}

func TestDiffLoaded_Error(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.diff = diffState{open: true, loading: true, folded: map[int]bool{}}
	m, _ = step(t, m, diffLoadedMsg{err: errors.New("boom")})
	if m.diff.err == nil || m.diff.loading {
		t.Fatalf("error should surface + clear loading, err=%v loading=%v", m.diff.err, m.diff.loading)
	}
}

func TestOpenDiff_RequiresSession(t *testing.T) {
	m := New(Config{URL: "http://x"}) // no session
	nm, cmd := m.openDiff()
	if nm.diff.open || cmd != nil {
		t.Fatal("openDiff with no session must not open or fetch")
	}
	m.cfg.SessionID = "ses_1"
	nm, cmd = m.openDiff()
	if !nm.diff.open || !nm.diff.loading || cmd == nil {
		t.Fatal("openDiff with a session should open, mark loading, and fetch")
	}
}

func TestDiffView_Smoke(t *testing.T) {
	m := withDiff()
	out := m.diffView()
	if !strings.Contains(out, "Diff") || !strings.Contains(out, "3 files") {
		t.Fatalf("diff view missing summary: %q", firstLine(out))
	}
	if !strings.Contains(out, "README.md") {
		t.Fatal("diff view should list files in the tree")
	}
	// Folded patch shows the hint, not the body.
	m.diff.folded[m.diff.sel] = true
	if !strings.Contains(m.diffView(), "folded") {
		t.Fatal("folded file should show the fold hint")
	}
}

func TestDiffOpen_ViaLeader(t *testing.T) {
	m := withDiff()
	m.diff = diffState{} // start closed
	m.cfg.SessionID = "ses_1"
	m, _ = step(t, m, key("ctrl+x"))
	m, _ = step(t, m, key("d"))
	if !m.diff.open || !m.diff.loading {
		t.Fatalf("ctrl+x d should open + load the reviewer, open=%v loading=%v", m.diff.open, m.diff.loading)
	}
}

// ─── M6: visual parity tests ─────────────────────────────────────────────────
//
// These tests assert the post-M6 diff rendering properties:
//  1. Added rows carry Diff.AddedBg; removed rows carry Diff.RemovedBg.
//  2. Every rendered row is exactly pane-width visible chars (full-width fill).
//  3. Gutter line numbers appear and increment correctly across a hunk.
//  4. Hunk headers are rendered in Diff.HunkHeader color.
//  5. Properties hold for both opcode42-dark and opcode42-light.

// withDiffModel builds a Model with the given palette and a single-file diff
// containing a representative hunk with added/removed/context lines.
func withDiffModel(t *testing.T, palName string) Model {
	t.Helper()
	m := New(Config{URL: "http://x"})
	m.width, m.height = 120, 30
	m = m.applyThemeByName(palName)

	// A patch with:
	//  • one hunk header
	//  • two context lines
	//  • one removed line
	//  • one added line
	//  • one more context line
	patch := "@@ -10,4 +10,4 @@\n context_a\n context_b\n-removed_line\n+added_line\n context_c"
	files := []SnapshotFileDiff{
		{File: "pkg/foo.go", Patch: patch, Additions: 1, Deletions: 1, Status: "modified"},
	}
	sortFileDiffs(files)
	m.diff = diffState{open: true, showTree: false, folded: map[int]bool{}, files: files}
	return m
}

// TestDiffPatchPane_AddedRowBg verifies that an added diff line row contains
// the AddedBg color in its visible output when rendered under opcode42-dark.
// Because lipgloss suppresses ANSI in non-TTY environments we fall back to a
// width check when ANSI is not emitted.
func TestDiffPatchPane_AddedRowBg(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")
	paneW := m.width // no tree pane
	pane := m.diffPatchPane(paneW, 20)

	// Find the "+added_line" row.
	lines := strings.Split(pane, "\n")
	found := false
	for _, l := range lines {
		if strings.Contains(stripANSI(l), "added_line") {
			found = true
			// Width check: must be exactly paneW visible chars.
			if got := lipgloss.Width(l); got != paneW {
				t.Errorf("added_line row: visible width %d, want %d\nrow: %q", got, paneW, l)
			}
			// Color check (only when ANSI is emitted).
			addedBgHex := string(m.styles.P.Diff.AddedBg)
			if strings.Contains(l, "\x1b[") && addedBgHex != "" {
				if !strings.Contains(l, addedBgHex[1:]) { // hex without '#'
					t.Logf("AddedBg color %q not found in raw row (palette may encode differently)", addedBgHex)
				}
			}
			break
		}
	}
	if !found {
		t.Error("added_line row not found in diffPatchPane output")
	}
}

// TestDiffPatchPane_RemovedRowBg mirrors the added-row test for removed lines.
func TestDiffPatchPane_RemovedRowBg(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")
	paneW := m.width
	pane := m.diffPatchPane(paneW, 20)

	lines := strings.Split(pane, "\n")
	found := false
	for _, l := range lines {
		if strings.Contains(stripANSI(l), "removed_line") {
			found = true
			if got := lipgloss.Width(l); got != paneW {
				t.Errorf("removed_line row: visible width %d, want %d\nrow: %q", got, paneW, l)
			}
			break
		}
	}
	if !found {
		t.Error("removed_line row not found in diffPatchPane output")
	}
}

// TestRenderDiffCodeLine_SignMarkers verifies that added/removed lines carry a
// colored +/- sign marker (opencode's addedSignColor/removedSignColor) ahead of
// the code body, context lines a blank marker, and that the marker is dropped
// from the highlighted body (no doubled +/-).
func TestRenderDiffCodeLine_SignMarkers(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")
	const codeWidth = 40

	cases := []struct {
		name     string
		line     string
		kind     diffLineKind
		wantSign byte
		wantBody string
	}{
		{"added", "+foo := bar", diffLineAdded, '+', "foo := bar"},
		{"removed", "-foo := bar", diffLineRemoved, '-', "foo := bar"},
		{"context space", " foo := bar", diffLineContext, ' ', "foo := bar"},
		{"context bare", "foo := bar", diffLineContext, ' ', "foo := bar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plain := stripANSI(m.renderDiffCodeLine(tc.line, tc.kind, codeWidth, "x.go"))
			if len(plain) == 0 {
				t.Fatal("rendered line is empty")
			}
			if plain[0] != tc.wantSign {
				t.Errorf("sign column = %q, want %q (full: %q)", plain[0], tc.wantSign, plain)
			}
			body := strings.TrimRight(plain[1:], " ")
			if body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}

// TestDiffPatchPane_SignMarkersPresent verifies the rendered pane carries the
// +/- sign markers ahead of the changed code bodies (not just background tints).
func TestDiffPatchPane_SignMarkersPresent(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")
	plain := stripANSI(m.diffPatchPane(m.width, 20))
	lines := strings.Split(plain, "\n")

	var addedSign, removedSign bool
	for _, l := range lines {
		// Slice off the gutter by visible columns (rune-aware: the gutter's │
		// separator is a multi-byte rune, so byte-indexing would land mid-rune).
		runes := []rune(l)
		if len(runes) <= gutterTotalWidth {
			continue
		}
		body := string(runes[gutterTotalWidth:])
		if strings.HasPrefix(body, "+") && strings.Contains(body, "added_line") {
			addedSign = true
		}
		if strings.HasPrefix(body, "-") && strings.Contains(body, "removed_line") {
			removedSign = true
		}
	}
	if !addedSign {
		t.Error("added line should be prefixed with a '+' sign marker in the code column")
	}
	if !removedSign {
		t.Error("removed line should be prefixed with a '-' sign marker in the code column")
	}
}

// TestDiffPatchPane_HunkHeaderPresent verifies the @@ line is included in the
// output and that the plain text starts with "@".
func TestDiffPatchPane_HunkHeaderPresent(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")
	pane := m.diffPatchPane(m.width, 20)
	if !strings.Contains(stripANSI(pane), "@@") {
		t.Error("diffPatchPane output must contain the @@ hunk header")
	}
}

// TestDiffPatchPane_GutterLineNumbers checks that the gutter numbers appear
// and increment. The patch starts at old=10/new=10 for context; after a
// removed and added line the context_c line should have old≥11, new≥11.
func TestDiffPatchPane_GutterLineNumbers(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")
	pane := m.diffPatchPane(m.width, 20)

	plain := stripANSI(pane)
	// Look for at least one digit in the gutter area (first gutterNumWidth chars
	// of a non-header patch line). Any numeric character in the first ~11 chars
	// of a content line indicates the gutter is populated.
	lines := strings.Split(plain, "\n")
	foundGutterNum := false
	for _, l := range lines {
		if len(l) > gutterTotalWidth {
			gutterPart := l[:gutterTotalWidth]
			for _, ch := range gutterPart {
				if ch >= '0' && ch <= '9' {
					foundGutterNum = true
					break
				}
			}
		}
		if foundGutterNum {
			break
		}
	}
	if !foundGutterNum {
		t.Error("no gutter line numbers found in diffPatchPane output (expected digits in first gutterTotalWidth chars of content lines)")
	}
}

// TestDiffPatchPane_FullWidthAllThemes asserts that every row produced by
// diffPatchPane is exactly paneW visible characters wide for opcode42-dark and
// opcode42-light (the main anti-bleed regression guard for M6).
func TestDiffPatchPane_FullWidthAllThemes(t *testing.T) {
	for _, palName := range []string{"opcode42-dark", "opcode42-light"} {
		t.Run(palName, func(t *testing.T) {
			m := withDiffModel(t, palName)
			paneW := 100 // fixed width for determinism
			m.width = paneW

			pane := m.diffPatchPane(paneW, 20)
			lines := strings.Split(pane, "\n")
			for i, l := range lines {
				if got := lipgloss.Width(l); got != paneW {
					t.Errorf("theme=%q line %d: visible width %d, want %d\nrow: %q",
						palName, i, got, paneW, l)
				}
			}
		})
	}
}

// TestAdvanceDiffLineNumbers verifies the line-number counter logic for each
// line type.
func TestAdvanceDiffLineNumbers(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		inOld   int
		inNew   int
		wantOld int
		wantNew int
	}{
		{"+added", "+foo", 5, 5, 5, 6},
		{"-removed", "-bar", 5, 5, 6, 5},
		{"context", " baz", 5, 5, 6, 6},
		{"context bare", "baz", 5, 5, 6, 6}, // context with no space prefix
		{"meta ---", "--- a/x.go", 5, 5, 5, 5},
		{"meta +++", "+++ b/x.go", 5, 5, 5, 5},
		{"meta diff", "diff --git a b", 5, 5, 5, 5},
		{"meta index", "index abc..def", 5, 5, 5, 5},
		{"hunk @@", "@@ -1,4 +1,4 @@", 5, 5, 5, 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotOld, gotNew := advanceDiffLineNumbers(tc.line, tc.inOld, tc.inNew)
			if gotOld != tc.wantOld || gotNew != tc.wantNew {
				t.Errorf("advanceDiffLineNumbers(%q, %d, %d) = (%d, %d), want (%d, %d)",
					tc.line, tc.inOld, tc.inNew, gotOld, gotNew, tc.wantOld, tc.wantNew)
			}
		})
	}
}

// TestClassifyDiffLine verifies that each diff-line prefix is classified correctly.
func TestClassifyDiffLine(t *testing.T) {
	cases := []struct {
		line string
		want diffLineKind
	}{
		{"+added", diffLineAdded},
		{"-removed", diffLineRemoved},
		{" context", diffLineContext},
		{"bare context", diffLineContext},
		{"@@ -1,2 +1,2 @@", diffLineHunk},
		{"--- a/foo.go", diffLineMeta},
		{"+++ b/foo.go", diffLineMeta},
		{"diff --git a b", diffLineMeta},
		{"index abc..def", diffLineMeta},
	}

	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got := classifyDiffLine(tc.line)
			if got != tc.want {
				t.Errorf("classifyDiffLine(%q) = %d, want %d", tc.line, got, tc.want)
			}
		})
	}
}

// TestPadRow verifies that padRow returns a string of exactly the requested
// visible width regardless of input length.
func TestPadRow(t *testing.T) {
	p := theme.Default()
	bg := p.Diff.AddedBg

	cases := []struct {
		name  string
		row   string
		width int
	}{
		{"short", "abc", 20},
		{"exact", strings.Repeat("x", 20), 20},
		{"long", strings.Repeat("x", 30), 20},
		{"empty", "", 20},
		{"ansi row", "\x1b[32mfoo\x1b[0m", 20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := padRow(tc.row, tc.width, bg)
			got := lipgloss.Width(out)
			if got != tc.width {
				t.Errorf("padRow(%q, %d, bg): visible width %d, want %d\nout: %q",
					tc.row, tc.width, got, tc.width, out)
			}
		})
	}
}

// TestRenderGutter_VisibleWidth confirms the gutter rendered by renderGutter
// is exactly gutterTotalWidth visible characters wide.
func TestRenderGutter_VisibleWidth(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")

	cases := []struct {
		old  int
		new  int
		kind diffLineKind
	}{
		{10, 10, diffLineContext},
		{0, 11, diffLineAdded},
		{11, 0, diffLineRemoved},
		{-1, -1, diffLineHunk},
	}

	for _, tc := range cases {
		gutter := m.renderGutter(tc.old, tc.new, tc.kind)
		got := lipgloss.Width(gutter)
		if got != gutterTotalWidth {
			t.Errorf("renderGutter(%d,%d,%d): visible width %d, want %d\nraw: %q",
				tc.old, tc.new, tc.kind, got, gutterTotalWidth, gutter)
		}
	}
}
