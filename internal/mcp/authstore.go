package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

// authEntry is one MCP server's persisted OAuth state. It is the exact wire shape
// of opencode's McpAuth.Entry (mcp/auth.ts:23-30), so a token Opcode42 stores in
// mcp-auth.json is read by opencode and vice-versa. Fields are mutable across the
// flow: clientInfo is filled by dynamic client registration, codeVerifier/
// oauthState during the authorize step, tokens after the exchange.
type authEntry struct {
	Tokens       *authTokens     `json:"tokens,omitempty"`
	ClientInfo   *authClientInfo `json:"clientInfo,omitempty"`
	CodeVerifier string          `json:"codeVerifier,omitempty"`
	OAuthState   string          `json:"oauthState,omitempty"`
	ServerURL    string          `json:"serverUrl,omitempty"`
}

// authTokens mirrors McpAuth.Tokens (mcp/auth.ts:7-12). expiresAt is unix
// seconds (matching opencode's Date.now()/1000 math in oauth-provider.ts:128).
type authTokens struct {
	AccessToken  string  `json:"accessToken"`
	RefreshToken string  `json:"refreshToken,omitempty"`
	ExpiresAt    float64 `json:"expiresAt,omitempty"`
	Scope        string  `json:"scope,omitempty"`
}

// authClientInfo mirrors McpAuth.ClientInfo (mcp/auth.ts:15-20): the credentials
// from RFC 7591 dynamic client registration.
type authClientInfo struct {
	ClientID              string  `json:"clientId"`
	ClientSecret          string  `json:"clientSecret,omitempty"`
	ClientIDIssuedAt      float64 `json:"clientIdIssuedAt,omitempty"`
	ClientSecretExpiresAt float64 `json:"clientSecretExpiresAt,omitempty"`
}

// authStoreMu serializes the read-modify-write of mcp-auth.json across the
// daemon's concurrent HTTP handlers (the same discipline credstore uses for
// auth.json — a file-level RMW race the -race detector can't catch).
var authStoreMu sync.Mutex

// mcpAuthPath is overridable in tests so the suite never touches a real
// ~/.local/share/opencode/mcp-auth.json. When empty it resolves to the XDG path.
var mcpAuthPath string

// authStorePath is $DATA/opencode/mcp-auth.json, honoring XDG_DATA_HOME
// (Global.Path.data in opencode; mcp/auth.ts:35).
func authStorePath() string {
	if mcpAuthPath != "" {
		return mcpAuthPath
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "mcp-auth.json"
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "opencode", "mcp-auth.json")
}

// loadAuthData reads the whole mcp-auth.json map. A missing/unreadable store is
// an empty map, not an error (matching McpAuth.all's catch). Callers hold
// authStoreMu.
func loadAuthData() map[string]authEntry {
	out := map[string]authEntry{}
	b, err := os.ReadFile(authStorePath())
	if err != nil || len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

// writeAuthData persists the whole map with mode 0600 (mcp/auth.ts:85). Callers
// hold authStoreMu.
func writeAuthData(data map[string]authEntry) error {
	path := authStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-auth-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// getAuthEntry returns the stored entry for name (or the zero entry, ok=false).
func getAuthEntry(name string) (authEntry, bool) {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()
	e, ok := loadAuthData()[name]
	return e, ok
}

// mutateAuthEntry applies fn to name's entry (creating it if absent) and writes
// the store back atomically.
func mutateAuthEntry(name string, fn func(*authEntry)) error {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()
	data := loadAuthData()
	e := data[name]
	fn(&e)
	data[name] = e
	return writeAuthData(data)
}

// removeAuthEntry deletes name's entry (no error if absent), matching
// McpAuth.remove (mcp/auth.ts:88-92).
func removeAuthEntry(name string) error {
	authStoreMu.Lock()
	defer authStoreMu.Unlock()
	data := loadAuthData()
	delete(data, name)
	return writeAuthData(data)
}

// hasStoredTokens reports whether name has persisted access tokens
// (MCP.hasStoredTokens, mcp/index.ts:935-938).
func hasStoredTokens(name string) bool {
	e, ok := getAuthEntry(name)
	return ok && e.Tokens != nil && e.Tokens.AccessToken != ""
}

// persistentTokenStore is the mcp-go transport.TokenStore backed by mcp-auth.json
// for one server. It scopes tokens to serverURL (McpAuth.getForUrl, mcp/auth.ts:
// 74-80): a stored token whose serverUrl no longer matches the configured URL is
// treated as absent so a URL change forces re-auth.
type persistentTokenStore struct {
	name      string
	serverURL string
}

var _ transport.TokenStore = (*persistentTokenStore)(nil)

// GetToken returns the persisted token, or ErrNoToken when absent (so the
// transport triggers the auth flow rather than treating it as an error).
func (s *persistentTokenStore) GetToken(ctx context.Context) (*transport.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e, ok := getAuthEntry(s.name)
	if !ok || e.Tokens == nil || e.Tokens.AccessToken == "" {
		return nil, transport.ErrNoToken
	}
	// URL-scope the token: a token saved for a different server URL is not valid
	// here (getForUrl).
	if e.ServerURL != "" && e.ServerURL != s.serverURL {
		return nil, transport.ErrNoToken
	}
	tok := &transport.Token{
		AccessToken:  e.Tokens.AccessToken,
		TokenType:    "Bearer",
		RefreshToken: e.Tokens.RefreshToken,
		Scope:        e.Tokens.Scope,
	}
	if e.Tokens.ExpiresAt > 0 {
		tok.ExpiresAt = time.Unix(int64(e.Tokens.ExpiresAt), 0)
	}
	return tok, nil
}

// SaveToken persists the token (and the current serverURL scope), mirroring
// McpOAuthProvider.saveTokens (oauth-provider.ts:121-135).
func (s *persistentTokenStore) SaveToken(ctx context.Context, token *transport.Token) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return mutateAuthEntry(s.name, func(e *authEntry) {
		t := &authTokens{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			Scope:        token.Scope,
		}
		if !token.ExpiresAt.IsZero() {
			t.ExpiresAt = float64(token.ExpiresAt.Unix())
		} else if token.ExpiresIn > 0 {
			t.ExpiresAt = float64(time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Unix())
		}
		e.Tokens = t
		e.ServerURL = s.serverURL
	})
}
