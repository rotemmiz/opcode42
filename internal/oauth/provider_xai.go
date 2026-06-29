package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// xAI OAuth constants, ported from packages/opencode/src/plugin/xai.ts:11-43.
// The Grok-CLI public client_id is reused because xAI's auth server only accepts
// loopback OAuth from allowlisted clients (xai.ts:9-12).
const (
	xaiClientID     = "b1a00492-073a-47ea-816f-4c329264a828"
	xaiAuthorizeURL = "https://auth.x.ai/oauth2/authorize"
	xaiTokenURL     = "https://auth.x.ai/oauth2/token"
	xaiScope        = "openid profile email offline_access grok-cli:access api:access"
	// xaiCallbackPort is the loopback port the Grok-CLI client registered its
	// redirect_uri against. xAI rejects redirect_uris that don't match the
	// registered host:port, so the callback server must bind this exact port
	// (xai.ts:36-43).
	xaiCallbackPort = 56121
)

// xaiProvider implements Provider for xAI/Grok via the authorization-code +
// PKCE loopback flow (xai.ts buildAuthorizeUrl / exchangeCodeForTokens).
type xaiProvider struct {
	authorizeURL string
	tokenURL     string
	httpClient   *http.Client
	// callbackPort overrides the registered loopback port; xaiCallbackPort in
	// production, 0 in tests so the OS picks a free port (no fixed-port clashes
	// between parallel test runs).
	callbackPort int
}

func newXaiProvider() *xaiProvider {
	return &xaiProvider{
		authorizeURL: xaiAuthorizeURL,
		tokenURL:     xaiTokenURL,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		callbackPort: xaiCallbackPort,
	}
}

func (p *xaiProvider) ID() string { return "xai" }

func (p *xaiProvider) CallbackPort() int { return p.callbackPort }

func (p *xaiProvider) Methods() []Method {
	return []Method{
		{Type: "oauth", Label: "Sign in with xAI"},
	}
}

func (p *xaiProvider) Authorize(_ context.Context, methodIndex int, _ map[string]string, redirectURI string) (Authorization, *Handshake, error) {
	if methodIndex != 0 {
		return Authorization{}, nil, fmt.Errorf("xai: unknown auth method index %d", methodIndex)
	}
	pkce, err := newPKCE()
	if err != nil {
		return Authorization{}, nil, err
	}
	state, err := newState()
	if err != nil {
		return Authorization{}, nil, err
	}
	nonce, err := newState()
	if err != nil {
		return Authorization{}, nil, err
	}

	// buildAuthorizeUrl (xai.ts:139-163). plan=generic + referrer=opencode match
	// what xAI's consent screen expects for the Grok-CLI client.
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {xaiClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {xaiScope},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"nonce":                 {nonce},
		"plan":                  {"generic"},
		"referrer":              {"opencode"},
	}
	authURL := p.authorizeURL + "?" + q.Encode()

	hs := &Handshake{
		State: state,
		Exchange: func(ctx context.Context, code string) (Token, error) {
			return p.exchange(ctx, code, pkce, redirectURI)
		},
	}
	return Authorization{
		URL:          authURL,
		Method:       "auto", // Opcode42's loopback server captures the redirect.
		Instructions: "Complete sign-in in your browser; Opcode42 will capture the redirect automatically.",
	}, hs, nil
}

// tokenResponse is the OAuth2 token endpoint JSON (xai.ts:99-106).
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// exchange swaps the authorization code for tokens (xai.ts:165-186).
func (p *xaiProvider) exchange(ctx context.Context, code string, pkce PKCE, redirectURI string) (Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {xaiClientID},
		"code_verifier": {pkce.Verifier},
	}
	tr, err := p.postToken(ctx, form)
	if err != nil {
		return Token{}, err
	}
	return tr, nil
}

// Refresh renews the access token via the refresh_token grant (xai.ts:188-202).
// xAI rotates the refresh_token, so the result carries whichever refresh token
// the server returned; postToken/Access retain the old one if it is omitted.
func (p *xaiProvider) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	if refreshToken == "" {
		return Token{}, fmt.Errorf("xai refresh: missing refresh token")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {xaiClientID},
	}
	tr, err := p.postToken(ctx, form)
	if err != nil {
		return Token{}, err
	}
	// Keep the old refresh token if the server did not rotate it (xai.ts:614 keeps
	// the prior refresh token; same rule as mcp-go refreshToken oauth.go:321-323).
	if tr.Refresh == "" {
		tr.Refresh = refreshToken
	}
	return tr, nil
}

// postToken POSTs a form-encoded body to the token endpoint and normalizes the
// response into a Token. expires is computed as now + expires_in (millis); 0
// when the server omits expires_in (xai.ts best-effort expires handling).
func (p *xaiProvider) postToken(ctx context.Context, form url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// xAI's auth server attributes requests by User-Agent (xai.ts authHeaders).
	req.Header.Set("User-Agent", "opcode42")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("xai token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("xai token exchange failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return Token{}, fmt.Errorf("xai token response decode: %w", err)
	}
	if tr.AccessToken == "" {
		return Token{}, fmt.Errorf("xai token response missing access_token")
	}
	var expires int64
	if tr.ExpiresIn > 0 {
		expires = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UnixMilli()
	}
	return Token{Access: tr.AccessToken, Refresh: tr.RefreshToken, Expires: expires}, nil
}
