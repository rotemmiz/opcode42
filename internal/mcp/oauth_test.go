package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// isolateAuthStore points the MCP token store at a temp file so the suite never
// touches a real ~/.local/share/opencode/mcp-auth.json.
func isolateAuthStore(t *testing.T) {
	t.Helper()
	prev := mcpAuthPath
	mcpAuthPath = filepath.Join(t.TempDir(), "mcp-auth.json")
	t.Cleanup(func() { mcpAuthPath = prev })
}

// oauthMock is a mock OAuth+MCP server: its MCP endpoint 401s until a Bearer
// token is presented, and it serves RFC 8414 auth-server metadata, RFC 7591
// dynamic client registration, and the token endpoint. supportsDCR toggles the
// registration_endpoint so tests can exercise needs_client_registration.
type oauthMock struct {
	srv         *httptest.Server
	supportsDCR bool
	registered  atomic.Bool // a client registered via DCR
	tokenIssued atomic.Bool // the token endpoint issued a token
	issuedCode  string      // the code the /token endpoint accepts
}

func newOAuthMock(t *testing.T, supportsDCR bool) *oauthMock {
	t.Helper()
	m := &oauthMock{supportsDCR: supportsDCR, issuedCode: "the-auth-code"}
	mux := http.NewServeMux()
	m.srv = httptest.NewServer(mux)
	base := m.srv.URL

	// MCP endpoint: 401 with a WWW-Authenticate header unless a Bearer token is
	// present. With a token it does a minimal initialize handshake.
	mcpHandler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		writeInitializeResult(w, r)
	}
	mux.HandleFunc("/", mcpHandler)

	// RFC 9728 protected-resource metadata: 404 so discovery falls back to
	// auth-server discovery (the path mcp-go probes next).
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})

	// RFC 8414 authorization-server metadata.
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		meta := map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"response_types_supported":              []string{"code"},
			"code_challenge_methods_supported":      []string{"S256"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported": []string{"none"},
		}
		if m.supportsDCR {
			meta["registration_endpoint"] = base + "/register"
		}
		writeJSONResp(w, meta)
	})

	// RFC 7591 dynamic client registration.
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		m.registered.Store(true)
		w.WriteHeader(http.StatusCreated)
		writeJSONResp(w, map[string]any{"client_id": "dcr-client-id"})
	})

	// Token endpoint: exchange the code for an access token.
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != m.issuedCode {
			w.WriteHeader(http.StatusBadRequest)
			writeJSONResp(w, map[string]any{"error": "invalid_grant"})
			return
		}
		m.tokenIssued.Store(true)
		writeJSONResp(w, map[string]any{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})

	t.Cleanup(m.srv.Close)
	return m
}

