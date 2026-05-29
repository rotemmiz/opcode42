// Package instance holds per-directory in-memory state (config, event bus, PTY
// sessions, LSP clients) and the directory→instance cache with single-flight
// initialization.
//
// Directory resolution order matches opencode: ?directory query →
// x-opencode-directory header → process working directory
// (server/.../middleware/workspace-routing.ts:87). Cache is keyed by the
// canonical absolute path; equivalent to opencode's Deferred single-flight
// (project/instance-store.ts:105-120).
//
// Implemented in plan 01 (M3).
package instance
