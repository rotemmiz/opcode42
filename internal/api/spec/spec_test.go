package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEmbeddedMatchesCanonicalReference guards against the embedded copy
// drifting from the canonical vendored contract. Both are written by
// scripts/sync-openapi.sh from the same source, so they must be identical.
func TestEmbeddedMatchesCanonicalReference(t *testing.T) {
	canonical, err := os.ReadFile(filepath.Join("..", "..", "..", "conformance", "openapi-reference.json"))
	if err != nil {
		t.Fatalf("read canonical reference: %v", err)
	}
	if string(canonical) != string(Reference()) {
		t.Fatal("embedded openapi.json differs from conformance/openapi-reference.json; re-run scripts/sync-openapi.sh")
	}
}

func TestOperationsCoverAllPaths(t *testing.T) {
	ops, err := Operations()
	if err != nil {
		t.Fatalf("Operations: %v", err)
	}
	// The frozen contract has 113 paths / 131 operations.
	if len(ops) != 131 {
		t.Errorf("operation count: want 131, got %d", len(ops))
	}
	paths := map[string]bool{}
	for _, op := range ops {
		paths[op.Path] = true
		if op.Method == "" || op.Path == "" {
			t.Errorf("malformed operation: %+v", op)
		}
	}
	if len(paths) != 113 {
		t.Errorf("distinct paths: want 113, got %d", len(paths))
	}
}
