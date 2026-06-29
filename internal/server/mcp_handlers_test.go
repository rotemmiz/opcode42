package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rotemmiz/opcode42/internal/auth"
)

// reqBody issues an HTTP request with a JSON body to h, returning the recorder
// and the response body (the directory-scoped sibling of req for mutating calls).
func reqBody(t *testing.T, h http.Handler, method, path, dir, body string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if dir != "" {
		r.Header.Set("x-opencode-directory", dir)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	out, _ := io.ReadAll(rr.Body)
	return rr, out
}

func TestMCPStatus_Empty(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	rr, body := req(t, h, http.MethodGet, "/mcp", t.TempDir())
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if string(body) != "{}\n" && string(body) != "{}" {
		t.Fatalf("empty /mcp must be {}; got %q", body)
	}
}

func TestMCPStatus_DisabledFromConfig(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	dir := t.TempDir()
	// A disabled local server exercises the config→instance→manager→endpoint
	// path without spawning a process.
	cfg := `{"mcp":{"my-tool":{"type":"local","command":["my-mcp"],"enabled":false}}}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	rr, body := req(t, h, http.MethodGet, "/mcp", dir)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; %s", rr.Code, body)
	}
	var status map[string]struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatal(err)
	}
	if status["my-tool"].Status != "disabled" {
		t.Fatalf("my-tool status = %+v (body %s)", status["my-tool"], body)
	}
}

// TestMCPMutating_NotFound proves connect/disconnect/auth-start/auth-callback/
// auth-remove all 404 with the McpServerNotFoundError shape for an unknown name.
func TestMCPMutating_NotFound(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	dir := t.TempDir()

	cases := []struct {
		method, path, body string
	}{
		{http.MethodPost, "/mcp/ghost/connect", ""},
		{http.MethodPost, "/mcp/ghost/disconnect", ""},
		{http.MethodPost, "/mcp/ghost/auth", ""},
		{http.MethodPost, "/mcp/ghost/auth/callback", `{"code":"x"}`},
		{http.MethodDelete, "/mcp/ghost/auth", ""},
	}
	for _, c := range cases {
		rr, body := reqBody(t, h, c.method, c.path, dir, c.body)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s %s code = %d, want 404; body=%s", c.method, c.path, rr.Code, body)
		}
		var e struct {
			Tag, Name, Message string
		}
		var raw map[string]any
		_ = json.Unmarshal(body, &raw)
		e.Tag, _ = raw["_tag"].(string)
		e.Name, _ = raw["name"].(string)
		e.Message, _ = raw["message"].(string)
		if e.Tag != "McpServerNotFoundError" || e.Name != "ghost" || e.Message == "" {
			t.Fatalf("%s %s 404 body = %s", c.method, c.path, body)
		}
	}
}

// TestMCPAuthStart_UnsupportedOAuth proves auth-start on a non-OAuth (local)
// server 400s with the McpUnsupportedOAuthError {error} shape.
func TestMCPAuthStart_UnsupportedOAuth(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	dir := t.TempDir()
	cfg := `{"mcp":{"loc":{"type":"local","command":["x"],"enabled":false}}}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	rr, body := reqBody(t, h, http.MethodPost, "/mcp/loc/auth", dir, "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body=%s", rr.Code, body)
	}
	var e struct {
		Error string `json:"error"`
		Tag   string `json:"_tag"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	if e.Error == "" {
		t.Fatalf("McpUnsupportedOAuthError must carry {error}; body=%s", body)
	}
	if e.Tag != "" {
		t.Fatalf("McpUnsupportedOAuthError must NOT carry _tag; body=%s", body)
	}
}

// TestMCPAdd_DisabledServer proves POST /mcp adds a server at runtime and returns
// the resulting status map (a disabled server connects to "disabled" without a
// process spawn).
func TestMCPAdd_DisabledServer(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	dir := t.TempDir()
	body := `{"name":"added","config":{"type":"local","command":["x"],"enabled":false}}`
	rr, out := reqBody(t, h, http.MethodPost, "/mcp", dir, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("add code = %d; body=%s", rr.Code, out)
	}
	var status map[string]struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatal(err)
	}
	if status["added"].Status != "disabled" {
		t.Fatalf("added status = %+v; body=%s", status["added"], out)
	}

	// The runtime-added server is now disconnect-able (404 would mean Add didn't
	// register it).
	rr2, out2 := reqBody(t, h, http.MethodPost, "/mcp/added/disconnect", dir, "")
	if rr2.Code != http.StatusOK || string(out2) != "true\n" {
		t.Fatalf("disconnect added = %d %q", rr2.Code, out2)
	}
}

// TestMCPAdd_BadPayload proves a malformed add body 400s (BadRequest).
func TestMCPAdd_BadPayload(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	rr, _ := reqBody(t, h, http.MethodPost, "/mcp", t.TempDir(), `{"name":""}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad add payload code = %d, want 400", rr.Code)
	}
}

// TestMCPDisconnect_Configured proves disconnecting a config-declared server
// succeeds and flips its status to disabled.
func TestMCPDisconnect_Configured(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	dir := t.TempDir()
	cfg := `{"mcp":{"my-tool":{"type":"local","command":["x"],"enabled":false}}}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	rr, out := reqBody(t, h, http.MethodPost, "/mcp/my-tool/disconnect", dir, "")
	if rr.Code != http.StatusOK || string(out) != "true\n" {
		t.Fatalf("disconnect configured = %d %q", rr.Code, out)
	}
}
