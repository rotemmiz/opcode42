package tui

// inlinediff_test.go — Plan 17 Workstream C tests.
//
// Covers:
//   - edit tool completion renders the unified diff inline (with the # Edited <file>
//     title above, +/-/context coloring visible in the body).
//   - apply_patch renders one title+diff block per file (multi-file support).
//   - Per-file +N -N stats appear in the title row.
//   - The diff is cached (rendered once at completion, not rebuilt across frames).
//   - The 20-line cap is bypassed (a >20-line diff renders in full).
//   - ctrl+x v fold toggle still collapses/expands the diff.
//   - The full-screen diff reviewer (ctrl+x d) still works.

import (
	"encoding/json"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// stateWithMetadata builds a tool state JSON with the given status, input, and
// metadata blocks. Helper for the diff tests — keeps the test bodies short.
func stateWithMetadata(t *testing.T, status string, input, metadata map[string]any) json.RawMessage {
	t.Helper()
	if input == nil {
		input = map[string]any{}
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	return rawState(t, map[string]any{
		"status":   status,
		"input":    input,
		"metadata": metadata,
	})
}

// TestToolRow_EditRendersInlineDiff asserts that an `edit` tool completion
// renders the unified diff inline below the "# Edited <file>" title, with
// +, -, and context lines all present (the +/- sign marker column carries
// the color cue).
func TestToolRow_EditRendersInlineDiff(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	patch := "@@ -1,3 +1,3 @@\n context line\n-removed line\n+added line\n trailing context"
	state := stateWithMetadata(t, "completed",
		map[string]any{"filePath": "src/foo.go"},
		map[string]any{"diff": patch},
	)
	part := Part{ID: "p_edit", Tool: "edit", Type: "tool", State: state}

	row := m.toolRow(part)
	plain := stripANSI(row)

	for _, want := range []string{
		"Edit src/foo.go", // header
		"# Edited src/foo.go",
		"@@",
		"+added line",
		"-removed line",
		"trailing context",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("edit inline diff missing %q in:\n%s", want, plain)
		}
	}
}

// TestToolRow_ApplyPatchRendersMultiFileDiff asserts apply_patch renders one
// title+diff block per file (multi-file support). Each file's title uses the
// opencode patchTitle shape ("# Created|Patched <file>").
func TestToolRow_ApplyPatchRendersMultiFileDiff(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	patch1 := "@@ -1 +1 @@\n-a\n+b\n"
	patch2 := "@@ -1 +1 @@\n-c\n+d\n"
	files := []map[string]any{
		{
			"filePath":     "/repo/a/foo.go",
			"relativePath": "a/foo.go",
			"type":         "add",
			"patch":        patch1,
			"additions":    1,
			"deletions":    1,
		},
		{
			"filePath":     "/repo/b/bar.go",
			"relativePath": "b/bar.go",
			"type":         "update",
			"patch":        patch2,
			"additions":    1,
			"deletions":    1,
		},
	}
	state := stateWithMetadata(t, "completed", map[string]any{}, map[string]any{
		"diff":  patch1 + patch2,
		"files": files,
	})
	part := Part{ID: "p_patch", Tool: "apply_patch", Type: "tool", State: state}

	row := m.toolRow(part)
	plain := stripANSI(row)

	for _, want := range []string{
		"# Created a/foo.go",
		"# Patched b/bar.go",
		"+b",
		"+d",
		"-a",
		"-c",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("apply_patch multi-file diff missing %q in:\n%s", want, plain)
		}
	}
}

// TestToolRow_ApplyPatchPerFileStats asserts per-file +N -N stats appear in
// the title row of an apply_patch diff.
func TestToolRow_ApplyPatchPerFileStats(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	patch := "@@ -1,5 +1,5 @@\n c1\n c2\n-r1\n-r2\n+r1\n+r2\n"
	files := []map[string]any{
		{
			"filePath":     "/repo/big.go",
			"relativePath": "big.go",
			"type":         "update",
			"patch":        patch,
			"additions":    2,
			"deletions":    2,
		},
	}
	state := stateWithMetadata(t, "completed", map[string]any{}, map[string]any{
		"diff":  patch,
		"files": files,
	})
	part := Part{ID: "p_stats", Tool: "apply_patch", Type: "tool", State: state}

	row := m.toolRow(part)
	plain := stripANSI(row)
	if !strings.Contains(plain, "+2") || !strings.Contains(plain, "-2") {
		t.Errorf("apply_patch stats missing +2/-2 in:\n%s", plain)
	}
}

