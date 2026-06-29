package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rotemmiz/opcode42/internal/credstore"
)

// Sentinel errors map to opencode's ProviderAuth tagged errors
// (provider/auth.ts:67-83) so the HTTP layer can render the right 400 `name`.
var (
	// ErrUnknownProvider — providerID has no built-in OAuth method.
	ErrUnknownProvider = errors.New("unknown oauth provider")
	// ErrOauthMissing — callback called with no pending authorize
	// (ProviderAuthOauthMissing).
	ErrOauthMissing = errors.New("no pending oauth authorization")
	// ErrOauthCodeMissing — a "code" method callback arrived without a code
	// (ProviderAuthOauthCodeMissing).
	ErrOauthCodeMissing = errors.New("oauth authorization code missing")
	// ErrOauthCallbackFailed — the exchange/redirect failed
	// (ProviderAuthOauthCallbackFailed).
	ErrOauthCallbackFailed = errors.New("oauth callback failed")
)

// callbackTimeout bounds how long a loopback flow waits for the browser
// redirect before giving up (matches xai.ts waitForOAuthCallback 5min).
const callbackTimeout = 5 * time.Minute

// pending is one in-flight authorize() awaiting its callback() (provider/auth.ts
// `pending` Map entry).
type pending struct {
	kind      pendingKind
	handshake *Handshake
	// resultCh is set for loopback flows: the callback server pushes the
	// captured code (or error) here.
	resultCh <-chan callbackResult
}

// Service is the provider-OAuth orchestrator: it lists methods, starts authorize
// flows, and completes callbacks by persisting the token to the shared
// auth.json (credstore). It is the Go analogue of ProviderAuth.Service
// (provider/auth.ts:104-221), backed by a built-in provider registry rather than
// plugin hooks.
type Service struct {
	providers map[string]Provider
	cbServer  *callbackServer

	mu      sync.Mutex
	pending map[string]*pending // keyed by providerID
}

// NewService builds the OAuth service. proxyURL is the optional
// --oauth-callback-proxy-url for remote reachability (empty = loopback-only).
// Returns an error if proxyURL is malformed.
func NewService(proxyURL string) (*Service, error) {
	if err := validateProxyURL(proxyURL); err != nil {
		return nil, err
	}
	provs := map[string]Provider{}
	for _, p := range builtinProviders() {
		provs[p.ID()] = p
	}
	return &Service{
		providers: provs,
		cbServer:  newCallbackServer("/callback", proxyURL),
		pending:   map[string]*pending{},
	}, nil
}

// builtinProviders returns the providers Opcode42 ships OAuth for.
func builtinProviders() []Provider {
	return []Provider{newXaiProvider()}
}

// Methods returns the OAuth/api methods for every built-in provider, keyed by
// provider id (the GET /provider/auth shape; provider/auth.ts methods()).
func (s *Service) Methods() map[string][]Method {
	out := make(map[string][]Method, len(s.providers))
	for id, p := range s.providers {
		out[id] = p.Methods()
	}
	return out
}

// Authorize starts the OAuth flow for providerID/methodIndex. For loopback
// flows it lazily starts the shared callback server, registers the CSRF state,
// and returns the Authorization (with the browser URL) to the caller. The
// handshake is stored under providerID until Callback completes it.
func (s *Service) Authorize(ctx context.Context, providerID string, methodIndex int, inputs map[string]string) (Authorization, error) {
	prov, ok := s.providers[providerID]
	if !ok {
		return Authorization{}, fmt.Errorf("%w: %s", ErrUnknownProvider, providerID)
	}

	port, err := s.cbServer.ensureStarted(prov.CallbackPort())
	if err != nil {
		return Authorization{}, err
	}
	redirectURI := s.cbServer.redirectURI(port)

	auth, hs, err := prov.Authorize(ctx, methodIndex, inputs, redirectURI)
	if err != nil {
		return Authorization{}, err
	}

	p := &pending{handshake: hs}
	switch auth.Method {
	case "auto":
		// Loopback: register state so the callback server can route the redirect
		// back to this handshake.
		p.kind = pendingLoopback
		p.resultCh = s.cbServer.register(hs.State)
	case "code":
		p.kind = pendingCode
	default:
		return Authorization{}, fmt.Errorf("provider %s returned unsupported method %q", providerID, auth.Method)
	}

	// Replace any prior in-flight attempt for this provider, releasing its
	// loopback registration (xai.ts waitForOAuthCallback supersede behavior).
	s.mu.Lock()
	if prev, ok := s.pending[providerID]; ok && prev.kind == pendingLoopback {
		s.cbServer.unregister(prev.handshake.State)
	}
	s.pending[providerID] = p
	s.mu.Unlock()

	return auth, nil
}

// Callback completes a pending authorize. For "code" methods the caller supplies
// the pasted code; for loopback ("auto") methods it waits (bounded) for the
// browser redirect captured by the callback server. On success the token is
// persisted to auth.json as an Auth "oauth" record (auth/index.ts:13-20).
func (s *Service) Callback(ctx context.Context, providerID string, code string) error {
	s.mu.Lock()
	p := s.pending[providerID]
	if p != nil {
		delete(s.pending, providerID)
	}
	s.mu.Unlock()
	if p == nil {
		return ErrOauthMissing
	}

	var exchangeCode string
	switch p.kind {
	case pendingCode:
		if code == "" {
			return ErrOauthCodeMissing
		}
		exchangeCode = code
	case pendingLoopback:
		// Ensure the state registration is cleaned up if we bail early.
		defer s.cbServer.unregister(p.handshake.State)
		select {
		case res := <-p.resultCh:
			if res.err != nil {
				return fmt.Errorf("%w: %v", ErrOauthCallbackFailed, res.err)
			}
			exchangeCode = res.code
		case <-time.After(callbackTimeout):
			return fmt.Errorf("%w: timed out waiting for browser redirect", ErrOauthCallbackFailed)
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", ErrOauthCallbackFailed, ctx.Err())
		}
	}

	tok, err := p.handshake.Exchange(ctx, exchangeCode)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOauthCallbackFailed, err)
	}
	return persistOAuth(providerID, tok)
}

// Shutdown stops the shared callback server and releases pending handshakes.
func (s *Service) Shutdown(ctx context.Context) {
	s.cbServer.shutdown(ctx)
}

// oauthRecord is the auth.json "oauth" record shape (auth/index.ts:13-20).
// Fields with omitempty match opencode's optional fields.
type oauthRecord struct {
	Type      string `json:"type"`
	Refresh   string `json:"refresh"`
	Access    string `json:"access"`
	Expires   int64  `json:"expires"`
	AccountID string `json:"accountId,omitempty"`
}

// persistOAuth writes the token to the shared auth.json as an "oauth" record.
func persistOAuth(providerID string, tok Token) error {
	rec := oauthRecord{
		Type:      "oauth",
		Refresh:   tok.Refresh,
		Access:    tok.Access,
		Expires:   tok.Expires,
		AccountID: tok.AccountID,
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return credstore.Set(providerID, raw)
}
