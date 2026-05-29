// Package server hosts the HTTP transport for the Forge daemon.
//
// This S4 skeleton wires just enough to be a conformance dual-run target:
//   - GET /global/health -> {healthy:true, version} (handlers/global.ts:76)
//   - GET /doc           -> the embedded OpenAPI reference, matching opencode's
//     spec endpoint (server/routes/instance/httpapi/server.ts:162-167). Note:
//     opencode serves the live spec at /doc, NOT /openapi.json.
//   - every other documented operation -> 501 Not Implemented (a Forge Phase-A
//     placeholder; opencode never returns 501, so this is an expected
//     conformance divergence until plan 01/02 implement the real handlers).
//
// The middleware chain, SSE writer, PTY upgrade, auth, and routing land in plan 01.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/forge/internal/api/spec"
)

// Options configures the daemon HTTP handler. Version is reported by
// GET /global/health.
type Options struct {
	Version string
}

// New builds the daemon's HTTP handler.
func New(opts Options) (http.Handler, error) {
	r := chi.NewRouter()

	// Real endpoints first; the spec-driven 501 loop skips anything already set.
	r.Get("/global/health", healthHandler(opts.Version))
	r.Get("/doc", docHandler())
	// /openapi.json is a Forge known-addition (alias of /doc); opencode serves
	// the spec only at /doc. Logged in conformance/known-additions.json.
	r.Get("/openapi.json", docHandler())

	registered := map[string]bool{
		routeKey(http.MethodGet, "/global/health"): true,
		routeKey(http.MethodGet, "/doc"):           true,
		routeKey(http.MethodGet, "/openapi.json"):  true,
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
