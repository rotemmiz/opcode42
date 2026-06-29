package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rotemmiz/opcode42/internal/push"
)

// registerPushRoutes wires the Opcode42 push-notification relay endpoints
// (plan 13 §13.8). These are a Opcode42 known-addition — opencode has no push
// surface — recorded in conformance/known-additions.json and kept off the
// wire-compat critical path.
//
//	POST   /push/register             register/refresh an FCM device token
//	DELETE /push/register/{deviceID}  unregister a device
//	GET    /push/register             list registered devices
//
// Registration persists regardless of whether FCM credentials are configured,
// so a client can register before the operator wires the service account; live
// delivery is gated by the relay's no-op mode (push.Relay.Enabled).
func registerPushRoutes(reg func(method, path string, h http.HandlerFunc), store *push.Store) {
	reg(http.MethodPost, "/push/register", pushRegisterHandler(store))
	reg(http.MethodDelete, "/push/register/{deviceID}", pushUnregisterHandler(store))
	reg(http.MethodGet, "/push/register", pushListHandler(store))
}

// pushRegisterInput is the POST /push/register body (plan 13 §"Device
// Registration").
type pushRegisterInput struct {
	DeviceID      string   `json:"device_id"`
	FCMToken      string   `json:"fcm_token"`
	Platform      string   `json:"platform,omitempty"`
	SessionFilter []string `json:"session_filter,omitempty"`
}

func pushRegisterHandler(store *push.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "could not read body")
			return
		}
		var in pushRegisterInput
		if err := json.Unmarshal(body, &in); err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", "invalid JSON body")
			return
		}
		if in.DeviceID == "" || in.FCMToken == "" {
			writeError(w, http.StatusBadRequest, "BadRequest", "device_id and fcm_token are required")
			return
		}
		if err := store.Register(push.Device{
			DeviceID:      in.DeviceID,
			FCMToken:      in.FCMToken,
			Platform:      in.Platform,
			SessionFilter: in.SessionFilter,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "PushStoreError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}

func pushUnregisterHandler(store *push.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceID := chi.URLParam(r, "deviceID")
		if err := store.Unregister(deviceID); err != nil {
			if errors.Is(err, push.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NotFound", "device not registered")
				return
			}
			writeError(w, http.StatusInternalServerError, "PushStoreError", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, true)
	}
}

func pushListHandler(store *push.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		devices, err := store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PushStoreError", err.Error())
			return
		}
		if devices == nil {
			devices = []push.Device{}
		}
		writeJSON(w, http.StatusOK, devices)
	}
}
