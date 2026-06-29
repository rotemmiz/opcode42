package server

import (
	"net/http"

	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/lsp"
)

// registerLSPRoutes wires GET /lsp (server status). The route returns the status
// of already-connected LSP clients for the request directory; it does NOT spawn
// servers (lazy spawn happens on file access via the lsp tool / TouchFile),
// matching opencode's lsp.status which iterates the existing clients
// (lsp/lsp.ts:315-328; httpapi/handlers/instance.ts:88-89).
func registerLSPRoutes(reg func(method, path string, h http.HandlerFunc), instances *instance.Manager) {
	reg(http.MethodGet, "/lsp", lspStatusHandler(instances))
}

// lspStatusHandler returns the LSPStatus array for the request directory's
// running LSP clients (openapi LSPStatus: id, name, root, status).
func lspStatusHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inst := instances.Get(DirectoryFromContext(r.Context()))
		status := inst.LSP.Status()
		if status == nil {
			status = []lsp.StatusItem{}
		}
		writeJSON(w, http.StatusOK, status)
	}
}
