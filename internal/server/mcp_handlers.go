package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/mcp"
)

// registerMCPRoutes wires the /mcp endpoints, matching opencode's groups/mcp.ts:
//   - GET    /mcp                          server status map
//   - POST   /mcp                          add a server at runtime
//   - POST   /mcp/{name}/connect           (re)connect a server
//   - POST   /mcp/{name}/disconnect        disconnect a server
//   - POST   /mcp/{name}/auth              start an OAuth flow → {authorizationUrl, oauthState}
//   - POST   /mcp/{name}/auth/callback     complete OAuth with the pasted code → Status
//   - POST   /mcp/{name}/auth/authenticate start + (loopback) wait → Status
//   - DELETE /mcp/{name}/auth              remove stored OAuth credentials
func registerMCPRoutes(reg func(method, path string, h http.HandlerFunc), instances *instance.Manager) {
	reg(http.MethodGet, "/mcp", mcpStatusHandler(instances))
	reg(http.MethodPost, "/mcp", mcpAddHandler(instances))
	reg(http.MethodPost, "/mcp/{name}/connect", mcpConnectHandler(instances))
	reg(http.MethodPost, "/mcp/{name}/disconnect", mcpDisconnectHandler(instances))
	reg(http.MethodPost, "/mcp/{name}/auth", mcpAuthStartHandler(instances))
	reg(http.MethodPost, "/mcp/{name}/auth/callback", mcpAuthCallbackHandler(instances))
	reg(http.MethodPost, "/mcp/{name}/auth/authenticate", mcpAuthAuthenticateHandler(instances))
	reg(http.MethodDelete, "/mcp/{name}/auth", mcpAuthRemoveHandler(instances))
}

// mcpStatusHandler returns each configured MCP server's status for the request
// directory (connecting enabled servers on first access).
func mcpStatusHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inst := instances.Get(DirectoryFromContext(r.Context()))
		status := inst.MCP.Status(r.Context())
		if status == nil {
			status = map[string]mcp.Status{}
		}
		writeJSON(w, http.StatusOK, status)
	}
}

// mcpAddPayload is the POST /mcp body (groups/mcp.ts AddPayload): a name and an
// MCP server config (local | remote union).
type mcpAddPayload struct {
	Name   string         `json:"name"`
	Config mcpConfigInput `json:"config"`
}

// mcpConfigInput is the McpLocalConfig | McpRemoteConfig union decoded from the
// add payload's `config` field (config/mcp.ts Local|Remote).
type mcpConfigInput struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Enabled     *bool             `json:"enabled,omitempty"`
	Timeout     int               `json:"timeout,omitempty"`
	OAuth       json.RawMessage   `json:"oauth,omitempty"`
}

// toServer converts the decoded payload into an mcp.Server, applying the same
// three-way `oauth` discriminator as config parsing (false | object | absent).
func (in mcpConfigInput) toServer() mcp.Server {
	s := mcp.Server{
		Type:        in.Type,
		Command:     in.Command,
		Environment: in.Environment,
		URL:         in.URL,
		Headers:     in.Headers,
		Enabled:     in.Enabled,
		Timeout:     in.Timeout,
	}
	if len(in.OAuth) > 0 {
		var v any
		if json.Unmarshal(in.OAuth, &v) == nil {
			s.OAuth = mcp.ParseOAuthField(v)
		}
	}
	return s
}

// mcpAddHandler adds a server at runtime and returns the resulting status map
// (handlers/mcp.ts add).
func mcpAddHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeMCPBadRequest(w)
			return
		}
		var p mcpAddPayload
		if err := json.Unmarshal(body, &p); err != nil || p.Name == "" || p.Config.Type == "" {
			writeMCPBadRequest(w)
			return
		}
		inst := instances.Get(DirectoryFromContext(r.Context()))
		status := inst.MCP.Add(r.Context(), p.Name, p.Config.toServer())
		if status == nil {
			status = map[string]mcp.Status{}
		}
		writeJSON(w, http.StatusOK, status)
	}
}

// mcpConnectHandler (re)connects a server, returning true (handlers/mcp.ts connect).
func mcpConnectHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		inst := instances.Get(DirectoryFromContext(r.Context()))
		if err := inst.MCP.Connect(r.Context(), name); err != nil {
			writeMCPError(w, name, err)
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}

// mcpDisconnectHandler disconnects a server, returning true (handlers/mcp.ts
// disconnect).
func mcpDisconnectHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		inst := instances.Get(DirectoryFromContext(r.Context()))
		if err := inst.MCP.Disconnect(r.Context(), name); err != nil {
			writeMCPError(w, name, err)
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}

