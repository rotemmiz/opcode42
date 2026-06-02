package tui

import (
	"errors"
	"strings"
	"testing"
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
