package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rotemmiz/forge/internal/credstore"
)

// ErrNoOAuthToken — the provider has no stored "oauth" record (never signed in,
// or the record is an api/wellknown type). The caller should drive the authorize
// flow rather than retry.
var ErrNoOAuthToken = errors.New("no stored oauth token for provider")

// ErrNeedsReauth — a stored token is expired/near-expiry and the refresh_token
// grant failed (revoked/expired refresh token, network error, auth-server
// rejection). The caller must re-run the interactive authorize flow; this maps to
// opencode's "needs auth" outcome where the refresh path gives up and the 401/
// re-auth path takes over (xai.ts loader leaves the failure to the caller).
var ErrNeedsReauth = errors.New("oauth token expired and refresh failed; re-authentication required")

// accessTokenRefreshSkew is how long before the real expiry we proactively
// refresh, so a single long-running request doesn't have to recover from a
// mid-flight 401 (xai.ts ACCESS_TOKEN_REFRESH_SKEW_MS = 120_000).
const accessTokenRefreshSkew = 120 * time.Second

// refreshGroup serializes concurrent refreshes for the same provider so two
// in-flight requests don't each POST the (rotating) refresh_token — replaying a
// consumed refresh_token would have the second call fail. This is the Go analogue
// of xai.ts's single-flight refreshPromise (xai.ts:579-636), but keyed by
// provider so distinct providers refresh independently.
var refreshGroup = newSingleFlight()

// now is overridable in tests so expiry math is deterministic.
var now = time.Now

// Access returns a usable access token for providerID, refreshing it first when
// the stored token is expired or within the skew window. It is the Go analogue of
// opencode's per-provider auth `loader` (xai.ts:575-657): read the stored oauth
// record, decide whether it needs refreshing, run the refresh_token grant if so,
// persist the renewed token back to the shared auth.json, and hand back the live
// access token. A provider with no built-in OAuth method, or no stored oauth
// record, yields ErrUnknownProvider / ErrNoOAuthToken respectively; a refresh
// that fails yields ErrNeedsReauth.
func (s *Service) Access(ctx context.Context, providerID string) (string, error) {
	prov, ok := s.providers[providerID]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownProvider, providerID)
	}
	rec, ok := loadOAuthRecord(providerID)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoOAuthToken, providerID)
	}
	if !needsRefresh(rec) {
		return rec.Access, nil
	}
	if rec.Refresh == "" {
		// Expired with nothing to refresh from: the user must re-authenticate.
		return "", fmt.Errorf("%w: %s", ErrNeedsReauth, providerID)
	}

	// Single-flight the refresh per provider; concurrent callers share the one
	// HTTP round-trip and its persisted result.
	res, err := refreshGroup.Do(providerID, func() (any, error) {
		// Re-read under the flight so a refresh that another goroutine just
		// persisted is observed instead of refreshing again (the loaded record may
		// be stale by the time we win the flight).
		cur, ok := loadOAuthRecord(providerID)
		if !ok {
			return "", fmt.Errorf("%w: %s", ErrNoOAuthToken, providerID)
		}
		if !needsRefresh(cur) {
			return cur.Access, nil
		}
		if cur.Refresh == "" {
			return "", fmt.Errorf("%w: %s", ErrNeedsReauth, providerID)
		}
		tok, rerr := prov.Refresh(ctx, cur.Refresh)
		if rerr != nil {
			return "", fmt.Errorf("%w: %s: %v", ErrNeedsReauth, providerID, rerr)
		}
		if tok.Refresh == "" {
			tok.Refresh = cur.Refresh
		}
		// Carry over the account id, which the refresh response doesn't echo.
		if tok.AccountID == "" {
			tok.AccountID = cur.AccountID
		}
		if perr := persistOAuth(providerID, tok); perr != nil {
			return "", perr
		}
		return tok.Access, nil
	})
	if err != nil {
		return "", err
	}
	return res.(string), nil
}

// loadOAuthRecord reads providerID's stored credential and decodes it as an oauth
// record. ok is false when there is no record or it is not an "oauth" type.
func loadOAuthRecord(providerID string) (oauthRecord, bool) {
	raw, ok := credstore.Load()[providerID]
	if !ok {
		return oauthRecord{}, false
	}
	var rec oauthRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.Type != "oauth" {
		return oauthRecord{}, false
	}
	return rec, true
}

// needsRefresh reports whether a stored token should be refreshed before use: its
// stored expiry is missing or within the skew window, or — for JWT access tokens
// — the JWT exp claim itself is within the skew. This mirrors opencode's combined
// stored-expires + JWT-exp check (xai.ts:603-606): the stored expires field is
// best-effort (xAI doesn't always return expires_in), so the JWT check backstops
// it. An opaque (non-JWT) token with a healthy stored expiry is left as-is.
func needsRefresh(rec oauthRecord) bool {
	deadline := now().Add(accessTokenRefreshSkew).UnixMilli()
	if rec.Expires == 0 || rec.Expires <= deadline {
		return true
	}
	return accessTokenIsExpiring(rec.Access, accessTokenRefreshSkew)
}

// accessTokenIsExpiring reports whether a JWT access token's exp claim is within
// skew of now. It decodes the JWT payload WITHOUT verifying the signature — used
// only to decide whether to proactively refresh, never for a trust decision, so
// unsigned decode is safe (xai.ts accessTokenIsExpiring:121-135). An opaque
// (non-JWT) token, or one without a numeric exp, returns false so the stored-
// expires check alone governs it.
func accessTokenIsExpiring(token string, skew time.Duration) bool {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return false
	}
	var claims struct {
		Exp *float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == nil {
		return false
	}
	expMillis := int64(*claims.Exp * 1000)
	return expMillis <= now().Add(skew).UnixMilli()
}

// singleFlight collapses concurrent calls keyed by string onto one execution,
// sharing its result. It is a tiny local stand-in for golang.org/x/sync/singleflight
// (not a current dependency) covering exactly the per-provider refresh dedupe need.
type singleFlight struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	wg  sync.WaitGroup
	val any
	err error
}

func newSingleFlight() *singleFlight {
	return &singleFlight{calls: map[string]*flightCall{}}
}

// Do runs fn for key, deduplicating concurrent calls: callers that arrive while a
// call is in flight wait for and share its result.
func (g *singleFlight) Do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &flightCall{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	// Release the flight even if fn panics, so a panicking refresh can't wedge
	// every later caller for this key on a wg that is never Done (the re-panic
	// preserves the original failure for this caller). Mirrors x/sync/singleflight.
	defer func() {
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		c.wg.Done()
	}()

	c.val, c.err = fn()
	return c.val, c.err
}