// mcpAuthStartHandler starts an OAuth flow and returns the browser URL + state
// (handlers/mcp.ts authStart → AuthStartResponse).
func mcpAuthStartHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		inst := instances.Get(DirectoryFromContext(r.Context()))
		// opencode checks supportsOAuth first → McpUnsupportedOAuthError (400).
		if ok, err := inst.MCP.SupportsOAuth(r.Context(), name); err != nil {
			writeMCPError(w, name, err)
			return
		} else if !ok {
			writeMCPUnsupportedOAuth(w, name)
			return
		}
		authURL, state, err := inst.MCP.StartAuth(r.Context(), name)
		if err != nil {
			writeMCPError(w, name, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"authorizationUrl": authURL,
			"oauthState":       state,
		})
	}
}

// mcpAuthCallbackPayload is the POST /mcp/{name}/auth/callback body
// (groups/mcp.ts AuthCallbackPayload).
type mcpAuthCallbackPayload struct {
	Code string `json:"code"`
}

// mcpAuthCallbackHandler completes an OAuth flow with the pasted code, returning
// the server's new status (handlers/mcp.ts authCallback → Status).
func mcpAuthCallbackHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeMCPBadRequest(w)
			return
		}
		var p mcpAuthCallbackPayload
		if err := json.Unmarshal(body, &p); err != nil || p.Code == "" {
			writeMCPBadRequest(w)
			return
		}
		inst := instances.Get(DirectoryFromContext(r.Context()))
		status, err := inst.MCP.FinishAuth(r.Context(), name, p.Code)
		if err != nil {
			writeMCPError(w, name, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

// mcpAuthAuthenticateHandler starts an OAuth flow that the caller completes via
// the callback (handlers/mcp.ts authAuthenticate → Status). opencode opens a
// browser on the daemon host and blocks for the loopback callback; Opcode42's daemon
// has no interactive browser, so it starts the flow and returns needs_auth — the
// caller opens the authorization URL (from POST /mcp/{name}/auth) and posts the
// code back to /auth/callback. See the known-divergence note.
func mcpAuthAuthenticateHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		inst := instances.Get(DirectoryFromContext(r.Context()))
		if ok, err := inst.MCP.SupportsOAuth(r.Context(), name); err != nil {
			writeMCPError(w, name, err)
			return
		} else if !ok {
			writeMCPUnsupportedOAuth(w, name)
			return
		}
		if _, _, err := inst.MCP.StartAuth(r.Context(), name); err != nil {
			writeMCPError(w, name, err)
			return
		}
		writeJSON(w, http.StatusOK, mcp.Status{Status: "needs_auth"})
	}
}

// mcpAuthRemoveHandler removes a server's stored OAuth credentials (handlers/mcp.ts
// authRemove → {success:true}). It 404s an unknown server name.
func mcpAuthRemoveHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		inst := instances.Get(DirectoryFromContext(r.Context()))
		if !inst.MCP.Exists(r.Context(), name) {
			writeMCPNotFound(w, name)
			return
		}
		if err := inst.MCP.RemoveAuth(name); err != nil {
			writeError(w, http.StatusInternalServerError, "McpAuthStoreError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	}
}

// writeMCPError maps an mcp service error to opencode's wire shape:
// ErrServerNotFound → 404 McpServerNotFoundError; ErrOAuthDisabled → 400
// McpUnsupportedOAuthError; anything else → 400 BadRequest.
func writeMCPError(w http.ResponseWriter, name string, err error) {
	switch {
	case errors.Is(err, mcp.ErrServerNotFound):
		writeMCPNotFound(w, name)
	case errors.Is(err, mcp.ErrOAuthDisabled):
		writeMCPUnsupportedOAuth(w, name)
	default:
		writeMCPBadRequest(w)
	}
}

// writeMCPNotFound emits opencode's McpServerNotFoundError (404; errors.ts).
func writeMCPNotFound(w http.ResponseWriter, name string) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"_tag":    "McpServerNotFoundError",
		"name":    name,
		"message": "MCP server not found: " + name,
	})
}

// writeMCPUnsupportedOAuth emits opencode's McpUnsupportedOAuthError (400;
// groups/mcp.ts). The body is exactly {error} (no _tag — it is an Effect
// ErrorClass, not a TaggedErrorClass).
func writeMCPUnsupportedOAuth(w http.ResponseWriter, name string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error": "MCP server " + name + " does not support OAuth",
	})
}

// writeMCPBadRequest emits the effect HttpApiError.BadRequest shape ({_tag:
// "BadRequest"}) used for the add/callback payload-decode failures.
func writeMCPBadRequest(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"_tag": "BadRequest"})
}
