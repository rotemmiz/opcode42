package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	// modernc.org/sqlite registers the pure-Go "sqlite" database/sql driver.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// pragmas mirror opencode's connection settings (storage/db.ts:103-109): WAL
// journaling, NORMAL sync, a 5s busy timeout, a 64MB page cache, and enforced
// foreign keys.
var pragmas = []string{
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
	"busy_timeout(5000)",
	"cache_size(-64000)",
	"foreign_keys(ON)",
}

// DB wraps the SQLite handle. Writes are serialized through a single connection
// (SetMaxOpenConns(1)) for Phase A simplicity and to avoid SQLITE_BUSY; the WAL
// concurrent-reader optimization is revisited in plan 11.
type DB struct {
	*sql.DB
}

// Open opens (creating if needed) the SQLite database at path, applies the
// opencode-compatible PRAGMAs, and runs the embedded schema migrations. The
// special path ":memory:" yields a shared in-memory database (useful in tests).
func Open(path string) (*DB, error) {
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create db dir: %w", err)
			}
		}
	}
	dsn := buildDSN(path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	db := &DB{sqlDB}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// DefaultPath returns the daemon's database path: $OPCODE_DB if set, else
// $XDG_DATA_HOME/opcode42/opcode42.db (falling back to ~/.local/share/opcode42).
func DefaultPath() string {
	if p := os.Getenv("OPCODE_DB"); p != "" {
		return p
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(dataHome, "opcode42", "opcode42.db")
}

func buildDSN(path string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	if path == ":memory:" {
		// Shared cache keeps a single logical DB across the connection pool.
		q.Set("cache", "shared")
		return "file::memory:?" + q.Encode()
	}
	return "file:" + path + "?" + q.Encode()
}

// migrate applies embedded migrations in version order, gating each on SQLite's
// built-in PRAGMA user_version so every migration runs exactly once across
// restarts. Migration files are named "NNNN_name.sql"; the leading integer is
// the target version. This is a dependency-free versioned migrator (no
// golang-migrate) that, unlike bare "CREATE IF NOT EXISTS", can carry future
// ALTER/data migrations correctly.
func (db *DB) migrate() error {
	migs, err := loadMigrations()
	if err != nil {
		return err
	}

	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	for _, m := range migs {
		if m.version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", m.version, m.name, err)
		}
		// PRAGMA does not accept bind parameters; the version is our own int.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("set user_version %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}
	return nil
}

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads and parses the embedded "NNNN_name.sql" files, sorted by
// version. It errors on a malformed name or duplicate version so a packaging
// mistake fails loudly rather than silently skipping a migration.
func loadMigrations() ([]migration, error) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var migs []migration
	seen := map[int]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("migration %q is not named NNNN_name.sql", name)
		}
		version, err := strconv.Atoi(prefix)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("migration %q has a non-positive-integer prefix", name)
		}
		if seen[version] {
			return nil, fmt.Errorf("duplicate migration version %d", version)
		}
		seen[version] = true
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		migs = append(migs, migration{version: version, name: name, sql: string(body)})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}
