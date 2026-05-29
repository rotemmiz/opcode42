// Package session implements project/session/message/part persistence and the
// cursor pagination that opencode clients expect.
//
// The data JSON blobs mirror opencode's MessageV2.Info / MessageV2.Part wire
// shapes so responses marshal without transformation. Cursor is
// base64url(json({id,time})); page query orders by (time_created DESC, id DESC)
// (session/message-v2.ts:563-610).
//
// Implemented in plan 01 (M2).
package session
