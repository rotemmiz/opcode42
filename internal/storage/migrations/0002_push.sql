-- Push notification device registrations (plan 13 §13.8). A client registers
-- its FCM token so the daemon can relay server-initiated push when the app is
-- backgrounded and no SSE client is actively connected.
--
-- This is a Opcode42 known-addition: opencode has no push surface. Registrations
-- are per-daemon (single-user, matching opencode's auth model — plan 13 review
-- "Multi-user — DECIDED: single-user").
--
-- session_filter holds a JSON array: ["all"] (default) or ["ses_...", ...] to
-- scope which sessions' events generate a push for this device.

CREATE TABLE IF NOT EXISTS push_device (
    device_id      TEXT PRIMARY KEY,
    fcm_token      TEXT NOT NULL,
    platform       TEXT NOT NULL DEFAULT 'android',
    session_filter TEXT NOT NULL DEFAULT '["all"]',
    registered_at  INTEGER NOT NULL,
    last_refreshed INTEGER NOT NULL
);

-- Look up registrations by fcm_token when FCM reports an unregistered token so
-- the dispatcher can prune it.
CREATE INDEX IF NOT EXISTS push_device_token_idx ON push_device(fcm_token);
