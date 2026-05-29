package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/forge/internal/instance"
	"github.com/rotemmiz/forge/internal/pty"
)

// ptyConnectTokenHeader / Value gate the connect-token endpoint; a browser must
// send this header to mint a WS ticket (server/shared/pty-ticket.ts:2-3).
const (
	ptyConnectTokenHeader = "x-opencode-ticket"
	ptyConnectTokenValue  = "1"
	ptyTicketTTLSeconds   = 60
)

// registerPtyRoutes wires the HTTP PTY endpoints (the WebSocket /pty/{id}/connect
// is registered separately so it can bypass Basic auth for ticketed clients).
func registerPtyRoutes(reg func(method, path string, h http.HandlerFunc), instances *instance.Manager) {
	reg(http.MethodGet, "/pty/shells", ptyShells())
	reg(http.MethodGet, "/pty", ptyList(instances))
	reg(http.MethodPost, "/pty", ptyCreate(instances))
	reg(http.MethodGet, "/pty/{ptyID}", ptyGet(instances))
	reg(http.MethodPut, "/pty/{ptyID}", ptyUpdate(instances))
	reg(http.MethodDelete, "/pty/{ptyID}", ptyRemove(instances))
	reg(http.MethodPost, "/pty/{ptyID}/connect-token", ptyConnectToken(instances))
}

// ptyManager returns the PTY manager for the request's resolved directory.
func ptyManager(instances *instance.Manager, r *http.Request) *pty.Manager {
	return instances.Get(DirectoryFromContext(r.Context())).Pty
}

func ptyShells() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		shells := pty.ListShells()
		out := make([]map[string]any, 0, len(shells))
		for _, p := range shells {
			out = append(out, map[string]any{
				"path":       p,
				"name":       filepath.Base(p),
				"acceptable": true,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func ptyList(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, ptyManager(instances, r).List())
	}
}

func ptyCreate(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in pty.CreateInput
		_ = decodeBody(r, &in)
		info, err := ptyManager(instances, r).Create(in)
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func ptyGet(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := chi.URLParam(r, "ptyID")
		info, err := ptyManager(instances, r).Get(pid)
		if errors.Is(err, pty.ErrNotFound) {
			writePtyNotFound(w, pid)
			return
		}
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func ptyUpdate(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := chi.URLParam(r, "ptyID")
		var in pty.UpdateInput
		_ = decodeBody(r, &in)
		info, err := ptyManager(instances, r).Update(pid, in)
		if errors.Is(err, pty.ErrNotFound) {
			writePtyNotFound(w, pid)
			return
		}
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func ptyRemove(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := chi.URLParam(r, "ptyID")
		err := ptyManager(instances, r).Remove(pid)
		if errors.Is(err, pty.ErrNotFound) {
			writePtyNotFound(w, pid)
			return
		}
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}

func ptyConnectToken(instances *instance.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ptyConnectTokenHeader) != ptyConnectTokenValue {
			writePtyForbidden(w, "Invalid PTY connect token request")
			return
		}
		mgr := ptyManager(instances, r)
		pid := chi.URLParam(r, "ptyID")
		if _, err := mgr.Get(pid); errors.Is(err, pty.ErrNotFound) {
			writePtyNotFound(w, pid)
			return
		}
		token, err := mgr.IssueTicket(pid)
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ticket":     token,
			"expires_in": ptyTicketTTLSeconds,
		})
	}
}

func decodeBody(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writePtyNotFound(w http.ResponseWriter, ptyID string) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"_tag":    "PtyNotFoundError",
		"ptyID":   ptyID,
		"message": "PTY session not found: " + ptyID,
	})
}

func writePtyForbidden(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusForbidden, map[string]any{
		"_tag":    "PtyForbiddenError",
		"message": msg,
	})
}
