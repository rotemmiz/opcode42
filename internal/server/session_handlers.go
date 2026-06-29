package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/opcode42/internal/session"
)

// registerSessionRoutes wires the M2 session CRUD endpoints. Paths use the spec
// param name {sessionID}.
func registerSessionRoutes(reg func(method, path string, h http.HandlerFunc), store *session.Store) {
	reg(http.MethodPost, "/session", createSession(store))
	reg(http.MethodGet, "/session", listSessions(store))
	reg(http.MethodGet, "/session/{sessionID}", getSession(store))
	reg(http.MethodPatch, "/session/{sessionID}", updateSession(store))
	reg(http.MethodDelete, "/session/{sessionID}", deleteSession(store))
	reg(http.MethodPost, "/session/{sessionID}/fork", forkSession(store))
	reg(http.MethodGet, "/session/{sessionID}/children", childrenSession(store))
}

func createSession(store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, err := store.Create(r.Context(), DirectoryFromContext(r.Context()))
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func listSessions(store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := store.List(r.Context())
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

func getSession(store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := chi.URLParam(r, "sessionID")
		info, err := store.Get(r.Context(), sid)
		if errors.Is(err, session.ErrNotFound) {
			writeSessionNotFound(w, sid)
			return
		}
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

// updateSessionBody is the PATCH /session/{id} request shape. It mirrors
// opencode's UpdatePayload: every field is optional and partial
// (server/.../groups/session.ts:46-54, openapi.json session.update). `permission`
// is accepted-and-ignored for now (Opcode42 has no per-session permission ruleset
// store yet); title and time.archived are persisted. Unknown top-level fields are
// IGNORED, not rejected: despite the spec's additionalProperties:false, opencode's
// runtime decode accepts and drops extra keys (verified live: PATCH {"bogus":1}
// returns 200), so this struct does NOT DisallowUnknownFields.
type updateSessionBody struct {
	// Title accepts a string or null (opencode types it Schema.optional(NullOr
	// String); a null title is a no-op). A *string leaves Title nil for both the
	// absent and explicit-null cases.
	Title *string `json:"title"`
	Time  *struct {
		// Archived is a *float64 so we accept the wire's JSON number (the openapi
		// schema types it as `number`; opencode rejects non-numbers with 400). Only
		// a number sets time.archived; a JSON null or absent leaves this nil and is
		// a no-op (verified live: PATCH {time:{archived:null}} returns 200 with
		// archived unchanged — opencode has no un-archive path).
		Archived *float64 `json:"archived"`
	} `json:"time"`
	// Permission is decoded but unused; accepting it keeps the body schema-compatible
	// with opencode's PATCH (which also takes a PermissionRuleset).
	Permission json.RawMessage `json:"permission"`
}

// updateSession handles PATCH /session/{sessionID}: a partial update of title
// and/or time.archived, returning the full refreshed session (opencode
// session.update — handlers/session.ts:180-198, openapi.json
// /session/{sessionID} patch). A missing session 404s with the standard
// NotFoundError envelope. Body handling matches opencode's observed contract:
// an empty/absent body 400s ("Expected object, got undefined"); a present body
// that fails to decode against the payload schema (wrong-typed title/archived,
// non-object JSON) 400s with {name:"BadRequest", data:{message,kind}}.
//
// KNOWN DIVERGENCE: for a syntactically MALFORMED JSON body opencode returns 500
// (UnknownError) — an uncaught decode throw on this endpoint — whereas Opcode42
// returns the more correct 400 BadRequest. Recorded in
// conformance/known-divergences.json (session-update-malformed-body-status).
func updateSession(store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := chi.URLParam(r, "sessionID")

		// opencode requires a JSON object body: an empty or absent body fails the
		// payload decode with 400 "Expected object, got undefined". Detect that
		// before decoding so an empty body is a 400, not a 200 no-op.
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			writeUpdateBadRequest(w, "Expected object, got undefined")
			return
		}

		var body updateSessionBody
		if err := json.Unmarshal(raw, &body); err != nil {
			// A present-but-undecodable body (wrong-typed field, non-object, or
			// malformed JSON) is a 400 BadRequest, matching opencode's schema-decode
			// error envelope {name:"BadRequest", data:{message,kind}}
			// (middleware/schema-error.ts:40). (opencode 500s on malformed JSON
			// specifically; see the KNOWN DIVERGENCE note above.)
			writeUpdateBadRequest(w, "invalid request body")
			return
		}

		params := session.UpdateParams{Title: body.Title}
		if body.Time != nil && body.Time.Archived != nil {
			// A finite number archives at that epoch-ms (opencode setArchived;
			// session.ts:731). null/absent already collapsed to a nil pointer above.
			ts := int64(*body.Time.Archived)
			params.Archived = &ts
		}

		info, err := store.Update(r.Context(), sid, params)
		if errors.Is(err, session.ErrNotFound) {
			writeSessionNotFound(w, sid)
			return
		}
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func deleteSession(store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := chi.URLParam(r, "sessionID")
		ok, err := store.Delete(r.Context(), sid)
		if err != nil {
			writeInternal(w, err)
			return
		}
		if !ok {
			writeSessionNotFound(w, sid)
			return
		}
		// opencode returns a bare boolean true on a successful delete.
		writeJSON(w, http.StatusOK, true)
	}
}

func forkSession(store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := chi.URLParam(r, "sessionID")
		info, err := store.Fork(r.Context(), sid)
		if errors.Is(err, session.ErrNotFound) {
			writeSessionNotFound(w, sid)
			return
		}
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, info)
	}
}

func childrenSession(store *session.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid := chi.URLParam(r, "sessionID")
		// requireSession first: a missing parent 404s before listing children
		// (session.ts:86-88).
		if _, err := store.Get(r.Context(), sid); errors.Is(err, session.ErrNotFound) {
			writeSessionNotFound(w, sid)
			return
		} else if err != nil {
			writeInternal(w, err)
			return
		}
		list, err := store.Children(r.Context(), sid)
		if err != nil {
			writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// writeSessionNotFound emits opencode's 404 envelope:
// {"data":{"message":"Session not found: <id>"},"name":"NotFoundError"}.
func writeSessionNotFound(w http.ResponseWriter, sessionID string) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"name": "NotFoundError",
		"data": map[string]any{"message": "Session not found: " + sessionID},
	})
}

// writeUpdateBadRequest emits opencode's payload-decode 400 envelope
// {name:"BadRequest", data:{message, kind:"Payload"}} (middleware/schema-error.ts:40).
func writeUpdateBadRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"name": "BadRequest",
		"data": map[string]any{"message": message, "kind": "Payload"},
	})
}

func writeInternal(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"_tag": "InternalError", "message": err.Error(),
	})
}
