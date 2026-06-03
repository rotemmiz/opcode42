package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/rotemmiz/forge/internal/auth"
)

// TestLSPStatus_Empty asserts GET /lsp returns an empty JSON array before any
// server has spawned. The route does NOT lazily spawn (opencode's lsp.status
// iterates the already-connected clients), so a fresh instance reports [].
func TestLSPStatus_Empty(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	rr, body := req(t, h, http.MethodGet, "/lsp", t.TempDir())
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rr.Code, body)
	}
	var status []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Root   string `json:"root"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("decode /lsp body %q: %v", body, err)
	}
	if len(status) != 0 {
		t.Fatalf("fresh instance should report no LSP clients, got %v", status)
	}
}
