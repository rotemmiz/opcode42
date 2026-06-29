package storage

import (
	"path/filepath"
	"testing"
)

func TestOpenAppliesPragmas(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	var journal string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}

	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestMigrationsCreateTables(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	for _, table := range []string{"project", "session", "message", "part"} {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", table, err)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opcode42.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	_ = db.Close()
	// Re-open the same file: already-applied migrations must be skipped, not error.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("Open #2 (re-migrate): %v", err)
	}
	_ = db2.Close()
}

func TestUserVersionAdvances(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "opcode42.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("no migrations embedded")
	}
	want := migs[len(migs)-1].version

	var got int
	if err := db.QueryRow("PRAGMA user_version").Scan(&got); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if got != want {
		t.Errorf("user_version = %d, want %d (latest migration)", got, want)
	}
}
