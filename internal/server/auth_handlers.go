package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/opcode42/internal/credstore"
	"github.com/rotemmiz/opcode42/internal/oauth"
)

// registerProviderAuthRoutes wires the credential write/delete endpoints onto
// opencode's shared auth.json store (auth/index.ts). PUT sets a provider's
// credential, DELETE removes it; both are reflected in /provider's connected
// list.
//
// When oauthSvc is non-nil it also wires the provider OAuth surface
// (provider/auth.ts): GET /provider/auth (method listing) and the
// authorize/callback pair that drive the loopback OAuth flow (plan 13).
func registerProviderAuthRoutes(reg func(method, path string, h http.HandlerFunc), oauthSvc *oauth.Service) {
	reg(http.MethodPut, "/auth/{providerID}", putAuthHandler())
	reg(http.MethodDelete, "/auth/{providerID}", deleteAuthHandler())
	if oauthSvc != nil {
		reg(http.MethodGet, "/provider/auth", providerAuthMethodsHandler(oauthSvc))
		reg(http.MethodPost, "/provider/{providerID}/oauth/authorize", providerAuthorizeHandler(oauthSvc))
		reg(http.MethodPost, "/provider/{providerID}/oauth/callback", providerCallbackHandler(oauthSvc))
	}
}

// providerAuthMethodsHandler serves GET /provider/auth: the map of providerID →
// auth methods opencode clients render in their login picker
// (provider/auth.ts methods(); ProviderAuth.Methods).
func providerAuthMethodsHandler(svc *oauth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, svc.Methods())
	}
}

// authorizeInput is the POST /provider/:id/oauth/authorize body
// (ProviderAuth.AuthorizeInput): method index + optional prompt inputs.
type authorizeInput struct {
	Method int               `json:"method"`
	Inputs map[string]string `json:"inputs,omitempty"`
}

// callbackInput is the POST /provider/:id/oauth/callback body
// (ProviderAuth.CallbackInput): method index + optional pasted code.
type callbackInput struct {
	Method int    `json:"method"`
	Code   string `json:"code,omitempty"`
}

// providerAuthorizeHandler starts an OAuth flow and returns the Authorization
// (browser URL + method + instructions), or the legacy `null` body when the
// provider resolves without a redirect (handlers/provider.ts:85-90).
func providerAuthorizeHandler(svc *oauth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := chi.URLParam(r, "providerID")
		var in authorizeInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		auth, err := svc.Authorize(r.Context(), providerID, in.Method, in.Inputs)
		if err != nil {
			writeProviderAuthError(w, providerID, err)
			return
		}
		writeJSON(w, http.StatusOK, auth)
	}
}

// providerCallbackHandler completes an OAuth flow, returning `true` on success
// (handlers/provider.ts callback → boolean).
func providerCallbackHandler(svc *oauth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := chi.URLParam(r, "providerID")
		var in callbackInput
		if !decodeJSONBody(w, r, &in) {
			return
		}
		if err := svc.Callback(r.Context(), providerID, in.Code); err != nil {
			writeProviderAuthError(w, providerID, err)
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}

// decodeJSONBody reads a small JSON body into v, writing a 400 BadRequest in the
// ProviderAuthError shape on failure. Returns false if it already wrote a response.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeProviderAuthErrorRaw(w, "BadRequest", nil)
		return false
	}
	if err := json.Unmarshal(body, v); err != nil {
		writeProviderAuthErrorRaw(w, "BadRequest", nil)
		return false
	}
	return true
}

// writeProviderAuthError maps an oauth.Service error to opencode's
// ProviderAuthError 400 shape (handlers/provider.ts mapProviderAuthError).
func writeProviderAuthError(w http.ResponseWriter, providerID string, err error) {
	switch {
	case errors.Is(err, oauth.ErrOauthMissing):
		writeProviderAuthErrorRaw(w, "ProviderAuthOauthMissing", map[string]any{"providerID": providerID})
	case errors.Is(err, oauth.ErrOauthCodeMissing):
		writeProviderAuthErrorRaw(w, "ProviderAuthOauthCodeMissing", map[string]any{"providerID": providerID})
	case errors.Is(err, oauth.ErrOauthCallbackFailed):
		writeProviderAuthErrorRaw(w, "ProviderAuthOauthCallbackFailed", nil)
	case errors.Is(err, oauth.ErrUnknownProvider):
		// opencode has no "unknown provider" tag; a request for a provider
		// with no OAuth method is a BadRequest.
		writeProviderAuthErrorRaw(w, "BadRequest", nil)
	default:
		writeProviderAuthErrorRaw(w, "BadRequest", nil)
	}
}

// writeProviderAuthErrorRaw writes the {name, data} ProviderAuthError body with
// HTTP 400 (groups/provider.ts ProviderAuthApiError, httpApiStatus 400).
func writeProviderAuthErrorRaw(w http.ResponseWriter, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"name": name, "data": data})
}

// validAuthTypes are the credential record kinds opencode's Auth union accepts.
var validAuthTypes = map[string]bool{"api": true, "oauth": true, "wellknown": true}

// putAuthHandler stores a provider credential (Auth: api|oauth|wellknown).
func putAuthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := chi.URLParam(r, "providerID")
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "could not read body")
			return
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(body, &probe); err != nil || !validAuthTypes[probe.Type] {
			writeError(w, http.StatusBadRequest, "BadRequest",
				"auth record must have type api, oauth, or wellknown")
			return
		}
		if err := credstore.Set(providerID, json.RawMessage(body)); err != nil {
			writeError(w, http.StatusInternalServerError, "AuthStoreError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}

// deleteAuthHandler removes a provider credential.
func deleteAuthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := chi.URLParam(r, "providerID")
		if err := credstore.Remove(providerID); err != nil {
			writeError(w, http.StatusInternalServerError, "AuthStoreError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}
