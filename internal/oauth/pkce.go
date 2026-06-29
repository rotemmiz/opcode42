// Package oauth implements end-to-end provider OAuth for Opcode42: the
// authorize → loopback/callback → token-exchange → persisted-auth flow that lets
// a provider requiring OAuth (vs a pasted API key) be authenticated.
//
// It is the Go analogue of opencode's provider-auth surface
// (packages/opencode/src/provider/auth.ts) plus the per-provider loopback OAuth
// implementations opencode ships as plugins
// (e.g. packages/opencode/src/plugin/xai.ts). opencode discovers OAuth methods
// from plugin `auth` hooks; Opcode42 has no plugin host for auth yet, so the same
// methods are provided by a built-in registry (registry.go). The persisted
// result is written to the SAME ~/.local/share/opencode/auth.json store opencode
// uses (internal/credstore), in the exact Auth union shape
// (auth/index.ts:13-32): {type:"oauth",refresh,access,expires,...} or
// {type:"api",key,metadata?}.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// pkceChars is the unreserved character set for the code_verifier
// (RFC 7636 §4.1; matches xai.ts generateRandomString).
const pkceChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// PKCE holds a code_verifier/code_challenge pair for the S256 method.
type PKCE struct {
	Verifier  string
	Challenge string
}

// newPKCE generates a 64-char verifier and its S256 challenge
// (RFC 7636; xai.ts:55-59 generatePKCE).
func newPKCE() (PKCE, error) {
	v, err := randomString(64)
	if err != nil {
		return PKCE{}, err
	}
	sum := sha256.Sum256([]byte(v))
	return PKCE{Verifier: v, Challenge: base64URL(sum[:])}, nil
}

// newState returns a base64url-encoded 32-byte random CSRF state value
// (xai.ts:79-81 generateState).
func newState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64URL(b), nil
}

// randomString returns a length-n string over pkceChars using modulo mapping
// (matches xai.ts generateRandomString byte→char mapping).
func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, x := range b {
		out[i] = pkceChars[int(x)%len(pkceChars)]
	}
	return string(out), nil
}

// base64URL is unpadded base64url (RFC 4648 §5), matching the JS
// base64UrlEncode used for code_challenge and state.
func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
