package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/tailscale/hujson"

	"github.com/rotemmiz/opcode42/internal/worktree"
)

// schemaURL is opencode's config $schema, seeded as a default so editor
// completion works (config.ts:447-453). Opcode42 injects it on read rather than
// writing files.
const schemaURL = "https://opencode.ai/config.json"

// deprecatedKeys are the legacy TUI keys silently stripped from any loaded
// config layer (config.ts:64-71).
var deprecatedKeys = []string{"theme", "keybinds", "tui"}

// Load assembles the opencode-format config for a resolved directory, in
// opencode's layer order (config.ts:443-476,596-674), deep-merging each layer
// (last wins) with the one exception that "instructions" arrays are
// concatenated and de-duplicated (config.ts:55-61). The result is then
// post-processed to match what opencode's GET /config returns: $schema,
// username, and empty agent/command/mode/plugin defaults are injected.
//
// dir is the symlink-resolved request directory (see internal/worktree.Resolve).
func Load(dir string) (map[string]any, error) {
	result := map[string]any{}

	// 1. Global layer: ~/.config/opencode/{config.json,opencode.json,opencode.jsonc}.
	cfgHome := configHome()
	for _, name := range []string{"config.json", "opencode.json", "opencode.jsonc"} {
		if err := mergeFile(result, filepath.Join(cfgHome, name)); err != nil {
			return nil, err
		}
	}

	// 2. OPENCODE_CONFIG: a single named file loaded after the global layer.
	if p := os.Getenv("OPENCODE_CONFIG"); p != "" {
		if err := mergeFile(result, p); err != nil {
			return nil, err
		}
	}

	// 3. Project layer: opencode.json[c] walked from the worktree down to the
	//    directory (parent-most first so the child wins), unless disabled.
	if os.Getenv("OPENCODE_DISABLE_PROJECT_CONFIG") == "" && dir != "" {
		for _, p := range projectConfigFiles(dir) {
			if err := mergeFile(result, p); err != nil {
				return nil, err
			}
		}
	}

	// 4. OPENCODE_CONFIG_DIR: an extra config directory.
	if d := os.Getenv("OPENCODE_CONFIG_DIR"); d != "" {
		for _, name := range []string{"opencode.json", "opencode.jsonc"} {
			if err := mergeFile(result, filepath.Join(d, name)); err != nil {
				return nil, err
			}
		}
	}

	// 5. OPENCODE_CONFIG_CONTENT: a raw JSONC string, highest priority.
	if content := os.Getenv("OPENCODE_CONFIG_CONTENT"); content != "" {
		layer, err := parseJSONC([]byte(content), "OPENCODE_CONFIG_CONTENT")
		if err != nil {
			return nil, err
		}
		mergeInto(result, layer)
	}

	postProcess(result)
	return result, nil
}

// configHome returns ~/.config/opencode, honoring XDG_CONFIG_HOME. opencode uses
// this path on every platform (not the OS-specific app-data dir).
func configHome() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "opencode")
	}
	return filepath.Join(home, ".config", "opencode")
}

// projectConfigFiles lists opencode.json / opencode.jsonc from the worktree root
// down to dir, parent-most first (so the deepest, most-specific file is merged
// last and wins). Mirrors config/paths.ts:10-21.
func projectConfigFiles(dir string) []string {
	root := worktree.Root(dir)

	// Collect ancestors from dir up to (and including) root.
	var chain []string
	cur := dir
	for {
		chain = append(chain, cur)
		if cur == root || cur == "/" {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	// Reverse to parent-most first, then list json before jsonc at each level.
	var files []string
	for i := len(chain) - 1; i >= 0; i-- {
		files = append(files,
			filepath.Join(chain[i], "opencode.json"),
			filepath.Join(chain[i], "opencode.jsonc"),
		)
	}
	return files
}

// mergeFile loads one config file (a no-op when it is missing) and merges it
// into result. Missing files are not an error.
func mergeFile(result map[string]any, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config %s: %w", path, err)
	}
	layer, err := parseJSONC(data, path)
	if err != nil {
		return err
	}
	mergeInto(result, layer)
	return nil
}

// parseJSONC standardizes a JSONC document (strips comments and trailing commas)
// and decodes it into a map, dropping deprecated TUI keys.
func parseJSONC(data []byte, source string) (map[string]any, error) {
	std, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", source, err)
	}
	var layer map[string]any
	if err := json.Unmarshal(std, &layer); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", source, err)
	}
	for _, k := range deprecatedKeys {
		delete(layer, k)
	}
	return layer, nil
}

// mergeInto deep-merges src into dst (last wins), concatenating and
// de-duplicating the "instructions" array (config.ts:55-61).
func mergeInto(dst, src map[string]any) {
	for k, sv := range src {
		if k == "instructions" {
			dst[k] = concatInstructions(dst[k], sv)
			continue
		}
		if sm, ok := sv.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				mergeInto(dm, sm)
				continue
			}
		}
		dst[k] = sv
	}
}

// concatInstructions appends src instruction strings to dst's, preserving order
// and dropping exact duplicates.
func concatInstructions(dst, src any) []any {
	seen := map[string]bool{}
	var out []any
	add := func(list any) {
		items, ok := list.([]any)
		if !ok {
			return
		}
		for _, it := range items {
			s, ok := it.(string)
			if ok && seen[s] {
				continue
			}
			if ok {
				seen[s] = true
			}
			out = append(out, it)
		}
	}
	add(dst)
	add(src)
	return out
}

// postProcess injects the fields opencode's config service adds before
// returning GET /config: $schema default, the OS username (config.ts:765-770),
// and empty agent/command/mode/plugin containers.
func postProcess(cfg map[string]any) {
	if _, ok := cfg["$schema"]; !ok {
		cfg["$schema"] = schemaURL
	}
	if _, ok := cfg["username"]; !ok {
		cfg["username"] = systemUsername()
	}
	for _, k := range []string{"agent", "command", "mode"} {
		if _, ok := cfg[k]; !ok {
			cfg[k] = map[string]any{}
		}
	}
	if _, ok := cfg["plugin"]; !ok {
		cfg["plugin"] = []any{}
	}
}

// ServerSettings is the typed view of the config "server" block
// (config.ts ServerConfig). Pointer fields distinguish "unset" from a zero
// value so the caller can apply config-over-flag precedence.
type ServerSettings struct {
	Port       *int     `json:"port,omitempty"`
	Hostname   *string  `json:"hostname,omitempty"`
	MDNS       *bool    `json:"mdns,omitempty"`
	MDNSDomain *string  `json:"mdnsDomain,omitempty"`
	CORS       []string `json:"cors,omitempty"`
}

// Server extracts the server settings from a loaded config map.
func Server(cfg map[string]any) ServerSettings {
	var out ServerSettings
	raw, ok := cfg["server"]
	if !ok {
		return out
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func systemUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "user"
}