// writeInitializeResult answers a JSON-RPC initialize (and tools/list) so an
// authenticated client connects. It is a minimal MCP server for the auth path.
func writeInitializeResult(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     any    `json:"id"`
		Method string `json:"method"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	w.Header().Set("Content-Type", "application/json")
	switch req.Method {
	case "initialize":
		writeJSONResp(w, map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "mock", "version": "1.0.0"},
			},
		})
	case "tools/list":
		writeJSONResp(w, map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{"tools": []any{}},
		})
	default:
		// notifications/initialized and others: 202 Accepted, no body.
		w.WriteHeader(http.StatusAccepted)
	}
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// TestStatus_NeedsAuth_WithDCR proves a remote OAuth server that requires auth
// and supports DCR surfaces needs_auth (the client gets dynamically registered).
func TestStatus_NeedsAuth_WithDCR(t *testing.T) {
	isolateAuthStore(t)
	mock := newOAuthMock(t, true)

	m := NewManager(map[string]Server{"r": {Name: "r", Type: "remote", URL: mock.srv.URL, Timeout: 5000}})
	st := m.Status(context.Background())["r"]
	if st.Status != "needs_auth" {
		t.Fatalf("status = %+v, want needs_auth", st)
	}
	if !mock.registered.Load() {
		t.Fatal("expected dynamic client registration to be attempted")
	}
	// The DCR-issued client id should be persisted for the next reconnect.
	if e, ok := getAuthEntry("r"); !ok || e.ClientInfo == nil || e.ClientInfo.ClientID != "dcr-client-id" {
		t.Fatalf("DCR client info not persisted: %+v", e)
	}
	m.Close()
}

// TestStatus_NeedsClientRegistration proves a server that requires auth but does
// NOT support DCR (and has no configured clientId) surfaces
// needs_client_registration with the guidance error.
func TestStatus_NeedsClientRegistration(t *testing.T) {
	isolateAuthStore(t)
	mock := newOAuthMock(t, false)

	m := NewManager(map[string]Server{"r": {Name: "r", Type: "remote", URL: mock.srv.URL, Timeout: 5000}})
	st := m.Status(context.Background())["r"]
	if st.Status != "needs_client_registration" {
		t.Fatalf("status = %+v, want needs_client_registration", st)
	}
	if st.Error == "" {
		t.Fatal("needs_client_registration must carry a guidance error")
	}
	m.Close()
}

// TestStatus_OAuthDisabled_StaysFailed proves `oauth: false` opts out: a 401
// surfaces as plain "failed", not needs_auth.
func TestStatus_OAuthDisabled_StaysFailed(t *testing.T) {
	isolateAuthStore(t)
	mock := newOAuthMock(t, true)

	m := NewManager(map[string]Server{"r": {
		Name: "r", Type: "remote", URL: mock.srv.URL, Timeout: 5000,
		OAuth: OAuthField{Disabled: true},
	}})
	st := m.Status(context.Background())["r"]
	if st.Status != "failed" {
		t.Fatalf("status = %+v, want failed (oauth opted out)", st)
	}
	m.Close()
}

// TestAuthFlow_StartFinish proves the end-to-end OAuth flow (plan §M3-2 / test #7):
// StartAuth returns an authorization URL + state and runs DCR; FinishAuth exchanges
// the code for a token, persists it, and reconnects so the server becomes connected.
func TestAuthFlow_StartFinish(t *testing.T) {
	isolateAuthStore(t)
	mock := newOAuthMock(t, true)
	ctx := context.Background()

	m := NewManager(map[string]Server{"r": {Name: "r", Type: "remote", URL: mock.srv.URL, Timeout: 5000}})
	// Initial status: needs_auth.
	if st := m.Status(ctx)["r"]; st.Status != "needs_auth" {
		t.Fatalf("pre-auth status = %+v, want needs_auth", st)
	}

	authURL, state, err := m.StartAuth(ctx, "r")
	if err != nil {
		t.Fatalf("StartAuth: %v", err)
	}
	if state == "" || !strings.Contains(authURL, "/authorize") {
		t.Fatalf("StartAuth url=%q state=%q", authURL, state)
	}
	if !strings.Contains(authURL, "code_challenge=") || !strings.Contains(authURL, "state="+state) {
		t.Fatalf("authorization URL missing PKCE/state: %s", authURL)
	}
	// The verifier + state must be persisted across the (separate) requests.
	if e, ok := getAuthEntry("r"); !ok || e.CodeVerifier == "" || e.OAuthState != state {
		t.Fatalf("flow state not persisted: %+v", e)
	}

	st, err := m.FinishAuth(ctx, "r", mock.issuedCode)
	if err != nil {
		t.Fatalf("FinishAuth: %v", err)
	}
	if !mock.tokenIssued.Load() {
		t.Fatal("token endpoint was not hit")
	}
	if st.Status != "connected" {
		t.Fatalf("post-auth status = %+v, want connected", st)
	}
	// Tokens persisted; transient flow state cleared.
	if !hasStoredTokens("r") {
		t.Fatal("access token not persisted after FinishAuth")
	}
	if e, _ := getAuthEntry("r"); e.CodeVerifier != "" || e.OAuthState != "" {
		t.Fatalf("transient flow state not cleared: %+v", e)
	}
	m.Close()
}

// TestStartAuth_OAuthDisabled proves StartAuth on a non-OAuth / disabled server
// returns ErrOAuthDisabled (mapped to 400 McpUnsupportedOAuthError).
func TestStartAuth_OAuthDisabled(t *testing.T) {
	isolateAuthStore(t)
	m := NewManager(map[string]Server{
		"local":    {Name: "local", Type: "local", Command: []string{"x"}},
		"disabled": {Name: "disabled", Type: "remote", URL: "https://x", OAuth: OAuthField{Disabled: true}},
	})
	if _, _, err := m.StartAuth(context.Background(), "local"); err != ErrOAuthDisabled {
		t.Fatalf("StartAuth(local) err = %v, want ErrOAuthDisabled", err)
	}
	if _, _, err := m.StartAuth(context.Background(), "disabled"); err != ErrOAuthDisabled {
		t.Fatalf("StartAuth(disabled) err = %v, want ErrOAuthDisabled", err)
	}
	if _, _, err := m.StartAuth(context.Background(), "ghost"); err != ErrServerNotFound {
		t.Fatalf("StartAuth(ghost) err = %v, want ErrServerNotFound", err)
	}
}

// TestRemoveAuth clears persisted tokens.
func TestRemoveAuth(t *testing.T) {
	isolateAuthStore(t)
	if err := mutateAuthEntry("r", func(e *authEntry) {
		e.Tokens = &authTokens{AccessToken: "tok"}
	}); err != nil {
		t.Fatal(err)
	}
	if !hasStoredTokens("r") {
		t.Fatal("precondition: token should be stored")
	}
	m := NewManager(nil)
	if err := m.RemoveAuth("r"); err != nil {
		t.Fatalf("RemoveAuth: %v", err)
	}
	if hasStoredTokens("r") {
		t.Fatal("token should be removed")
	}
}
