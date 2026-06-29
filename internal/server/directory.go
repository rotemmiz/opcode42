package server

import (
	"context"
	"net/http"
	"net/url"

	"github.com/rotemmiz/opcode42/internal/worktree"
)

type ctxKey int

const dirCtxKey ctxKey = iota

// directoryMiddleware resolves the per-request project directory and stores its
// canonical (symlink-resolved) form in the request context. Resolution order
// matches opencode: ?directory (or ?workspace) query → x-opencode-directory
// header → the daemon's startup working directory
// (middleware/workspace-routing.ts:23-25,87).
func directoryMiddleware(fallbackCwd string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resolved := worktree.Resolve(resolveDirParam(r, fallbackCwd))
			ctx := context.WithValue(r.Context(), dirCtxKey, resolved)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// resolveDirParam returns the raw (pre-resolution) directory for a request.
// Note: ?workspace is NOT a directory source — opencode uses it only to select
// a workspaceID (workspace-routing.ts:70-83); defaultDirectory is strictly
// ?directory → x-opencode-directory → cwd (workspace-routing.ts:86-87).
func resolveDirParam(r *http.Request, fallbackCwd string) string {
	if d := r.URL.Query().Get("directory"); d != "" {
		return d
	}
	// The SDK sends x-opencode-directory: encodeURIComponent(dir); PathUnescape
	// decodes %xx without mangling '+', and is a no-op for an already-plain path.
	if h := r.Header.Get("x-opencode-directory"); h != "" {
		if dec, err := url.PathUnescape(h); err == nil {
			return dec
		}
		return h
	}
	return fallbackCwd
}

// DirectoryFromContext returns the resolved directory stored by
// directoryMiddleware, or "" if none was set.
func DirectoryFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(dirCtxKey).(string); ok {
		return v
	}
	return ""
}
