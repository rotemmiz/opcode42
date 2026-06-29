package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/opcode42/internal/engine/permission"
	"github.com/rotemmiz/opcode42/internal/instance"
)

// registerPermissionRoutes wires the permission reply endpoint onto the
// per-directory instance's permission manager (M7).
func registerPermissionRoutes(reg func(method, path string, h http.HandlerFunc), instances *instance.Manager) {
	reg(http.MethodPost, "/permission/{requestID}/reply", permissionReplyHandler(instances))
}

// permissionReplyBody is the POST /permission/:id/reply request shape
// (openapi: reply ∈ {once,always,reject}, optional message).
type permissionReplyBody struct {
	Reply   string `json:"reply"`
	Message string `json:"message"`
}

// permissionReplyHandler resolves a pending permission request. It mirrors
// opencode's PermissionHttpApi.reply: a missing/already-answered request maps to
// a 404 PermissionNotFoundError; success returns `true`
// (server/.../handlers/permission.ts:16-36).
func permissionReplyHandler(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := chi.URLParam(r, "requestID")
		var body permissionReplyBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "invalid JSON body")
			return
		}
		switch body.Reply {
		case permission.ReplyOnce, permission.ReplyAlways, permission.ReplyReject:
		default:
			writeError(w, http.StatusBadRequest, "BadRequest",
				"reply must be one of once, always, reject")
			return
		}
		inst := instances.Get(DirectoryFromContext(r.Context()))
		if err := inst.Permissions.Reply(requestID, body.Reply); err != nil {
			writeNotFoundRequest(w, "PermissionNotFoundError", "Permission", requestID)
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}
