package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client/transport"
)

// mockToken builds a non-expiring transport.Token with the given access token.
func mockToken(access string) transport.Token {
	return transport.Token{AccessToken: access, TokenType: "Bearer"}
}

// mockTokenExpiring builds a transport.Token expiring in expiresIn seconds.
func mockTokenExpiring(access string, expiresIn int64) transport.Token {
	return transport.Token{AccessToken: access, TokenType: "Bearer", ExpiresIn: expiresIn}
}

// TestAuthStore_RoundTrip persists an entry and reads it back, proving the
// mcp-auth.json shape round-trips (tokens + clientInfo + flow state).
func TestAuthStore_RoundTrip(t *testing.T) {
	isolateAuthStore(t)

	if err := mutateAuthEntry("srv", func(e *authEntry) {
		e.Tokens = &authTokens{AccessToken: "acc", RefreshToken: "ref", Scope: "s"}
		e.ClientInfo = &authClientInfo{ClientID: "cid", ClientSecret: "sec"}
		e.CodeVerifier = "verif"
		e.OAuthState = "state"
		e.ServerURL = "https://srv"
	}); err != nil {
		t.Fatal(err)
	}

	e, ok := getAuthEntry("srv")
	if !ok {
		t.Fatal("entry not found after write")
	}
	if e.Tokens == nil || e.Tokens.AccessToken != "acc" || e.Tokens.RefreshToken != "ref" {
		t.Fatalf("tokens round-trip wrong: %+v", e.Tokens)
	}
	if e.ClientInfo == nil || e.ClientInfo.ClientID != "cid" {
		t.Fatalf("clientInfo round-trip wrong: %+v", e.ClientInfo)
	}
	if e.CodeVerifier != "verif" || e.OAuthState != "state" || e.ServerURL != "https://srv" {
		t.Fatalf("flow state round-trip wrong: %+v", e)
	}

	if err := removeAuthEntry("srv"); err != nil {
		t.Fatal(err)
	}
	if _, ok := getAuthEntry("srv"); ok {
		t.Fatal("entry should be gone after remove")
	}
}

// TestPersistentTokenStore_URLScope proves a stored token is only returned for
// the matching server URL (McpAuth.getForUrl), so a URL change forces re-auth.
func TestPersistentTokenStore_URLScope(t *testing.T) {
	isolateAuthStore(t)
	ctx := context.Background()

	store := &persistentTokenStore{name: "srv", serverURL: "https://a"}
	tk := mockToken("https-a-token")
	if err := store.SaveToken(ctx, &tk); err != nil {
		t.Fatal(err)
	}

	// Same URL → returns the token.
	tok, err := store.GetToken(ctx)
	if err != nil || tok.AccessToken != "https-a-token" {
		t.Fatalf("same-url GetToken = %v, err=%v", tok, err)
	}

	// Different URL → ErrNoToken (so the transport re-triggers auth).
	other := &persistentTokenStore{name: "srv", serverURL: "https://b"}
	if _, err := other.GetToken(ctx); err == nil {
		t.Fatal("token stored for https://a must not be returned for https://b")
	}
}

// TestPersistentTokenStore_ExpiresAt proves ExpiresIn is converted to an absolute
// expiry on save and read back as ExpiresAt.
func TestPersistentTokenStore_ExpiresAt(t *testing.T) {
	isolateAuthStore(t)
	ctx := context.Background()
	store := &persistentTokenStore{name: "srv", serverURL: "https://a"}

	tk := mockTokenExpiring("tok", 3600)
	if err := store.SaveToken(ctx, &tk); err != nil {
		t.Fatal(err)
	}
	tok, err := store.GetToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tok.ExpiresAt.IsZero() || tok.ExpiresAt.Before(time.Now()) {
		t.Fatalf("expiry not in the future: %v", tok.ExpiresAt)
	}
}
