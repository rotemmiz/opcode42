// Package credresolve picks the credential a provider/model request should use,
// resolving an OAuth access token ahead of the static API-key path — the same
// precedence opencode's per-provider auth `loader` applies.
//
// opencode reference (packages/opencode/src/plugin/xai.ts:575-660): when a
// provider has an "oauth" auth record, its loader returns a fetch override that
// refreshes the access token if needed and injects
// `authorization: Bearer ${access}` (xai.ts:657), overriding the dummy
// apiKey-derived bearer. When the record is not "oauth" (e.g. the user pasted an
// api key), the loader passes the request through untouched so the apiKey path
// governs (xai.ts:596). credresolve.Resolve mirrors that branch: an OAuth token
// wins; otherwise the caller's existing API-key resolution is used unchanged.
package credresolve

import (
	"context"
	"errors"

	"github.com/rotemmiz/opcode42/internal/oauth"
)

// Accessor resolves a provider's live OAuth access token, refreshing it when the
// stored token is expired. *oauth.Service satisfies this; tests supply a stub.
// It is intentionally narrow (just the consumer-side Access call) so the engine
// provider path depends on the OAuth service's behavior, not its construction.
type Accessor interface {
	// Access returns a usable OAuth access token for providerID, or one of the
	// oauth package's sentinel errors (ErrNoOAuthToken / ErrUnknownProvider when
	// there is no OAuth method or record; ErrNeedsReauth when a refresh failed).
	Access(ctx context.Context, providerID string) (string, error)
}

// Credential is the resolved auth material for a single provider request.
type Credential struct {
	// APIKey is the credential the provider client sends (Bearer token for the
	// OpenAI-compatible client, x-api-key for Anthropic). It is either an OAuth
	// access token (when OAuth is true) or the static API key.
	APIKey string
	// OAuth reports whether APIKey came from an OAuth access token (true) versus
	// the static API-key path (false). It lets the provider client choose a
	// Bearer header for OAuth even on providers whose static path uses a
	// different header. For Opcode42's only OAuth provider today (xai, an
	// OpenAI-compatible endpoint) both paths use Authorization: Bearer, so this
	// is informational; it future-proofs Anthropic OAuth (Claude Pro/Max), which
	// switches from x-api-key to Authorization: Bearer when OAuth-backed.
	OAuth bool
}

// Resolve decides the credential for providerID, preferring an OAuth access
// token over apiKey. accessor may be nil (OAuth disabled), in which case the
// static apiKey is returned untouched.
//
//   - A live OAuth token  → Credential{APIKey: token, OAuth: true}.
//   - No OAuth method/record for this provider (ErrNoOAuthToken /
//     ErrUnknownProvider) → fall back to the static apiKey (Credential{APIKey:
//     apiKey}). This is the common case: a provider authenticated by env-var
//     API key has no OAuth record, and opencode's loader passes such requests
//     through unchanged (xai.ts:577,596).
//   - ErrNeedsReauth (a stored token expired and its refresh failed) → returned
//     to the caller so the request fails loudly with a re-auth signal instead of
//     silently calling the provider with a dead/absent credential.
func Resolve(ctx context.Context, accessor Accessor, providerID, apiKey string) (Credential, error) {
	if accessor == nil {
		return Credential{APIKey: apiKey}, nil
	}
	token, err := accessor.Access(ctx, providerID)
	if err == nil {
		return Credential{APIKey: token, OAuth: true}, nil
	}
	// No OAuth configured/stored for this provider: use the static API key.
	if errors.Is(err, oauth.ErrNoOAuthToken) || errors.Is(err, oauth.ErrUnknownProvider) {
		return Credential{APIKey: apiKey}, nil
	}
	// ErrNeedsReauth (or any other refresh-path error): surface it. The engine's
	// provider factory call site returns this up the loop, failing the request
	// with the re-auth signal rather than crashing or silently misauthenticating.
	return Credential{}, err
}
