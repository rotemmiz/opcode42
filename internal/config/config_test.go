package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// withCleanEnv points the loader at an empty config home and clears the env
// overrides so a test sees only what it sets up.
func withCleanEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, k := range []string{
		"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG_CONTENT",
		"OPENCODE_DISABLE_PROJECT_CONFIG",
	} {
		t.Setenv(k, "")
	}
	return home
}

// TestEmptyConfigDefaults locks the GET /config shape for an empty config and a
// non-project directory against the recorded opencode truth:
//
//	{"$schema":..., "agent":{}, "command":{}, "mode":{}, "plugin":[], "username":...}
func TestEmptyConfigDefaults(t *testing.T) {
	withCleanEnv(t)
	dir := t.TempDir()

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg["$schema"] != schemaURL {
		t.Errorf("$schema = %v, want %s", cfg["$schema"], schemaURL)
	}
	for _, k := range []string{"agent", "command", "mode"} {
		m, ok := cfg[k].(map[string]any)
		if !ok || len(m) != 0 {
			t.Errorf("%s = %v, want empty object", k, cfg[k])
		}
	}
	if pl, ok := cfg["plugin"].([]any); !ok || len(pl) != 0 {
		t.Errorf("plugin = %v, want empty array", cfg["plugin"])
	}
	if u, ok := cfg["username"].(string); !ok || u == "" {
		t.Errorf("username = %v, want non-empty string", cfg["username"])
	}
}

func TestDeepMergeLastWins(t *testing.T) {
	home := withCleanEnv(t)
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// opencode.json sets model; opencode.jsonc (loaded later) overrides it.
	write(t, filepath.Join(cfgDir, "opencode.json"), `{"model":"a","server":{"port":1}}`)
	write(t, filepath.Join(cfgDir, "opencode.jsonc"), `{"model":"b"}`)

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg["model"] != "b" {
		t.Errorf("model = %v, want b (last wins)", cfg["model"])
	}
	// Untouched nested object survives the merge.
	srv, _ := cfg["server"].(map[string]any)
	if srv == nil || srv["port"].(float64) != 1 {
		t.Errorf("server.port = %v, want 1 (deep-merge preserved)", cfg["server"])
	}
}

func TestInstructionsConcatDedup(t *testing.T) {
	home := withCleanEnv(t)
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cfgDir, "opencode.json"), `{"instructions":["a","b"]}`)
	write(t, filepath.Join(cfgDir, "opencode.jsonc"), `{"instructions":["b","c"]}`)

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg["instructions"].([]any)
	want := []any{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("instructions = %v, want %v (concat + dedup, order preserved)", got, want)
	}
}

func TestDeprecatedTUIKeysStripped(t *testing.T) {
	home := withCleanEnv(t)
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cfgDir, "opencode.json"), `{"theme":"x","keybinds":{},"tui":{},"model":"keep"}`)

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, k := range deprecatedKeys {
		if _, ok := cfg[k]; ok {
			t.Errorf("deprecated key %q was not stripped", k)
		}
	}
	if cfg["model"] != "keep" {
		t.Errorf("model = %v, want keep", cfg["model"])
	}
}

func TestConfigContentWins(t *testing.T) {
	home := withCleanEnv(t)
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cfgDir, "opencode.json"), `{"model":"file"}`)
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"content"}`)

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg["model"] != "content" {
		t.Errorf("model = %v, want content (OPENCODE_CONFIG_CONTENT highest priority)", cfg["model"])
	}
}

func TestServerSettings(t *testing.T) {
	home := withCleanEnv(t)
	cfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cfgDir, "opencode.json"),
		`{"server":{"port":4321,"hostname":"0.0.0.0","mdns":true,"mdnsDomain":"opcode42.local"}}`)

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := Server(cfg)
	if s.Port == nil || *s.Port != 4321 {
		t.Errorf("port = %v, want 4321", s.Port)
	}
	if s.Hostname == nil || *s.Hostname != "0.0.0.0" {
		t.Errorf("hostname = %v, want 0.0.0.0", s.Hostname)
	}
	if s.MDNS == nil || !*s.MDNS {
		t.Errorf("mdns = %v, want true", s.MDNS)
	}
	if s.MDNSDomain == nil || *s.MDNSDomain != "opcode42.local" {
		t.Errorf("mdnsDomain = %v, want opcode42.local", s.MDNSDomain)
	}
}

func TestServerSettingsEmpty(t *testing.T) {
	withCleanEnv(t)
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := Server(cfg)
	if s.Port != nil || s.Hostname != nil || s.MDNS != nil {
		t.Errorf("empty config should yield nil settings, got %+v", s)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
