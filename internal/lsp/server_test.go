package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// touch creates an empty file (and parent dirs) under root.
func touch(t *testing.T, root string, rel string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNearestRoot_FindsMarker(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	src := touch(t, dir, "pkg/sub/main.go")

	got := nearestRoot(src, dir, []string{"go.mod"}, nil)
	if got != dir {
		t.Fatalf("want %s, got %s", dir, got)
	}
}

func TestNearestRoot_NestedMarkerWins(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")
	nested := filepath.Join(dir, "service")
	touch(t, dir, "service/go.mod")
	src := touch(t, dir, "service/internal/main.go")

	got := nearestRoot(src, dir, []string{"go.mod"}, nil)
	if got != nested {
		t.Fatalf("nearest marker should win: want %s, got %s", nested, got)
	}
}

func TestNearestRoot_FallbackToInstanceDir(t *testing.T) {
	dir := t.TempDir()
	src := touch(t, dir, "a/b/main.go")
	got := nearestRoot(src, dir, []string{"go.mod"}, nil)
	if got != dir {
		t.Fatalf("no marker should fall back to instanceDir: want %s, got %s", dir, got)
	}
}

func TestNearestRoot_ExcludeBailsOut(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package-lock.json")
	touch(t, dir, "deno.json")
	src := touch(t, dir, "src/index.ts")

	// typescript excludes deno projects: with deno.json present, root is "".
	got := nearestRoot(src, dir,
		[]string{"package-lock.json"},
		[]string{"deno.json", "deno.jsonc"})
	if got != "" {
		t.Fatalf("deno exclusion should yield empty root, got %s", got)
	}
}

func TestNearestRoot_ExcludeNotPresent(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package-lock.json")
	src := touch(t, dir, "src/index.ts")

	got := nearestRoot(src, dir,
		[]string{"package-lock.json"},
		[]string{"deno.json", "deno.jsonc"})
	if got != dir {
		t.Fatalf("no deno marker should resolve to instanceDir, got %s", got)
	}
}

func TestGoplsRoot_GoWorkWins(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.work")
	module := filepath.Join(dir, "mod")
	touch(t, dir, "mod/go.mod")
	src := touch(t, dir, "mod/main.go")

	// go.work at the top wins over the nested go.mod.
	got := goplsRoot(src, dir)
	if got != dir {
		t.Fatalf("go.work should win: want %s, got %s", dir, got)
	}
	_ = module
}

func TestGoplsRoot_GoModWhenNoWork(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "mod")
	touch(t, dir, "mod/go.mod")
	src := touch(t, dir, "mod/main.go")

	got := goplsRoot(src, dir)
	if got != module {
		t.Fatalf("go.mod should be root: want %s, got %s", module, got)
	}
}

func TestServerDef_MatchesExtension(t *testing.T) {
	if !Servers["gopls"].matchesExtension(".go") {
		t.Fatalf("gopls should match .go")
	}
	if Servers["gopls"].matchesExtension(".py") {
		t.Fatalf("gopls should not match .py")
	}
	if !Servers["pyright"].matchesExtension(".pyi") {
		t.Fatalf("pyright should match .pyi")
	}
	if !Servers["typescript"].matchesExtension(".tsx") {
		t.Fatalf("typescript should match .tsx")
	}
}

func TestBuiltinIDs(t *testing.T) {
	ids := BuiltinIDs()
	for _, want := range []string{"gopls", "typescript", "pyright"} {
		if !ids[want] {
			t.Fatalf("missing built-in id %q", want)
		}
	}
	if len(ids) != 3 {
		t.Fatalf("foundation subset should be exactly 3 servers, got %d", len(ids))
	}
}
