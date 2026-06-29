// Package credstore reads and writes opencode's provider credential store
// (~/.local/share/opencode/auth.json) — the SAME file opencode uses, so a
// credential added in Opcode42 is visible to opencode and vice-versa. Records are
// keyed by (URL-normalized) provider id; each is an api/oauth/wellknown object
// (auth/index.ts:8-34). Opcode42 stores them as raw JSON so unknown fields
// round-trip losslessly.
package credstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Record is one stored credential, kept as raw JSON to preserve its exact shape.
type Record = json.RawMessage

// mu serializes the read-modify-write of auth.json: the daemon serves concurrent
// HTTP requests, so two PUT/DELETE /auth calls would otherwise each read the old
// store and clobber the other's write (the -race detector won't catch a
// file-level RMW race).
var mu sync.Mutex

// Load returns the credential map. The OPENCODE_AUTH_CONTENT env var overrides
// the file for reads (matching opencode). A missing/unreadable store is an empty
// map, not an error.
func Load() map[string]Record {
	mu.Lock()
	defer mu.Unlock()
	return load()
}

// load reads the store without locking (callers hold mu).
func load() map[string]Record {
	var data []byte
	if content := os.Getenv("OPENCODE_AUTH_CONTENT"); content != "" {
		data = []byte(content)
	} else if b, err := os.ReadFile(storePath()); err == nil {
		data = b
	}
	out := map[string]Record{}
	if len(data) == 0 {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

// Set writes (creating/replacing) the credential for providerID, persisting the
// whole store with mode 0600. The id is URL-normalized (trailing slashes
// stripped) to match opencode's keying.
//
// Note: when OPENCODE_AUTH_CONTENT is set it overrides reads, but writes still
// target the file — matching opencode's set/remove (read all(), write the file).
func Set(providerID string, record Record) error {
	mu.Lock()
	defer mu.Unlock()
	store := load()
	store[normalize(providerID)] = record
	return write(store)
}

// Remove deletes the credential for providerID (no error if absent).
func Remove(providerID string) error {
	mu.Lock()
	defer mu.Unlock()
	store := load()
	delete(store, normalize(providerID))
	return write(store)
}

// TypeOf returns the "type" discriminator of a stored record ("api"/"oauth"/
// "wellknown"), or "" if absent/unparseable.
func TypeOf(r Record) string {
	var probe struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(r, &probe)
	return probe.Type
}

// write persists the store atomically (temp file + rename) so a crash mid-write
// can't corrupt the auth.json that opencode also reads. Callers hold mu.
func write(store map[string]Record) error {
	path := storePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".auth-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// storePath is ~/.local/share/opencode/auth.json, honoring XDG_DATA_HOME
// (Global.Path.data in opencode).
func storePath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "auth.json"
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "opencode", "auth.json")
}

func normalize(id string) string { return strings.TrimRight(id, "/") }
