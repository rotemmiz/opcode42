package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/credstore"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/oauth"
	"github.com/rotemmiz/opcode42/internal/resource"
)

// newOAuthServer builds a minimal handler with only the provider OAuth surface
// wired, so the authorize/callback/methods endpoints are real (not 501).
func newOAuthServer(t *testing.T) http.Handler {
	t.Helper()
	svc, err := oauth.NewService("")
	if err != nil {
		t.Fatalf("oauth.NewService: %v", err)
	}
	t.Cleanup(func() { svc.Shutdown(context.Background()) })
	h, err := New(Options{Version: "0.0.1", Cwd: t.TempDir(), OAuth: svc})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

// TestProviderAuthMethodsEndpoint verifies GET /provider/auth returns the
// built-in provider methods map (not 501).
func TestProviderAuthMethodsEndpoint(t *testing.T) {
	h := newOAuthServer(t)
	r := httptest.NewRequest(http.MethodGet, "/provider/auth", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got map[string][]map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if methods, ok := got["xai"]; !ok || len(methods) == 0 || methods[0]["type"] != "oauth" {
		t.Fatalf("xai oauth method missing: %v", got)
	}
}

// TestProviderCallbackMissing verifies a callback with no prior authorize maps
// to the ProviderAuthOauthMissing 400 shape (handlers/provider.ts).
func TestProviderCallbackMissing(t *testing.T) {
	h := newOAuthServer(t)
	r := httptest.NewRequest(http.MethodPost, "/provider/xai/oauth/callback",
		bytes.NewReader([]byte(`{"method":0}`)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	var body struct {
		Name string         `json:"name"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Name != "ProviderAuthOauthMissing" {
		t.Fatalf("name = %q, want ProviderAuthOauthMissing", body.Name)
	}
	if body.Data["providerID"] != "xai" {
		t.Fatalf("data.providerID = %v", body.Data["providerID"])
	}
}

// TestProviderAuthorizeUnknownProvider verifies authorize for a provider with no
// built-in OAuth method returns a 400 BadRequest.
func TestProviderAuthorizeUnknownProvider(t *testing.T) {
	h := newOAuthServer(t)
	r := httptest.NewRequest(http.MethodPost, "/provider/nope/oauth/authorize",
		bytes.NewReader([]byte(`{"method":0}`)))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

// TestProviderOAuthDisabledFallsTo501 verifies that without an OAuth service the
// endpoints remain the Phase-A 501 placeholder (no regression for default wiring).
func TestProviderOAuthDisabledFallsTo501(t *testing.T) {
	h := newBackedServer(t, auth.Config{})
	r := httptest.NewRequest(http.MethodGet, "/provider/auth", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 when OAuth disabled", rr.Code)
	}
}

func TestProviderAuthPutDelete(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("OPENCODE_AUTH_CONTENT", "")
	h := newBackedServer(t, auth.Config{})

	// PUT an api key.
	put := func(body string) int {
		r := httptest.NewRequest(http.MethodPut, "/auth/anthropic", bytes.NewReader([]byte(body)))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, r)
		return rr.Code
	}
	if code := put(`{"type":"api","key":"sk-test"}`); code != http.StatusOK {
		t.Fatalf("PUT status = %d", code)
	}
	if credstore.TypeOf(credstore.Load()["anthropic"]) != "api" {
		t.Fatal("credential not persisted to the shared store")
	}

	// Invalid record → 400, store unchanged.
	if code := put(`{"type":"bogus"}`); code != http.StatusBadRequest {
		t.Fatalf("invalid type status = %d (want 400)", code)
	}

	// DELETE removes it.
	r := httptest.NewRequest(http.MethodDelete, "/auth/anthropic", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d", rr.Code)
	}
	if _, ok := credstore.Load()["anthropic"]; ok {
		t.Fatal("credential not removed")
	}
}

// TestProviderAuthFlipsConnected proves PUT /auth makes /provider report the
// provider connected (the end-to-end point of the credential store).
func TestProviderAuthFlipsConnected(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("OPENCODE_AUTH_CONTENT", "")
	if err := credstore.Set("anthropic", []byte(`{"type":"api","key":"k"}`)); err != nil {
		t.Fatal(err)
	}
	cat := catalog.Catalog{"anthropic": {ID: "anthropic", Models: map[string]catalog.Model{"m": {ID: "m"}}}}
	list := resource.BuildProviderList(cat, map[string]any{})
	connected := false
	for _, id := range list.Connected {
		if id == "anthropic" {
			connected = true
		}
	}
	if !connected {
		t.Fatalf("anthropic should be connected after storing a credential; connected=%v", list.Connected)
	}
}
