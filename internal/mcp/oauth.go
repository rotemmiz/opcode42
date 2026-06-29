package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// Default loopback callback for MCP OAuth, matching opencode's McpOAuthProvider
// (oauth-provider.ts:14-15). The redirect URI must be identical across the
// authorize URL, the DCR request, and the token exchange, so it is resolved once
// per server (explicit redirectUri > callbackPort shorthand > default).
const (
	oauthCallbackPort = 19876
	oauthCallbackPath = "/mcp/oauth/callback"
	oauthClientName   = "Opcode42"
	oauthClientURI    = "https://github.com/rotemmiz/opcode42"
)

// redirectURI resolves the effective OAuth redirect URI for a server (mirrors
// McpOAuthProvider.redirectUrl, oauth-provider.ts:38-44).
func redirectURI(s Server) string {
	if c := s.OAuth.Config; c != nil {
		if c.RedirectURI != "" {
			return c.RedirectURI
		}
		if c.CallbackPort > 0 {
			return fmt.Sprintf("http://127.0.0.1:%d%s", c.CallbackPort, oauthCallbackPath)
		}
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", oauthCallbackPort, oauthCallbackPath)
}

// oauthConfig builds the mcp-go OAuthConfig for a remote server, wiring the
// persistent token store and the resolved redirect URI. PKCE is always enabled
// (public-client default; opencode sets token_endpoint_auth_method "none" unless
// a client secret is configured).
func oauthConfig(name string, s Server) transport.OAuthConfig {
	cfg := transport.OAuthConfig{
		RedirectURI: redirectURI(s),
		TokenStore:  &persistentTokenStore{name: name, serverURL: s.URL},
		PKCEEnabled: true,
		ClientURI:   oauthClientURI,
	}
	if c := s.OAuth.Config; c != nil {
		cfg.ClientID = c.ClientID
		cfg.ClientSecret = c.ClientSecret
		if c.Scope != "" {
			cfg.Scopes = strings.Fields(c.Scope)
		}
	}
	// A stored, dynamically-registered client id takes precedence over none so a
	// reconnect after DCR doesn't re-register (oauth-provider.ts:67-80).
	if cfg.ClientID == "" {
		if e, ok := getAuthEntry(name); ok && e.ClientInfo != nil && e.ClientInfo.ClientID != "" {
			cfg.ClientID = e.ClientInfo.ClientID
			cfg.ClientSecret = e.ClientInfo.ClientSecret
		}
	}
	return cfg
}

// authStatusFromConnectError maps a failed remote connect to an MCP status.
// A non-auth error stays "failed"; a 401 becomes "needs_auth" unless the server
// lacks a usable client id AND does not support dynamic client registration, in
// which case it is "needs_client_registration" (mirroring opencode connectRemote,
// mcp/index.ts:360-414). ok reports whether err was an auth error (so the caller
// can stop trying further transports).
func authStatusFromConnectError(ctx context.Context, name string, s Server, err error) (st Status, ok bool) {
	if !isAuthRequired(err) {
		return Status{}, false
	}
	// A configured or previously-registered client id means only the user's
	// authorization is missing — no need to probe DCR.
	if s.OAuth.Config != nil && s.OAuth.Config.ClientID != "" {
		return Status{Status: "needs_auth"}, true
	}
	if e, hasEntry := getAuthEntry(name); hasEntry && e.ClientInfo != nil && e.ClientInfo.ClientID != "" {
		return Status{Status: "needs_auth"}, true
	}
	// No client id: prefer the OAuthHandler the error carries (already wired with
	// discovered metadata); otherwise build one. Probe dynamic client registration
	// so the status reflects whether the server supports it. A "does not support
	// dynamic client registration" failure is the needs_client_registration case.
	h := mcpgo.GetOAuthHandler(err)
	if h == nil {
		built, herr := obtainOAuthHandler(ctx, name, s)
		if herr != nil {
			// Can't reach the auth server to discover metadata; report needs_auth so
			// the user can retry the explicit auth flow.
			return Status{Status: "needs_auth"}, true
		}
		h = built
	}
	if regErr := h.RegisterClient(ctx, oauthClientName); regErr != nil {
		if isRegistrationUnsupported(regErr) {
			return Status{
				Status: "needs_client_registration",
				Error:  "Server does not support dynamic client registration. Please provide clientId in config.",
			}, true
		}
		// DCR endpoint exists but the attempt failed for another reason; the user
		// still needs to authenticate (the real attempt happens in StartAuth).
		return Status{Status: "needs_auth"}, true
	}
	// DCR succeeded: persist the registered client and report needs_auth.
	_ = persistClientInfo(name, s.URL, h.GetClientID(), h.GetClientSecret())
	return Status{Status: "needs_auth"}, true
}

// isRegistrationUnsupported reports whether a DCR error means the server has no
// registration endpoint (opencode keys needs_client_registration off the
// "registration"/"client_id" substrings, mcp/index.ts:371).
func isRegistrationUnsupported(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not support dynamic client registration") ||
		strings.Contains(msg, "registration") ||
		strings.Contains(msg, "client_id")
}

// persistClientInfo stores DCR-issued client credentials (McpOAuthProvider.
// saveClientInformation, oauth-provider.ts:86-103) so a later reconnect reuses
// them instead of re-registering.
func persistClientInfo(name, serverURL, clientID, clientSecret string) error {
	return mutateAuthEntry(name, func(e *authEntry) {
		e.ClientInfo = &authClientInfo{ClientID: clientID, ClientSecret: clientSecret}
		e.ServerURL = serverURL
	})
}

// authFlow holds the OAuthHandler and PKCE verifier for an in-progress
// authorization between StartAuth and FinishAuth (the Go analogue of opencode's
// pendingOAuthTransports + the codeVerifier/oauthState persisted in mcp-auth.json).
type authFlow struct {
	handler  *transport.OAuthHandler
	verifier string
	state    string
}

// startAuthFlow performs DCR (if needed), generates PKCE + state, and returns the
// authorization URL plus the flow state to stash for FinishAuth. It mirrors
// MCP.startAuth (mcp/index.ts:781-840): build an OAuth client, attempt a connect
// to obtain the handler, then derive the authorization URL. The caller
// (Manager.StartAuth) has already validated the server is remote + OAuth-enabled.
func startAuthFlow(ctx context.Context, name string, s Server) (authURL string, fl *authFlow, err error) {
	if s.Type != "remote" || s.oauthDisabled() {
		return "", nil, ErrOAuthDisabled
	}

	h, err := obtainOAuthHandler(ctx, name, s)
	if err != nil {
		return "", nil, err
	}

	// Ensure a client id exists (DCR for public clients without one).
	if h.GetClientID() == "" {
		if regErr := h.RegisterClient(ctx, oauthClientName); regErr != nil {
			return "", nil, fmt.Errorf("dynamic client registration failed: %w", regErr)
		}
		_ = persistClientInfo(name, s.URL, h.GetClientID(), h.GetClientSecret())
	}

	verifier, err := mcpgo.GenerateCodeVerifier()
	if err != nil {
		return "", nil, err
	}
	challenge := mcpgo.GenerateCodeChallenge(verifier)
	state, err := mcpgo.GenerateState()
	if err != nil {
		return "", nil, err
	}

	authURL, err = h.GetAuthorizationURL(ctx, state, challenge)
	if err != nil {
		return "", nil, err
	}

	// Persist verifier + state so the flow survives across the separate
	// authStart/authCallback HTTP requests (McpAuth.updateCodeVerifier/
	// updateOAuthState, mcp/auth.ts:112-113).
	_ = mutateAuthEntry(name, func(e *authEntry) {
		e.CodeVerifier = verifier
		e.OAuthState = state
		e.ServerURL = s.URL
	})

	return authURL, &authFlow{handler: h, verifier: verifier, state: state}, nil
}

// obtainOAuthHandler returns an OAuthHandler wired with the server's metadata. It
// connects with an OAuth-enabled client; the expected outcome is an
// OAuthAuthorizationRequiredError carrying a fully-initialized handler (the
// handler memoizes the discovered authorization-server metadata). If the connect
// unexpectedly succeeds, the server did not actually require auth.
func obtainOAuthHandler(ctx context.Context, name string, s Server) (*transport.OAuthHandler, error) {
	c, err := newOAuthClient(s.URL, oauthConfig(name, s), s.Headers)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()

	if err := c.Start(context.WithoutCancel(ctx)); err != nil {
		if h := mcpgo.GetOAuthHandler(err); h != nil {
			return h, nil
		}
		return nil, fmt.Errorf("failed to start MCP OAuth flow: %w", err)
	}
	if err := initialize(ctx, c); err != nil {
		if h := mcpgo.GetOAuthHandler(err); h != nil {
			return h, nil
		}
		return nil, fmt.Errorf("failed to start MCP OAuth flow: %w", err)
	}
	return nil, errors.New("MCP server did not require OAuth")
}

// newOAuthClient builds a StreamableHTTP MCP client wired for OAuth. It is used
// to obtain the OAuthHandler (and to drive the authorize URL/token exchange);
// the actual connected client at dial time is built by remoteDial.
func newOAuthClient(url string, cfg transport.OAuthConfig, headers map[string]string) (conn, error) {
	opts := []transport.StreamableHTTPCOption{transport.WithHTTPOAuth(cfg)}
	if len(headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(headers))
	}
	return mcpgo.NewStreamableHttpClient(url, opts...)
}

// initialize runs the MCP Start+Initialize handshake against a freshly-built
// client (shared by obtainOAuthHandler and the OAuth dial probe).
func initialize(ctx context.Context, c conn) error {
	return probeInitialize(ctx, c)
}

// finishAuthFlow exchanges the authorization code for tokens using the flow's
// handler + persisted verifier, then clears the transient flow state. It mirrors
// transport.finishAuth → ProcessAuthorizationResponse (mcp/index.ts:898-921).
func finishAuthFlow(ctx context.Context, name string, fl *authFlow, code string) error {
	if err := fl.handler.ProcessAuthorizationResponse(ctx, code, fl.state, fl.verifier); err != nil {
		return err
	}
	_ = mutateAuthEntry(name, func(e *authEntry) {
		e.CodeVerifier = ""
		e.OAuthState = ""
	})
	return nil
}
