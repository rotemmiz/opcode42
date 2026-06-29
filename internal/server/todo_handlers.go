package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/opcode42/internal/engine/tool"
)

// registerTodoRoutes wires GET /session/:id/todo onto the shared todo store
// (written by the todowrite tool).
func registerTodoRoutes(reg func(method, path string, h http.HandlerFunc), opts Options) {
	reg(http.MethodGet, "/session/{sessionID}/todo", todoListHandler(opts))
}

// todoListHandler returns the session's todo list. opencode's Todo is
// {content, status, priority} (all required); the store keeps id internally but
// it is omitted from the wire shape, matching the spec.
func todoListHandler(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := chi.URLParam(r, "sessionID")
		if !requireSession(w, r, opts, sessionID) {
			return
		}
		items := opts.Todos.Get(sessionID)
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			out = append(out, todoWire(it))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// todoWire projects a stored item onto opencode's Todo wire shape.
func todoWire(it tool.TodoItem) map[string]any {
	return map[string]any{
		"content":  it.Content,
		"status":   it.Status,
		"priority": it.Priority,
	}
}
