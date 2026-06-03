// Package mcp connects to the user's configured MCP (Model Context Protocol)
// servers and exposes their tools to the agent. It covers config parsing,
// per-instance connection to local (stdio) and remote (StreamableHTTP with SSE
// fallback) servers, status reporting (GET /mcp), merging MCP tools into the
// agent loop (permission-gated like built-ins), and the tools/list_changed
// watcher that emits mcp.tools.changed. OAuth (needs_auth statuses) and the
// mutating /mcp endpoints are follow-ups (logged in known-divergences). Mirrors
// opencode's config/mcp.ts + mcp/index.ts.
package mcp

// Server is one configured MCP server (config/mcp.ts Local|Remote, discriminated
// on Type).
type Server struct {
	Type        string            // "local" | "remote"
	Command     []string          // local: command + args
	Environment map[string]string // local
	URL         string            // remote
	Headers     map[string]string // remote
	Enabled     *bool             // nil ⇒ enabled (opencode default)
	Timeout     int               // ms; 0 ⇒ default
}

// enabled reports whether the server should be connected (absent ⇒ enabled).
func (s Server) enabled() bool { return s.Enabled == nil || *s.Enabled }

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
		s := Server{Type: str(entry["type"])}
		switch s.Type {
		case "local":
			s.Command = strSlice(entry["command"])
			s.Environment = strMap(entry["environment"])
		case "remote":
			s.URL = str(entry["url"])
			s.Headers = strMap(entry["headers"])
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
