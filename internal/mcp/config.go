// Package mcp connects to the user's configured MCP (Model Context Protocol)
// servers and exposes their tools to the agent. It covers config parsing,
// per-instance connection to local (stdio) and remote (StreamableHTTP with SSE
// fallback) servers, status reporting (GET /mcp), merging MCP tools into the
// agent loop (permission-gated like built-ins), and the tools/list_changed
// watcher that emits mcp.tools.changed. Remote servers requiring OAuth surface
// the needs_auth / needs_client_registration statuses and are authenticated via
// the mutating /mcp endpoints (POST /mcp add, connect/disconnect, and the
// /mcp/:name/auth* OAuth flow). Mirrors opencode's config/mcp.ts + mcp/index.ts.
package mcp

// Server is one configured MCP server (config/mcp.ts Local|Remote, discriminated
// on Type).
type Server struct {
	// Name is the config key for this server. It scopes the OAuth token store
	// (mcp-auth.json is keyed by server name); set by ParseConfig / the manager.
	Name        string
	Type        string            // "local" | "remote"
	Command     []string          // local: command + args
	Environment map[string]string // local
	URL         string            // remote
	Headers     map[string]string // remote
	Enabled     *bool             // nil ⇒ enabled (opencode default)
	Timeout     int               // ms; 0 ⇒ default
	// OAuth is the remote-server OAuth configuration (config/mcp.ts OAuth union):
	//   - OAuthField.Disabled ⇒ `oauth: false` in config (opt out of all OAuth)
	//   - OAuthField.Config non-nil ⇒ an explicit OAuth object (clientId/scope/…)
	//   - both zero ⇒ absent ⇒ auto-detect (RFC 8414 discovery + RFC 7591 DCR)
	OAuth OAuthField // remote only
}

// OAuthField is the three-way `oauth` discriminator from config/mcp.ts:48
// (Union([OAuth, Literal(false)])): absent (auto-detect) | false (disabled) |
// an OAuth config object.
type OAuthField struct {
	Disabled bool         // true ⇒ `oauth: false`
	Config   *OAuthConfig // non-nil ⇒ explicit object
}

// OAuthConfig mirrors config/mcp.ts OAuth (lines 21-36). All fields optional;
// absence of clientId triggers RFC 7591 dynamic client registration.
type OAuthConfig struct {
	ClientID     string // pre-registered client id (skips DCR)
	ClientSecret string // confidential-client secret
	Scope        string // requested OAuth scope
	CallbackPort int    // 1-65535; loopback callback port shorthand
	RedirectURI  string // explicit redirect URI (overrides callbackPort/default)
}

// enabled reports whether the server should be connected (absent ⇒ enabled).
func (s Server) enabled() bool { return s.Enabled == nil || *s.Enabled }

// oauthDisabled reports whether OAuth handling is opted out (`oauth: false`).
func (s Server) oauthDisabled() bool { return s.OAuth.Disabled }

// ParseConfig extracts the `mcp` map from the merged opencode config into Server
// entries, skipping malformed values.
func ParseConfig(cfg map[string]any) map[string]Server {
	raw, ok := cfg["mcp"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]Server, len(raw))
	for name, v := range raw {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		s := Server{Name: name, Type: str(entry["type"])}
		switch s.Type {
		case "local":
			s.Command = strSlice(entry["command"])
			s.Environment = strMap(entry["environment"])
		case "remote":
			s.URL = str(entry["url"])
			s.Headers = strMap(entry["headers"])
			s.OAuth = parseOAuth(entry["oauth"])
		default:
			continue // unknown type
		}
		if b, ok := entry["enabled"].(bool); ok {
			s.Enabled = &b
		}
		if t, ok := entry["timeout"].(float64); ok && t > 0 {
			s.Timeout = int(t)
		}
		out[name] = s
	}
	return out
}

// ParseOAuthField decodes a JSON-decoded `oauth` value (false | object | absent)
// into an OAuthField. It is exported so the HTTP add-server payload reuses the
// exact same three-way discriminator as config parsing.
func ParseOAuthField(v any) OAuthField { return parseOAuth(v) }

// parseOAuth decodes the `oauth` config field into an OAuthField. It is the
// three-way discriminator from config/mcp.ts:48: a literal `false` disables
// OAuth, an object configures it, and anything else (absent/null) leaves it for
// auto-detection.
func parseOAuth(v any) OAuthField {
	switch o := v.(type) {
	case bool:
		// Only `false` is meaningful (opt out); `true` is not a valid value and
		// is treated as auto-detect.
		return OAuthField{Disabled: !o}
	case map[string]any:
		c := &OAuthConfig{
			ClientID:     str(o["clientId"]),
			ClientSecret: str(o["clientSecret"]),
			Scope:        str(o["scope"]),
			RedirectURI:  str(o["redirectUri"]),
		}
		if p, ok := o["callbackPort"].(float64); ok && p > 0 {
			c.CallbackPort = int(p)
		}
		return OAuthField{Config: c}
	default:
		return OAuthField{}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func strSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func strMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}
