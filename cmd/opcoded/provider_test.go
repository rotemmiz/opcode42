package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/provider/anthropic"
	"github.com/rotemmiz/opcode42/internal/engine/provider/openai"
	"github.com/rotemmiz/opcode42/internal/oauth"
)

// stubAccessor is a deterministic credresolve.Accessor — no live provider/HTTP.
type stubAccessor struct {
	token string
	err   error
}

func (s stubAccessor) Access(context.Context, string) (string, error) {
	return s.token, s.err
}

// testCatalog gives xai an OpenAI-compatible base URL and anthropic its native
// one so providerFactory can build a client without env vars.
func testCatalog() catalog.Catalog {
	return catalog.Catalog{
		"xai":       {API: "https://api.x.ai/v1", Env: []string{"XAI_API_KEY"}},
		"anthropic": {API: "https://api.anthropic.com", NPM: "@ai-sdk/anthropic", Env: []string{"ANTHROPIC_API_KEY"}},
	}
}

func TestProviderFactory_OAuthTokenBecomesBearer(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-key-should-be-overridden")
	f := providerFactory(testCatalog(), stubAccessor{token: "oauth-access-tok"})
	prov, err := f(context.Background(), "xai", "grok-beta")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	c, ok := prov.(*openai.Client)
	if !ok {
		t.Fatalf("xai routed to %T, want *openai.Client", prov)
	}
	// The OpenAI client emits APIKey as Authorization: Bearer, so the OAuth token
	// must land in APIKey, overriding the env-var key.
	if c.APIKey != "oauth-access-tok" {
		t.Fatalf("APIKey = %q, want the OAuth access token (it must override the env key)", c.APIKey)
	}
}

func TestProviderFactory_APIKeyPathUnchangedWhenNoOAuth(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-static-key")
	// No OAuth record for this provider → fall back to the env api key.
	acc := stubAccessor{err: fmt.Errorf("%w: xai", oauth.ErrNoOAuthToken)}
	f := providerFactory(testCatalog(), acc)
	prov, err := f(context.Background(), "xai", "grok-beta")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	c := prov.(*openai.Client)
	if c.APIKey != "env-static-key" {
		t.Fatalf("APIKey = %q, want env-static-key (api-key path must be unchanged)", c.APIKey)
	}
}

func TestProviderFactory_NilAccessorUsesAPIKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-static-key")
	f := providerFactory(testCatalog(), nil)
	prov, err := f(context.Background(), "xai", "grok-beta")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if c := prov.(*openai.Client); c.APIKey != "env-static-key" {
		t.Fatalf("APIKey = %q, want env-static-key", c.APIKey)
	}
}

func TestProviderFactory_NeedsReauthSurfaced(t *testing.T) {
	acc := stubAccessor{err: fmt.Errorf("%w: xai", oauth.ErrNeedsReauth)}
	f := providerFactory(testCatalog(), acc)
	_, err := f(context.Background(), "xai", "grok-beta")
	if err == nil {
		t.Fatal("factory must surface ErrNeedsReauth, not build a client with a dead credential")
	}
	if !errors.Is(err, oauth.ErrNeedsReauth) {
		t.Fatalf("err = %v, want wrapped ErrNeedsReauth", err)
	}
}

func TestProviderFactory_AnthropicOAuthUsesBearerHeaderNotAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-ant-key")
	f := providerFactory(testCatalog(), stubAccessor{token: "ant-oauth-tok"})
	prov, err := f(context.Background(), "anthropic", "claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	c, ok := prov.(*anthropic.Client)
	if !ok {
		t.Fatalf("anthropic routed to %T, want *anthropic.Client", prov)
	}
	// Anthropic OAuth authenticates via Authorization: Bearer, not x-api-key, so
	// the OAuth token must NOT be placed in APIKey (which maps to x-api-key).
	if c.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty (OAuth must not use the x-api-key path)", c.APIKey)
	}
	if got := c.Headers["authorization"]; got != "Bearer ant-oauth-tok" {
		t.Fatalf("authorization header = %q, want %q", got, "Bearer ant-oauth-tok")
	}
}

func TestProviderFactory_AnthropicAPIKeyPathUnchanged(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-ant-key")
	acc := stubAccessor{err: fmt.Errorf("%w: anthropic", oauth.ErrNoOAuthToken)}
	f := providerFactory(testCatalog(), acc)
	prov, err := f(context.Background(), "anthropic", "claude-3-5-sonnet")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	c := prov.(*anthropic.Client)
	if c.APIKey != "env-ant-key" {
		t.Fatalf("APIKey = %q, want env-ant-key (x-api-key path unchanged)", c.APIKey)
	}
	if _, ok := c.Headers["authorization"]; ok {
		t.Fatal("static api-key path must not set an authorization header")
	}
}