// TestToolRow_DiffWithDiagnosticsTail asserts that when an edit/apply_patch
// tool has BOTH a diff AND a trailing LSP-diagnostics block in its output
// text, the diff renders inline and the diagnostics surface below it (the
// redundant "Edit applied successfully." success line is stripped). This
// mirrors opencode's edit.ts:196-201 path where LSP errors are appended to
// the output text.
func TestToolRow_DiffWithDiagnosticsTail(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	patch := "@@ -1 +1 @@\n-x\n+y\n"
	output := "Edit applied successfully.\n\nLSP errors detected in x.go, please fix:\n  unused variable 'y'"
	state := rawState(t, map[string]any{
		"status":   "completed",
		"output":   output,
		"input":    map[string]any{"filePath": "diag.go"},
		"metadata": map[string]any{"diff": patch},
	})
	part := Part{ID: "p_diag", Tool: "edit", Type: "tool", State: state}

	row := m.toolRow(part)
	plain := stripANSI(row)

	// The diff body must be present.
	if !strings.Contains(plain, "+y") || !strings.Contains(plain, "-x") {
		t.Errorf("diff body missing in diagnostics-tail case:\n%s", plain)
	}
	// The success-line prefix must be stripped.
	if strings.Contains(plain, "Edit applied successfully") {
		t.Errorf("redundant 'Edit applied successfully.' line should be stripped:\n%s", plain)
	}
	// The diagnostics block must be present.
	if !strings.Contains(plain, "LSP errors detected in x.go") {
		t.Errorf("LSP diagnostics tail should surface below the diff:\n%s", plain)
	}
}

// TestInlineDiff_CachedAcrossFrames asserts the diff is rendered once and not
// rebuilt on subsequent frames. After the first render, we drop a sentinel
// value into the cache and verify the second render returns the sentinel as
// the diff body (the header is added by toolRow, the body comes from the cache
// untouched — proving no re-render).
func TestInlineDiff_CachedAcrossFrames(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	patch := "@@ -1 +1 @@\n-old\n+new\n"
	state := stateWithMetadata(t, "completed",
		map[string]any{"filePath": "x.go"},
		map[string]any{"diff": patch},
	)
	part := Part{ID: "p_cache", Tool: "edit", Type: "tool", State: state}

	first := m.toolRow(part)
	if first == "" {
		t.Fatal("first render should produce a diff")
	}

	// After the first render, the cache must be populated. Drop a sentinel
	// value into the cache so we can tell whether the second render uses it.
	if len(m.diffCache) == 0 {
		t.Fatal("diff cache should be populated after first render")
	}
	for k := range m.diffCache {
		m.diffCache[k] = "SENTINEL_FROM_CACHE"
	}

	second := m.toolRow(part)
	if !strings.Contains(second, "SENTINEL_FROM_CACHE") {
		t.Fatalf("second render should return the cached sentinel as the diff body, got:\n%s", second)
	}
}

// TestInlineDiff_NoRebuildWhenPartUnchanged is the inverse: a second render
// on a fresh model (without a pre-populated cache) returns the SAME rendered
// string, proving the cache is keyed by partID and produces stable output.
func TestInlineDiff_NoRebuildWhenPartUnchanged(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	patch := "@@ -1,2 +1,2 @@\n ctx\n-x\n+y\n"
	state := stateWithMetadata(t, "completed",
		map[string]any{"filePath": "stable.go"},
		map[string]any{"diff": patch},
	)
	part := Part{ID: "p_stable", Tool: "edit", Type: "tool", State: state}

	first := m.toolRow(part)
	second := m.toolRow(part)
	if first != second {
		t.Fatalf("cached diff rebuild produced different output:\nfirst:  %q\nsecond: %q", first, second)
	}
}

