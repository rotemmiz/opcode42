// Package push implements the Opcode42 daemon-side push-notification relay
// (plan 13 §13.8). It lets a mobile client register an FCM device token, maps
// relevant daemon events (session idle, permission/question asked) to push
// notifications, and dispatches them to Firebase Cloud Messaging when no SSE
// client is actively connected.
//
// This is a Opcode42 known-addition: opencode has no push surface (verified —
// the only opencode "device token" hits are unrelated OAuth device-code flows).
// The endpoints are kept off the wire-compat critical path and recorded in
// conformance/known-additions.json.
//
// When no FCM service-account credential is configured the relay is a no-op:
// device registration still persists (so a client can register before the
// operator wires credentials), but no network send is attempted and the
// /push/register endpoints return 503 only when explicitly gated by the caller.
package push

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Device is a registered push target. session_filter scopes which sessions'
// events generate a push: ["all"] (default) or a list of session IDs.
type Device struct {
	DeviceID      string   `json:"device_id"`
	FCMToken      string   `json:"fcm_token"`
	Platform      string   `json:"platform"`
	SessionFilter []string `json:"session_filter"`
	RegisteredAt  int64    `json:"registered_at"`
	LastRefreshed int64    `json:"last_refreshed_at"`
}

// wants reports whether this device should receive a push for sessionID.
// An empty filter or one containing "all" matches every session.
func (d Device) wants(sessionID string) bool {
	if len(d.SessionFilter) == 0 {
		return true
	}
	for _, f := range d.SessionFilter {
		if f == "all" || f == sessionID {
			return true
		}
	}
	return false
}

// Store persists device registrations in the daemon's SQLite database. It is
// the single source of truth for which FCM tokens the dispatcher fans out to.
type Store struct {
	db *sql.DB
}

// NewStore builds a Store over the given database handle.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ErrNotFound is returned by Unregister when no device matches the ID.
var ErrNotFound = errors.New("push: device not found")

// Register inserts or updates a device registration. A repeated register for
// the same device_id refreshes the token/filter and bumps last_refreshed — this
// is the expected path when a client's FCM token rotates.
func (s *Store) Register(d Device) error {
	now := time.Now().UnixMilli()
	if d.Platform == "" {
		d.Platform = "android"
	}
	if len(d.SessionFilter) == 0 {
		d.SessionFilter = []string{"all"}
	}
	filterJSON, err := json.Marshal(d.SessionFilter)
	if err != nil {
		return fmt.Errorf("push: marshal session_filter: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO push_device (device_id, fcm_token, platform, session_filter, registered_at, last_refreshed)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id) DO UPDATE SET
			fcm_token      = excluded.fcm_token,
			platform       = excluded.platform,
			session_filter = excluded.session_filter,
			last_refreshed = excluded.last_refreshed`,
		d.DeviceID, d.FCMToken, d.Platform, string(filterJSON), now, now)
	if err != nil {
		return fmt.Errorf("push: register device: %w", err)
	}
	return nil
}

// Unregister removes a device registration. It returns ErrNotFound when no row
// matched so the HTTP handler can map it to 404.
func (s *Store) Unregister(deviceID string) error {
	res, err := s.db.Exec(`DELETE FROM push_device WHERE device_id = ?`, deviceID)
	if err != nil {
		return fmt.Errorf("push: unregister device: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("push: unregister rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// removeByToken deletes a registration whose fcm_token FCM reported as
// unregistered (UNREGISTERED / invalid). Best-effort; errors are ignored by the
// dispatcher (a stale row is harmless).
func (s *Store) removeByToken(fcmToken string) {
	_, _ = s.db.Exec(`DELETE FROM push_device WHERE fcm_token = ?`, fcmToken)
}

// List returns every registered device.
func (s *Store) List() ([]Device, error) {
	rows, err := s.db.Query(`
		SELECT device_id, fcm_token, platform, session_filter, registered_at, last_refreshed
		FROM push_device ORDER BY registered_at`)
	if err != nil {
		return nil, fmt.Errorf("push: list devices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Device
	for rows.Next() {
		var d Device
		var filterJSON string
		if err := rows.Scan(&d.DeviceID, &d.FCMToken, &d.Platform, &filterJSON, &d.RegisteredAt, &d.LastRefreshed); err != nil {
			return nil, fmt.Errorf("push: scan device: %w", err)
		}
		if err := json.Unmarshal([]byte(filterJSON), &d.SessionFilter); err != nil {
			d.SessionFilter = []string{"all"}
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("push: iterate devices: %w", err)
	}
	return out, nil
}

// targets returns the devices that want a push for sessionID.
func (s *Store) targets(sessionID string) ([]Device, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, d := range all {
		if d.wants(sessionID) {
			out = append(out, d)
		}
	}
	return out, nil
}
