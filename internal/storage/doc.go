// Package storage owns the SQLite connection, schema migrations, and PRAGMAs.
//
// Uses modernc.org/sqlite (pure Go, CGo-free) for trivial cross-compilation.
// PRAGMAs match opencode: WAL, synchronous NORMAL, busy_timeout 5000,
// cache_size -64000, foreign_keys ON (storage/db.ts:103-109).
//
// Implemented in plan 01 (M2).
package storage
