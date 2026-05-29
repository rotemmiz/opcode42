// Package auth implements opencode-compatible HTTP authentication.
//
// Credentials resolve from ?auth_token=base64(user:pass) (standard Base64, not
// URL-safe) first, then the Authorization: Basic <base64> header (regex is
// case-insensitive). On 401 the response carries
// WWW-Authenticate: Basic realm="Secure Area". Auth is active only when
// OPENCODE_SERVER_PASSWORD is set; username defaults to "opencode". The PTY
// connect endpoint additionally accepts a short-lived one-time ticket
// (server/auth.ts:17-34; middleware/authorization.ts:9,11,82-86,147).
//
// Implemented in plan 01 (M1/M5).
package auth
