package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tctx(dir string) Context {
	return Context{SessionID: "s", MessageID: "m", CallID: "c", Directory: dir}
}

func TestBash_RunsAndCaptures(t *testing.T) {
	dir := t.TempDir()
	res, err := Bash{}.Run(context.Background(), map[string]any{"command": "echo hello"}, tctx(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "hello") {
		t.Fatalf("output = %q", res.Output)
	}
	if res.Metadata["exit"] != 0 {
		t.Fatalf("exit = %v, want 0", res.Metadata["exit"])
	}
}

func TestBash_NonZeroExitNotError(t *testing.T) {
	res, err := Bash{}.Run(context.Background(), map[string]any{"command": "exit 3"}, tctx(t.TempDir()))
	if err != nil {
		t.Fatalf("non-zero exit should not error: %v", err)
	}
	if res.Metadata["exit"] != 3 {
		t.Fatalf("exit = %v, want 3", res.Metadata["exit"])
	}
}

func TestBash_RunsInWorkingDir(t *testing.T) {
	dir := t.TempDir()
	res, _ := Bash{}.Run(context.Background(), map[string]any{"command": "pwd"}, tctx(dir))
	// macOS /var -> /private/var symlink; compare basenames to stay portable.
	if !strings.Contains(res.Output, filepath.Base(dir)) {
		t.Fatalf("pwd = %q, want to contain %q", res.Output, filepath.Base(dir))
	}
}

func TestReadWriteEdit_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	if _, err := (Write{}).Run(ctx, map[string]any{"filePath": "a/b.txt", "content": "one\ntwo\nthree"}, tctx(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a/b.txt")); err != nil {
		t.Fatalf("file not written: %v", err)
	}

	res, err := Read{}.Run(ctx, map[string]any{"filePath": "a/b.txt"}, tctx(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "1\tone") || !strings.Contains(res.Output, "3\tthree") {
		t.Fatalf("read output missing line numbers: %q", res.Output)
	}

	if _, err := (Edit{}).Run(ctx, map[string]any{"filePath": "a/b.txt", "oldString": "two", "newString": "TWO"}, tctx(dir)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a/b.txt"))
	if !strings.Contains(string(data), "TWO") {
		t.Fatalf("edit not applied: %q", data)
	}
}

func TestRead_OffsetLimit(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc\nd\ne"), 0o644)
	res, err := Read{}.Run(context.Background(), map[string]any{"filePath": "f.txt", "offset": 1, "limit": 2}, tctx(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "2\tb") || !strings.Contains(res.Output, "3\tc") || strings.Contains(res.Output, "1\ta") {
		t.Fatalf("windowed read wrong: %q", res.Output)
	}
}

func TestEdit_AmbiguousFails(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x x x"), 0o644)
	if _, err := (Edit{}).Run(context.Background(), map[string]any{"filePath": "f.txt", "oldString": "x", "newString": "y"}, tctx(dir)); err == nil {
		t.Fatal("ambiguous edit should fail without replaceAll")
	}
	if _, err := (Edit{}).Run(context.Background(), map[string]any{"filePath": "f.txt", "oldString": "x", "newString": "y", "replaceAll": true}, tctx(dir)); err != nil {
		t.Fatalf("replaceAll should succeed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "y y y" {
		t.Fatalf("replaceAll result = %q", data)
	}
}

func TestGlob_FindsAndOrders(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.go", "sub/b.go", "sub/c.txt"} {
		full := filepath.Join(dir, f)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("x"), 0o644)
	}
	res, err := Glob{}.Run(context.Background(), map[string]any{"pattern": "**/*.go"}, tctx(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "a.go") || !strings.Contains(res.Output, "b.go") || strings.Contains(res.Output, "c.txt") {
		t.Fatalf("glob output wrong: %q", res.Output)
	}
}

func TestGrep_FindsMatches(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "f.go"), []byte("package x\nfunc Hello() {}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "g.txt"), []byte("Hello there\n"), 0o644)
	res, err := Grep{}.Run(context.Background(), map[string]any{"pattern": "Hello", "include": "*.go"}, tctx(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "f.go:2:func Hello") {
		t.Fatalf("grep output wrong: %q", res.Output)
	}
	if strings.Contains(res.Output, "g.txt") {
		t.Fatalf("include filter failed: %q", res.Output)
	}
}

func TestPatch_AppliesUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\nthree"), 0o644)
	diff := "--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	if _, err := (Patch{}).Run(context.Background(), map[string]any{"patch": diff}, tctx(dir)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(data) != "one\nTWO\nthree" {
		t.Fatalf("patch result = %q", data)
	}
}

func TestPatch_ContextMismatchFails(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo"), 0o644)
	diff := "--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n one\n-WRONG\n+x\n"
	if _, err := (Patch{}).Run(context.Background(), map[string]any{"patch": diff}, tctx(dir)); err == nil {
		t.Fatal("expected context mismatch error")
	}
}
