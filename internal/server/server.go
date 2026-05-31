// Package server hosts the HTTP transport for the Forge daemon.
//
// It wires the opencode-compatible middleware chain (auth → directory) and the
// real endpoints implemented so far, falling back to a structured 501 for every
// other documented operation (a Forge Phase-A placeholder; opencode never
// returns 501, so this is an expected conformance divergence until the
// remaining plan-01 milestones implement the handlers).
//
// Real endpoints:
//   - GET /global/health -> {healthy:true, version} (handlers/global.ts:76)
//   - GET /doc (+ /openapi.json alias) -> embedded OpenAPI reference
//     (server/routes/instance/httpapi/server.ts:162-167)
//   - GET /config -> merged opencode-format config (M1)
//   - session CRUD (M2): see session_handlers.go
package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/forge/internal/api/spec"
	"github.com/rotemmiz/forge/internal/auth"
	"github.com/rotemmiz/forge/internal/bus"
	"github.com/rotemmiz/forge/internal/config"
	"github.com/rotemmiz/forge/internal/engine"
	"github.com/rotemmiz/forge/internal/engine/catalog"
	"github.com/rotemmiz/forge/internal/engine/message"
	"github.com/rotemmiz/forge/internal/engine/registry"
	"github.com/rotemmiz/forge/internal/engine/tool"
	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/session"
)

// Options configures the daemon HTTP handler.
type Options struct {
	// Version is reported by GET /global/health.
	Version string
	// Auth holds the resolved Basic-auth settings (pass-through when no password).
	Auth auth.Config
	// Cwd is the daemon's startup working directory, used as the directory
	// fallback when a request carries neither ?directory nor x-opencode-directory.
	Cwd string
	// Sessions is the session store backing the /session endpoints (M2).
	Sessions *session.Store
	// Instances is the per-directory instance cache backing /event (M3/M4).
	Instances *instance.Manager
	// Messages is the message/part store backing the prompt endpoints (plan 02).
	Messages *message.Store
	// Catalog is the resolved models.dev catalog (cost/capability).
	Catalog catalog.Catalog
	// Registry is the agent tool registry.
	Registry *registry.Registry
	// Todos is the per-session todo store shared with the todowrite tool,
	// backing GET /session/:id/todo.
	Todos *tool.TodoStore
	// Providers builds a streaming LLM provider for a provider/model pair.
	Providers engine.ProviderFactory
	// Global is the process-global event bus backing /global/event (M4).
	Global *bus.Global
	// BaseCtx, when set, is cancelled at the start of graceful shutdown so
	// long-lived SSE/PTY streams unblock and the server can drain (M6).
	BaseCtx context.Context
}

// New builds the daemon's HTTP handler.
func New(opts Options) (http.Handler, error) {
	r := chi.NewRouter()

	// Middleware chain: auth runs first, then directory resolution. Both are
	// pass-throughs for the global routes that do not need them.
	r.Use(opts.Auth.Middleware)
	r.Use(directoryMiddleware(opts.Cwd))

	registered := map[string]bool{}
	reg := func(method, path string, h http.HandlerFunc) {
		registered[routeKey(method, path)] = true
		r.MethodFunc(method, path, h)
	}

	// Real endpoints first; the spec-driven 501 loop skips anything already set.
	reg(http.MethodGet, "/global/health", healthHandler(opts.Version))
	reg(http.MethodGet, "/doc", docHandler())
	// /openapi.json is a Forge known-addition (alias of /doc); opencode serves
	// the spec only at /doc. Logged in conformance/known-additions.json.
	reg(http.MethodGet, "/openapi.json", docHandler())
	reg(http.MethodGet, "/config", configHandler())
	registerFindRoutes(reg)

	if opts.Sessions != nil {
		registerSessionRoutes(reg, opts.Sessions)
	}
	if opts.Messages != nil && opts.Instances != nil && opts.Providers != nil {
		registerPromptRoutes(reg, opts)
	}
	if opts.Instances != nil {
		reg(http.MethodGet, "/event", instanceEventHandler(opts.BaseCtx, opts.Instances))
		registerPtyRoutes(reg, opts.Instances)
		reg(http.MethodGet, "/pty/{ptyID}/connect", ptyConnectHandler(opts.BaseCtx, opts.Instances))
		registerPermissionRoutes(reg, opts.Instances)
		registerQuestionRoutes(reg, opts.Instances)
	}
	if opts.Todos != nil && opts.Sessions != nil {
		registerTodoRoutes(reg, opts)
	}
	if opts.Global != nil {
		reg(http.MethodGet, "/global/event", globalEventHandler(opts.BaseCtx, opts.Global))
	}

	ops, err := spec.Operations()
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		key := routeKey(op.Method, op.Path)
		if registered[key] {
			continue
		}
		registered[key] = true
		r.MethodFunc(op.Method, op.Path, notImplemented(op.Method, op.Path))
	}

	return r, nil
}

func routeKey(method, path string) string { return method + " " + path }

func healthHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"healthy": true,
			"version": version,
		})
	}
}

func docHandler() http.HandlerFunc {
	body := spec.Reference()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

// configHandler returns the merged opencode-format config for the request's
// resolved directory (config-get scenario).
func configHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, err := config.Load(DirectoryFromContext(r.Context()))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"_tag": "ConfigError", "message": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func notImplemented(method, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]any{
			"_tag":      "NotImplemented",
			"message":   "operation not implemented in Forge Phase A",
			"operation": method + " " + path,
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
