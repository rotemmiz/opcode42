package credresolve

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rotemmiz/opcode42/internal/oauth"
)

// stubAccessor is a deterministic Accessor: it returns the configured token/err
// and records the providerID it was asked about. No live provider or HTTP.
type stubAccessor struct {
	token   string
	err     error
	calls   int
	lastReq string
}

func (s *stubAccessor) Access(_ context.Context, providerID string) (string, error) {
	s.calls++
	s.lastReq = providerID
	return s.token, s.err
}

func TestResolve_OAuthTokenWins(t *testing.T) {
	acc := &stubAccessor{token: "oauth-access-tok"}
	got, err := Resolve(context.Background(), acc, "xai", "env-api-key")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.APIKey != "oauth-access-tok" {
		t.Fatalf("APIKey = %q, want oauth-access-tok (OAuth must override env api key)", got.APIKey)
	}
	if !got.OAuth {
		t.Fatal("OAuth flag should be true for an OAuth-resolved token")
	}
	if acc.lastReq != "xai" {
		t.Fatalf("accessor queried %q, want xai", acc.lastReq)
	}
}

func TestResolve_NoOAuthRecordFallsBackToAPIKey(t *testing.T) {
	// ErrNoOAuthToken is wrapped (Access wraps it with %w), so test the wrapped form.
	acc := &stubAccessor{err: fmt.Errorf("%w: groq", oauth.ErrNoOAuthToken)}
	got, err := Resolve(context.Background(), acc, "groq", "env-api-key")
	if err != nil {
		t.Fatalf("Resolve should not surface ErrNoOAuthToken: %v", err)
	}
	if got.APIKey != "env-api-key" {
		t.Fatalf("APIKey = %q, want env-api-key (api-key path unchanged)", got.APIKey)
	}
	if got.OAuth {
		t.Fatal("OAuth flag should be false when falling back to the api key")
	}
}

func TestResolve_UnknownProviderFallsBackToAPIKey(t *testing.T) {
	acc := &stubAccessor{err: fmt.Errorf("%w: openai", oauth.ErrUnknownProvider)}
	got, err := Resolve(context.Background(), acc, "openai", "sk-openai")
	if err != nil {
		t.Fatalf("Resolve should not surface ErrUnknownProvider: %v", err)
	}
	if got.APIKey != "sk-openai" || got.OAuth {
		t.Fatalf("got %+v, want static api key fallback", got)
	}
}

func TestResolve_NeedsReauthSurfaced(t *testing.T) {
	acc := &stubAccessor{err: fmt.Errorf("%w: xai", oauth.ErrNeedsReauth)}
	_, err := Resolve(context.Background(), acc, "xai", "env-api-key")
	if err == nil {
		t.Fatal("Resolve must surface ErrNeedsReauth, not fall back silently")
	}
	if !errors.Is(err, oauth.ErrNeedsReauth) {
		t.Fatalf("err = %v, want wrapped ErrNeedsReauth", err)
	}
}

func TestResolve_NilAccessorUsesAPIKey(t *testing.T) {
	got, err := Resolve(context.Background(), nil, "anthropic", "sk-ant")
	if err != nil {
		t.Fatalf("Resolve(nil accessor): %v", err)
	}
	if got.APIKey != "sk-ant" || got.OAuth {
		t.Fatalf("got %+v, want plain api-key credential", got)
	}
}

// oauth.Service must satisfy Accessor so the daemon can pass it straight through.
var _ Accessor = (*oauth.Service)(nil)