// TestInlineDiff_Bypasses20LineCap asserts the inline diff renders full hunks
// beyond the generic renderOutputPanel 20-line cap. A 25-line diff should
// appear in full (no "… N more lines" truncation hint from the panel).
func TestInlineDiff_Bypasses20LineCap(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	var patchLines []string
	patchLines = append(patchLines, "@@ -1,25 +1,25 @@")
	for i := 0; i < 25; i++ {
		patchLines = append(patchLines, " context "+itoaSmall(i))
	}
	patch := strings.Join(patchLines, "\n")
	state := stateWithMetadata(t, "completed",
		map[string]any{"filePath": "big.go"},
		map[string]any{"diff": patch},
	)
	part := Part{ID: "p_big", Tool: "edit", Type: "tool", State: state}

	row := m.toolRow(part)
	plain := stripANSI(row)
	if strings.Contains(plain, "more line") {
		t.Errorf("inline diff should NOT truncate, found truncation hint:\n%s", plain)
	}
	if !strings.Contains(plain, "context 24") {
		t.Errorf("inline diff should render the full 25-line patch (last line missing):\n%s", plain)
	}
}

// TestInlineDiff_FoldStillWorks asserts the ctrl+x v fold toggle still works
// for the inline-diff tool row. Collapsed → header only (no diff); expanded →
// header + diff.
func TestInlineDiff_FoldStillWorks(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	patch := "@@ -1 +1 @@\n-x\n+y\n"
	state := stateWithMetadata(t, "completed",
		map[string]any{"filePath": "fold.go"},
		map[string]any{"diff": patch},
	)
	part := Part{ID: "p_fold", Tool: "edit", Type: "tool", State: state}

	expanded := m.toolRow(part)
	if !strings.Contains(stripANSI(expanded), "+y") {
		t.Fatalf("expanded diff should contain the added line:\n%s", expanded)
	}

	m.view = m.view.toggleToolCollapse("p_fold")
	collapsed := m.toolRow(part)
	plain := stripANSI(collapsed)
	if strings.Contains(plain, "+y") {
		t.Errorf("collapsed diff should NOT contain the diff body:\n%s", collapsed)
	}
	if !strings.Contains(plain, "Edit fold.go") {
		t.Errorf("collapsed header should still show the tool header:\n%s", collapsed)
	}
	if !strings.Contains(plain, "▸") {
		t.Errorf("collapsed row should show the ▸ affordance:\n%s", collapsed)
	}
}

// TestInlineDiff_NotRenderedWhileRunning asserts the inline diff only renders
// at completion — while the tool is running, no diff body is shown (opencode's
// entryLayout flips from inline→block at completion).
func TestInlineDiff_NotRenderedWhileRunning(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	patch := "@@ -1 +1 @@\n-x\n+y\n"
	state := stateWithMetadata(t, "running",
		map[string]any{"filePath": "run.go"},
		map[string]any{"diff": patch},
	)
	part := Part{ID: "p_run", Tool: "edit", Type: "tool", State: state}

	row := m.toolRow(part)
	plain := stripANSI(row)
	if strings.Contains(plain, "# Edited") {
		t.Errorf("running edit should not render the diff snapshot title:\n%s", plain)
	}
	if strings.Contains(plain, "+y") {
		t.Errorf("running edit should not render the diff body:\n%s", plain)
	}
}

// TestInlineDiff_NoMetadataNoDiff asserts the inline diff path gracefully
// returns "" (no diff rendered) when the metadata is absent or empty —
// leaving the generic output panel as the fallback. In this fallback case the
// raw output (including "Edit applied successfully.") is shown by the generic
// output panel; the success-line trimming only applies when a diff was
// actually rendered.
func TestInlineDiff_NoMetadataNoDiff(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 100

	// Empty metadata — should fall back to the generic output panel.
	state := rawState(t, map[string]any{
		"status": "completed",
		"output": "Edit applied successfully.",
		"input":  map[string]any{"filePath": "no_meta.go"},
	})
	part := Part{ID: "p_nometa", Tool: "edit", Type: "tool", State: state}

	row := m.toolRow(part)
	plain := stripANSI(row)
	// No "# Edited" title — metadata was empty.
	if strings.Contains(plain, "# Edited") {
		t.Errorf("edit with no diff metadata should not render a diff title:\n%s", plain)
	}
	// The generic output panel fallback shows the raw output (the success-line
	// trimming is only applied when a diff was rendered).
	if !strings.Contains(plain, "Edit applied successfully") {
		t.Errorf("edit with no diff metadata should fall back to the output panel:\n%s", plain)
	}
}

