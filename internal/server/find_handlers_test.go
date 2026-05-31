package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rotemmiz/forge/internal/auth"
)

// findProject lays out a small tree and returns its root.
func findProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, rel := range []string{
		"README.md",
		"cmd/forged/main.go",
		"internal/server/server.go",
		"internal/server/find_handlers.go",
		"internal/engine/engine.go",
		".git/config",         // skipped dir
		"node_modules/x/y.js", // skipped dir
		".env",                // hidden file
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFindFile_RanksAndFilters(t *testing.T) {
	root := findProject(t)
	h := newBackedServer(t, auth.Config{})

	files := getFindFiles(t, h, root, "/find/file?query=server.go")
	if len(files) == 0 || files[0] != "internal/server/server.go" {
		t.Fatalf("server.go should rank first, got %v", files)
	}
	for _, f := range files {
		if filepathHasPrefix(f, ".git") || filepathHasPrefix(f, "node_modules") || f == ".env" {
			t.Fatalf("hidden/ignored path leaked: %v", files)
		}
	}
}

func TestFindFile_Subsequence(t *testing.T) {
	root := findProject(t)
	h := newBackedServer(t, auth.Config{})
	// "egeng" is a subsequence of internal/enginE/enGine.go's tail.
	files := getFindFiles(t, h, root, "/find/file?query=enginego")
	if len(files) == 0 || files[0] != "internal/engine/engine.go" {
		t.Fatalf("engine.go should match, got %v", files)
	}
}

func TestFindFile_Directories(t *testing.T) {
	root := findProject(t)
	h := newBackedServer(t, auth.Config{})
	dirs := getFindFiles(t, h, root, "/find/file?query=server&type=directory")
	found := false
	for _, d := range dirs {
		if d == "internal/server/" {
			found = true
		}
		if d[len(d)-1] != '/' {
			t.Fatalf("directory result missing trailing slash: %q", d)
		}
	}
	if !found {
		t.Fatalf("expected internal/server/ dir, got %v", dirs)
	}
}

func TestFindFile_Errors(t *testing.T) {
	root := findProject(t)
	h := newBackedServer(t, auth.Config{})

	rr, _ := req(t, h, http.MethodGet, "/find/file", root) // no query
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing query: status = %d (want 400)", rr.Code)
	}
	rr, _ = req(t, h, http.MethodGet, "/find/file?query=x&limit=0", root)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("limit=0: status = %d (want 400)", rr.Code)
	}
	rr, _ = req(t, h, http.MethodGet, "/find/file?query=x&limit=999", root)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("limit=999: status = %d (want 400)", rr.Code)
	}
}

func TestFindFile_LimitTruncates(t *testing.T) {
	root := findProject(t)
	h := newBackedServer(t, auth.Config{})
	files := getFindFiles(t, h, root, "/find/file?query=go&limit=2")
	if len(files) > 2 {
		t.Fatalf("limit not honored: %d results", len(files))
	}
}

func TestFindFile_EmptyResultIsArrayNotNull(t *testing.T) {
	root := t.TempDir() // no matching files
	h := newBackedServer(t, auth.Config{})
	rr, body := req(t, h, http.MethodGet, "/find/file?query=zzznomatch", root)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if string(body) != "[]\n" && string(body) != "[]" {
		t.Fatalf("empty result must marshal as []; got %q", body)
	}
}

func TestFindFile_UppercaseQuery(t *testing.T) {
	root := findProject(t)
	h := newBackedServer(t, auth.Config{})
	files := getFindFiles(t, h, root, "/find/file?query=SERVER.GO")
	if len(files) == 0 || files[0] != "internal/server/server.go" {
		t.Fatalf("case-insensitive match failed, got %v", files)
	}
}

func TestFindFile_AllKindIncludesDirs(t *testing.T) {
	root := findProject(t)
	h := newBackedServer(t, auth.Config{})
	// default kind is "all": a dir-only query should still surface the directory.
	files := getFindFiles(t, h, root, "/find/file?query=cmd")
	foundDir := false
	for _, f := range files {
		if f == "cmd/" {
			foundDir = true
		}
	}
	if !foundDir {
		t.Fatalf("default (all) kind should include the cmd/ directory, got %v", files)
	}
}

func TestFindFile_InvalidEnums(t *testing.T) {
	root := findProject(t)
	h := newBackedServer(t, auth.Config{})
	for _, p := range []string{"/find/file?query=x&type=bogus", "/find/file?query=x&dirs=maybe"} {
		rr, _ := req(t, h, http.MethodGet, p, root)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d (want 400)", p, rr.Code)
		}
	}
}

func getFindFiles(t *testing.T, h http.Handler, dir, path string) []string {
	t.Helper()
	rr, body := req(t, h, http.MethodGet, path, dir)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, body)
	}
	var out []string
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	return out
}

func filepathHasPrefix(p, prefix string) bool {
	return len(p) >= len(prefix) && p[:len(prefix)] == prefix
}
