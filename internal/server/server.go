// Package server hosts the HTTP transport for the Opcode42 daemon.
//
// It wires the opencode-compatible middleware chain (auth → directory) and the
// real endpoints implemented so far, falling back to a structured 501 for every
// other documented operation (a Opcode42 Phase-A placeholder; opencode never
// returns 501, so this is an expected conformance divergence until the
// remaining plan-01 milestones implement the handlers).
//
// Real endpoints:
//   - GET /global/health -> {healthy:true, version} (handlers/global.ts:76)
//   - GET /doc -> embedded OpenAPI reference, served verbatim
//     (server/routes/instance/httpapi/server.ts:162-167)
//   - GET /openapi.json -> Opcode42-self-emitted spec derived from the route table
//     (a known-addition; opencode has no such endpoint — plan 06 Phase 2 / M10)
//   - GET /config -> merged opencode-format config (M1)
//   - session CRUD (M2): see session_handlers.go
package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/opcode42/internal/api/spec"
	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/config"
	"github.com/rotemmiz/opcode42/internal/engine"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/message"
	"github.com/rotemmiz/opcode42/internal/engine/registry"
	"github.com/rotemmiz/opcode42/internal/engine/tool"
	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/oauth"
	"github.com/rotemmiz/opcode42/internal/push"
	"github.com/rotemmiz/opcode42/internal/session"
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
	// OAuth, when set, drives the provider OAuth surface (GET /provider/auth +
	// the authorize/callback loopback flow, plan 13). nil disables those
	// endpoints (they fall through to the 501 placeholder).
	OAuth *oauth.Service
	// Push, when set, backs the FCM device-registration endpoints (/push/*,
	// plan 13 §13.8). nil disables those endpoints (they fall through to the 501
	// placeholder). Registration persists regardless of whether FCM credentials
	// are configured; live delivery is handled by the relay (cmd/opcoded).
	Push *push.Store
}

// New builds the daemon's HTTP handler.
func New(opts Options) (http.Handler, error) {
	r := chi.NewRouter()

	// Middleware chain: auth runs first, then directory resolution. Both are
	// pass-throughs for the global routes that do not need them.
	r.Use(opts.Auth.Middleware)
	r.Use(directoryMiddleware(opts.Cwd))

	// registered tracks every (method, path) the daemon actually wires, and regOps
	// records them in registration order. They are the source for the
	// self-emitted OpenAPI spec served at /openapi.json (plan 06 Phase 2 / M10):
	// the served spec is derived from this route table — not a static blob — so
	// removing or re-pathing a handler changes the emitted spec and the drift gate
	// catches it.
	registered := map[string]bool{}
	var regOps []spec.Operation
	reg := func(method, path string, h http.HandlerFunc) {
		if !registered[routeKey(method, path)] {
			regOps = append(regOps, spec.Operation{Method: method, Path: path})
		}
		registered[routeKey(method, path)] = true
		r.MethodFunc(method, path, h)
	}

	// emittedDoc is filled once the route table is final; the /openapi.json
	// handler closes over it.
	var emittedDoc []byte
	emittedDocHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(emittedDoc)
	}

	// Real endpoints first; the spec-driven 501 loop skips anything already set.
	reg(http.MethodGet, "/global/health", healthHandler(opts.Version))
	reg(http.MethodGet, "/doc", docHandler())
	// /openapi.json is a Opcode42 known-addition (opencode serves the spec only at
	// /doc; logged in conformance/known-additions.json). Unlike /doc — which
	// serves the frozen reference verbatim — /openapi.json serves the spec Opcode42
	// self-emits from its registered route table (plan 06 Phase 2 / M10).
	reg(http.MethodGet, "/openapi.json", emittedDocHandler)
	reg(http.MethodGet, "/config", configHandler())
	registerFindRoutes(reg)
	registerResourceRoutes(reg, opts.Catalog)
	registerProviderAuthRoutes(reg, opts.OAuth)

	if opts.Sessions != nil {
		registerSessionRoutes(reg, opts.Sessions)
	}
	if opts.Messages != nil && opts.Instances != nil && opts.Providers != nil {
		registerPromptRoutes(reg, opts)
	}
	if opts.Instances != nil {
		reg(http.MethodGet, "/event", instanceEventHandler(opts.BaseCtx, opts.Instances, opts.Global))
		registerPtyRoutes(reg, opts.Instances)
		reg(http.MethodGet, "/pty/{ptyID}/connect", ptyConnectHandler(opts.BaseCtx, opts.Instances))
		registerPermissionRoutes(reg, opts.Instances)
		registerQuestionRoutes(reg, opts.Instances)
		registerMCPRoutes(reg, opts.Instances)
		registerLSPRoutes(reg, opts.Instances)
	}
	if opts.Todos != nil && opts.Sessions != nil {
		registerTodoRoutes(reg, opts)
	}
	if opts.Global != nil {
		reg(http.MethodGet, "/global/event", globalEventHandler(opts.BaseCtx, opts.Global))
	}
	if opts.Push != nil {
		registerPushRoutes(reg, opts.Push)
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
		regOps = append(regOps, spec.Operation{Method: op.Method, Path: op.Path})
		r.MethodFunc(op.Method, op.Path, notImplemented(op.Method, op.Path))
	}

	// Self-emit the served OpenAPI spec from the final route table (plan 06 Phase
	// 2 / M10). The 501-stub loop above registers every reference operation, so a
	// fully-wired daemon emits a spec whose operation set matches the frozen
	// contract; if a future change drops a reg(...) for a real handler whose path
	// is not in the reference, or adds an unspec'd route, the emitted spec — and
	// the drift gate — reflects it.
	emittedDoc, err = spec.Emit(regOps)
	if err != nil {
		return nil, err
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
			"message":   "operation not implemented in Opcode42 Phase A",
			"operation": method + " " + path,
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
