package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// TUI file-config resolution (plan 08f H13 / G.15).
//
// Reads opencode-compatible TUI settings from OPENCODE_TUI_CONFIG (or the
// usual project/global discovery paths), then overlays the fields Opcode42
// already understands onto the Model. Unknown keys are ignored so a shared
// opencode.json / tui.json "just works" (CLAUDE.md ecosystem-compat).

// tuiFileConfig is the JSON shape accepted from tui.json / opencode.json's
// tui section (subset of opencode packages/tui/src/config Info).
type tuiFileConfig struct {
	Theme              string             `json:"theme,omitempty"`
	Keybinds           map[string]string  `json:"keybinds,omitempty"`
	LeaderTimeout      *int               `json:"leader_timeout,omitempty"`
	ScrollSpeed        *float64           `json:"scroll_speed,omitempty"`
	ScrollAcceleration *scrollAccelConfig `json:"scroll_acceleration,omitempty"`
	DiffStyle          string             `json:"diff_style,omitempty"` // "auto" | "stacked"
	Mouse              *bool              `json:"mouse,omitempty"`
	Prompt             *promptFileConfig  `json:"prompt,omitempty"`
	Attention          json.RawMessage    `json:"attention,omitempty"` // accepted, unused
}

type scrollAccelConfig struct {
	Enabled bool `json:"enabled"`
}

type promptFileConfig struct {
	MaxHeight *int            `json:"max_height,omitempty"`
	MaxWidth  json.RawMessage `json:"max_width,omitempty"` // int or "auto"
}

// defaultScrollStep is the lines moved per wheel notch / scroll key when no
// scroll_speed is configured (historical Opcode42 const).
const defaultScrollStep = 3

// resolveTUIConfigPaths returns config files to load, lowest-precedence first.
// Override (OPENCODE_TUI_CONFIG) short-circuits discovery.
func resolveTUIConfigPaths(override, cwd string) []string {
	if p := strings.TrimSpace(override); p != "" {
		return []string{p}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	var out []string
	// Global first, then project — later files win on merge.
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, ".config", "opencode", "tui.json"))
	}
	if cwd != "" {
		out = append(out,
			filepath.Join(cwd, "tui.json"),
			filepath.Join(cwd, "opencode.json"),
			filepath.Join(cwd, ".opencode", "config.json"),
			filepath.Join(cwd, ".opencode", "tui.json"),
		)
	}
	return out
}

// loadTUIConfigFile reads one JSON/JSONC-ish file. Missing files return an
// empty config (not an error). Nested {"tui":{...}} is flattened like
// opencode's normalize().
func loadTUIConfigFile(path string) (tuiFileConfig, error) {
	var empty tuiFileConfig
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, nil
		}
		return empty, err
	}
	return parseTUIConfigJSON(b)
}

// parseTUIConfigJSON decodes a tui config document (test seam).
func parseTUIConfigJSON(b []byte) (tuiFileConfig, error) {
	var empty tuiFileConfig
	// Strip // line comments (JSONC-lite) so shared opencode configs parse.
	cleaned := stripJSONCLineComments(b)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(cleaned, &raw); err != nil {
		return empty, err
	}
	// Flatten nested "tui" key (legacy opencode.json shape).
	if nested, ok := raw["tui"]; ok {
		var inner map[string]json.RawMessage
		if json.Unmarshal(nested, &inner) == nil {
			delete(raw, "tui")
			for k, v := range inner {
				if _, exists := raw[k]; !exists {
					raw[k] = v
				}
			}
		}
	}
	reb, err := json.Marshal(raw)
	if err != nil {
		return empty, err
	}
	var cfg tuiFileConfig
	if err := json.Unmarshal(reb, &cfg); err != nil {
		return empty, err
	}
	return cfg, nil
}

// mergeTUIFileConfig overlays b onto a (non-zero / non-nil fields in b win).
func mergeTUIFileConfig(a, b tuiFileConfig) tuiFileConfig {
	if b.Theme != "" {
		a.Theme = b.Theme
	}
	if b.Keybinds != nil {
		if a.Keybinds == nil {
			a.Keybinds = map[string]string{}
		}
		for k, v := range b.Keybinds {
			a.Keybinds[k] = v
		}
	}
	if b.LeaderTimeout != nil {
		a.LeaderTimeout = b.LeaderTimeout
	}
	if b.ScrollSpeed != nil {
		a.ScrollSpeed = b.ScrollSpeed
	}
	if b.ScrollAcceleration != nil {
		a.ScrollAcceleration = b.ScrollAcceleration
	}
	if b.DiffStyle != "" {
		a.DiffStyle = b.DiffStyle
	}
	if b.Mouse != nil {
		a.Mouse = b.Mouse
	}
	if b.Prompt != nil {
		a.Prompt = b.Prompt
	}
	if len(b.Attention) > 0 {
		a.Attention = b.Attention
	}
	return a
}

// loadMergedTUIConfig loads and merges all discovered config files.
func loadMergedTUIConfig(override, cwd string) tuiFileConfig {
	var acc tuiFileConfig
	for _, p := range resolveTUIConfigPaths(override, cwd) {
		cfg, err := loadTUIConfigFile(p)
		if err != nil {
			continue
		}
		// Skip empty/missing files (load returns zero-value + nil on NotExist).
		if cfg.Theme == "" && cfg.Keybinds == nil && cfg.LeaderTimeout == nil &&
			cfg.ScrollSpeed == nil && cfg.ScrollAcceleration == nil &&
			cfg.DiffStyle == "" && cfg.Mouse == nil && cfg.Prompt == nil &&
			len(cfg.Attention) == 0 {
			continue
		}
		acc = mergeTUIFileConfig(acc, cfg)
	}
	return acc
}

// scrollStepFromSpeed maps opencode scroll_speed onto an integer line step.
// speed ≤0 falls back to defaultScrollStep; otherwise round(default * speed)
// with a minimum of 1.
func scrollStepFromSpeed(speed float64) int {
	if speed <= 0 {
		return defaultScrollStep
	}
	step := int(speed*float64(defaultScrollStep) + 0.5)
	if step < 1 {
		return 1
	}
	return step
}

// stripJSONCLineComments removes // comments outside of strings so we can
// tolerate the JSONC opencode configs often ship.
func stripJSONCLineComments(b []byte) []byte {
	var out strings.Builder
	out.Grow(len(b))
	inStr := false
	esc := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out.WriteByte(c)
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(b) && b[i+1] == '/' {
			for i < len(b) && b[i] != '\n' {
				i++
			}
			if i < len(b) {
				out.WriteByte('\n')
			}
			continue
		}
		out.WriteByte(c)
	}
	return []byte(out.String())
}