// TestInlineDiff_FullScreenReviewerStillWorks asserts the full-screen diff
// reviewer (ctrl+x d) path still works after the renderInlineDiff extraction —
// the Model.renderGutter / Model.renderDiffCodeLine methods still produce
// output, and the reviewer renders a file + patch.
func TestInlineDiff_FullScreenReviewerStillWorks(t *testing.T) {
	m := withDiff()
	out := m.diffView()
	if !strings.Contains(out, "Diff") || !strings.Contains(out, "3 files") {
		t.Fatalf("full-screen reviewer broken after extraction: %q", firstLine(out))
	}
	// Select a file with a real patch (README.md has an empty patch in withDiff).
	// internal/tui/model.go (idx 2 after sort) has "@@ -1 +1 @@\n-old\n+new\n".
	m = m.diffMove(+2)
	pane := m.diffPatchPane(m.width, 20)
	if !strings.Contains(stripANSI(pane), "@@") {
		t.Fatalf("diffPatchPane should still render the @@ hunk header:\n%s", pane)
	}
}

// TestRenderGutter_Delegation sanity-checks that Model.renderGutter still
// produces the same visible width as the pure renderGutterP helper after the
// extraction (no behavior regression).
func TestRenderGutter_Delegation(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")
	for _, tc := range []struct {
		old, new int
		kind     diffLineKind
	}{
		{10, 10, diffLineContext},
		{0, 11, diffLineAdded},
		{11, 0, diffLineRemoved},
		{-1, -1, diffLineHunk},
	} {
		modelGutter := m.renderGutter(tc.old, tc.new, tc.kind)
		pureGutter := renderGutterP(m.styles.P, tc.old, tc.new, tc.kind)
		if lipgloss.Width(modelGutter) != lipgloss.Width(pureGutter) {
			t.Errorf("renderGutter width mismatch for %+v: model=%d pure=%d",
				tc, lipgloss.Width(modelGutter), lipgloss.Width(pureGutter))
		}
		if stripANSI(modelGutter) != stripANSI(pureGutter) {
			t.Errorf("renderGutter text mismatch for %+v:\nmodel=%q\npure =%q",
				tc, stripANSI(modelGutter), stripANSI(pureGutter))
		}
	}
}

// TestRenderDiffCodeLine_Delegation sanity-checks that Model.renderDiffCodeLine
// still produces the same visible output as the pure renderDiffCodeLineP
// helper after the extraction.
func TestRenderDiffCodeLine_Delegation(t *testing.T) {
	m := withDiffModel(t, "opcode42-dark")
	const codeWidth = 40
	for _, tc := range []struct {
		line string
		kind diffLineKind
	}{
		{"+foo := bar", diffLineAdded},
		{"-foo := bar", diffLineRemoved},
		{" foo := bar", diffLineContext},
		{"foo := bar", diffLineContext},
		{"--- a/x.go", diffLineMeta},
	} {
		modelOut := m.renderDiffCodeLine(tc.line, tc.kind, codeWidth, "x.go")
		pureOut := renderDiffCodeLineP(m.styles.P, tc.line, tc.kind, codeWidth, "x.go")
		if stripANSI(modelOut) != stripANSI(pureOut) {
			t.Errorf("renderDiffCodeLine mismatch for %q:\nmodel=%q\npure =%q",
				tc.line, stripANSI(modelOut), stripANSI(pureOut))
		}
	}
}

