package mcp

import "testing"

func TestParseConfig(t *testing.T) {
	cfg := map[string]any{"mcp": map[string]any{
		"local-srv": map[string]any{
			"type":        "local",
			"command":     []any{"my-server", "--flag"},
			"environment": map[string]any{"KEY": "val"},
			"timeout":     float64(5000),
		},
		"remote-srv": map[string]any{
			"type":    "remote",
			"url":     "https://mcp.example.com",
			"headers": map[string]any{"Authorization": "Bearer x"},
			"enabled": false,
		},
		"bad": map[string]any{"type": "weird"}, // skipped
	}}
	got := ParseConfig(cfg)
	if len(got) != 2 {
		t.Fatalf("want 2 servers, got %d: %v", len(got), got)
	}
	loc := got["local-srv"]
	if loc.Type != "local" || len(loc.Command) != 2 || loc.Command[0] != "my-server" {
		t.Fatalf("local parse wrong: %+v", loc)
	}
	if loc.Environment["KEY"] != "val" || loc.Timeout != 5000 {
		t.Fatalf("local env/timeout wrong: %+v", loc)
	}
	if !loc.enabled() {
		t.Error("local should be enabled (absent ⇒ enabled)")
	}
	rem := got["remote-srv"]
	if rem.Type != "remote" || rem.URL != "https://mcp.example.com" || rem.Headers["Authorization"] == "" {
		t.Fatalf("remote parse wrong: %+v", rem)
	}
	if rem.enabled() {
		t.Error("remote should be disabled")
	}
}

func TestParseConfig_NoMCP(t *testing.T) {
	if ParseConfig(map[string]any{}) != nil {
		t.Fatal("no mcp key ⇒ nil")
	}
}

// TestParseConfig_Name proves each parsed server carries its config key as Name
// (used to scope the OAuth token store).
func TestParseConfig_Name(t *testing.T) {
	got := ParseConfig(map[string]any{"mcp": map[string]any{
		"my-srv": map[string]any{"type": "local", "command": []any{"x"}},
	}})
	if got["my-srv"].Name != "my-srv" {
		t.Fatalf("Name = %q, want my-srv", got["my-srv"].Name)
	}
}

// TestParseConfig_OAuthField exercises the three-way `oauth` discriminator:
// false → disabled, object → config, absent → auto-detect.
func TestParseConfig_OAuthField(t *testing.T) {
	got := ParseConfig(map[string]any{"mcp": map[string]any{
		"off": map[string]any{"type": "remote", "url": "https://a", "oauth": false},
		"obj": map[string]any{"type": "remote", "url": "https://b", "oauth": map[string]any{
			"clientId": "cid", "clientSecret": "sec", "scope": "read write",
			"callbackPort": float64(12345), "redirectUri": "https://cb",
		}},
		"auto": map[string]any{"type": "remote", "url": "https://c"},
	}})

	if !got["off"].oauthDisabled() {
		t.Error("oauth:false should disable")
	}
	if got["off"].OAuth.Config != nil {
		t.Error("oauth:false should not produce a config object")
	}

	obj := got["obj"]
	if obj.oauthDisabled() || obj.OAuth.Config == nil {
		t.Fatalf("oauth object not parsed: %+v", obj.OAuth)
	}
	c := obj.OAuth.Config
	if c.ClientID != "cid" || c.ClientSecret != "sec" || c.Scope != "read write" ||
		c.CallbackPort != 12345 || c.RedirectURI != "https://cb" {
		t.Fatalf("oauth config fields wrong: %+v", c)
	}

	auto := got["auto"]
	if auto.oauthDisabled() || auto.OAuth.Config != nil {
		t.Fatalf("absent oauth should be auto-detect: %+v", auto.OAuth)
	}
}

// TestRedirectURI exercises the explicit > callbackPort > default resolution.
func TestRedirectURI(t *testing.T) {
	def := redirectURI(Server{Type: "remote"})
	if def != "http://127.0.0.1:19876/mcp/oauth/callback" {
		t.Fatalf("default redirect = %q", def)
	}
	port := redirectURI(Server{Type: "remote", OAuth: OAuthField{Config: &OAuthConfig{CallbackPort: 5000}}})
	if port != "http://127.0.0.1:5000/mcp/oauth/callback" {
		t.Fatalf("callbackPort redirect = %q", port)
	}
	explicit := redirectURI(Server{Type: "remote", OAuth: OAuthField{Config: &OAuthConfig{
		CallbackPort: 5000, RedirectURI: "https://example.com/cb",
	}}})
	if explicit != "https://example.com/cb" {
		t.Fatalf("explicit redirect = %q", explicit)
	}
}