// TestPatchTitle_CoversAllTypes asserts patchTitle covers add/delete/move/update.
func TestPatchTitle_CoversAllTypes(t *testing.T) {
	cases := []struct {
		f    patchFileMeta
		want string
	}{
		{patchFileMeta{RelativePath: "a.go", Type: "add"}, "# Created a.go"},
		{patchFileMeta{RelativePath: "b.go", Type: "delete"}, "# Deleted b.go"},
		{patchFileMeta{FilePath: "/abs/from.go", RelativePath: "to.go", MovePath: "to.go", Type: "move"}, "# Moved /abs/from.go -> to.go"},
		{patchFileMeta{RelativePath: "c.go", Type: "update"}, "# Patched c.go"},
		{patchFileMeta{RelativePath: "", FilePath: "/abs/d.go", Type: "update"}, "# Patched /abs/d.go"},
	}
	for _, tc := range cases {
		if got := patchTitle(tc.f); got != tc.want {
			t.Errorf("patchTitle(%+v) = %q, want %q", tc.f, got, tc.want)
		}
	}
}

// TestTrimDiffSuccessOutput covers the success-line trimming for edit and
// apply_patch output text (so the diagnostics-only tail still surfaces
// below the diff, but the redundant "Edit applied successfully." /
// "Success. Updated the following files:…" prefix is gone — the per-file
// diff titles already convey the change).
func TestTrimDiffSuccessOutput(t *testing.T) {
	cases := []struct {
		tool string
		out  string
		want string
	}{
		{"edit", "", ""},
		{"edit", "Edit applied successfully.", ""},
		{"edit", "Edit applied successfully.\n\nLSP errors detected in x.go, please fix:\n…", "LSP errors detected in x.go, please fix:\n…"},
		{"edit", "anything else", "anything else"},
		{"apply_patch", "Success. Updated the following files:\nM a.go\nM b.go", ""},
		{"apply_patch", "Success. Updated the following files:\nM a.go\n\nLSP errors detected in y.go, please fix:\n…", "LSP errors detected in y.go, please fix:\n…"},
		{"apply_patch", "", ""},
		{"bash", "anything", "anything"}, // non-diff tools pass through
	}
	for _, tc := range cases {
		if got := trimDiffSuccessOutput(tc.out, tc.tool); got != tc.want {
			t.Errorf("trimDiffSuccessOutput(%q, %q) = %q, want %q", tc.out, tc.tool, got, tc.want)
		}
	}
}

// TestDiffStatsSuffix covers the +N -N formatter.
func TestDiffStatsSuffix(t *testing.T) {
	p := theme.Default()
	cases := []struct {
		adds, dels   int
		wantNonEmpty bool
		wantSubstrs  []string
	}{
		{0, 0, false, nil},
		{3, 0, true, []string{"+3"}},
		{0, 2, true, []string{"-2"}},
		{3, 2, true, []string{"+3", "-2"}},
	}
	for _, tc := range cases {
		got := diffStatsSuffix(tc.adds, tc.dels, p)
		if tc.wantNonEmpty && got == "" {
			t.Errorf("diffStatsSuffix(%d,%d) = empty, want non-empty", tc.adds, tc.dels)
		}
		if !tc.wantNonEmpty && got != "" {
			t.Errorf("diffStatsSuffix(%d,%d) = %q, want empty", tc.adds, tc.dels, got)
		}
		plain := stripANSI(got)
		for _, want := range tc.wantSubstrs {
			if !strings.Contains(plain, want) {
				t.Errorf("diffStatsSuffix(%d,%d) = %q, missing %q", tc.adds, tc.dels, plain, want)
			}
		}
	}
}

// TestNoPatchHint covers the "-N lines" hint for files with no textual patch.
func TestNoPatchHint(t *testing.T) {
	p := theme.Default()
	// No deletions → "no textual patch" placeholder.
	got := noPatchHint(patchFileMeta{}, p, 80)
	if plain := stripANSI(got); !strings.Contains(plain, "no textual patch") {
		t.Errorf("noPatchHint with no deletions = %q, want 'no textual patch'", plain)
	}
	// With deletions → "-N line(s)".
	got = noPatchHint(patchFileMeta{Deletions: 3}, p, 80)
	if plain := stripANSI(got); !strings.Contains(plain, "-3 line") {
		t.Errorf("noPatchHint with 3 deletions = %q, want '-3 line'", plain)
	}
}

// itoaSmall is a tiny local int→string helper to avoid colliding with
// question.go's itoa. Used only by TestInlineDiff_Bypasses20LineCap.
func itoaSmall(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
