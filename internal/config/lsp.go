package config

import (
	"encoding/json"
	"fmt"
)

// LSP config parsing. opencode's `lsp` config key is `boolean | Record<string,
// Entry>` (config/lsp.ts:39): a bare `true` enables all built-in servers with
// their defaults, `false`/absent disables LSP entirely, and an object overrides
// or extends the built-ins per server id. Each entry is itself `Disabled |
// { command, extensions?, disabled?, env?, initialization? }` (config/lsp.ts:10-19).
// This is the foundation slice (plan 03 M3-3): config decode + the built-in
// server table; the JSON-RPC client is a follow-up.

// LSPEntry is one server's config (config/lsp.ts Entry). A plain `{ disabled:
// true }` and a full custom entry are both modelled here: Disabled distinguishes
// the explicit-disable form, and the remaining fields carry a custom or override
// definition. Pointer/slice fields stay nil when absent so an override can leave
// a built-in's command/extensions untouched.
type LSPEntry struct {
	// Disabled is the parsed `disabled` flag. opencode treats `{ disabled: true }`
	// as the dedicated Disabled variant; we fold it into one struct since a custom
	// entry may also set `disabled`.
	Disabled bool
	// disabledSet records whether `disabled` was present at all, so an override
	// that omits it does not force-enable a built-in.
	disabledSet bool

	Command        []string          `json:"command,omitempty"`
	Extensions     []string          `json:"extensions,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Initialization map[string]any    `json:"initialization,omitempty"`
}

// IsDisabled reports whether this entry explicitly disables its server.
func (e LSPEntry) IsDisabled() bool { return e.disabledSet && e.Disabled }

// entryJSON mirrors the JSON shape so UnmarshalJSON can detect field presence.
type entryJSON struct {
	Command        []string          `json:"command"`
	Extensions     []string          `json:"extensions"`
	Disabled       *bool             `json:"disabled"`
	Env            map[string]string `json:"env"`
	Initialization map[string]any    `json:"initialization"`
}

// UnmarshalJSON decodes an entry, tracking whether `disabled` was present so an
// override that omits it does not flip a built-in's enabled state.
func (e *LSPEntry) UnmarshalJSON(data []byte) error {
	var raw entryJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	e.Command = raw.Command
	e.Extensions = raw.Extensions
	e.Env = raw.Env
	e.Initialization = raw.Initialization
	if raw.Disabled != nil {
		e.disabledSet = true
		e.Disabled = *raw.Disabled
	}
	return nil
}

// LSPConfig is the decoded `lsp` config: the bool|map union from config/lsp.ts:39.
// Enabled is true when the top-level value is `true` OR a (possibly empty) map is
// present — in both cases built-in servers are active. Servers holds per-server
// overrides/custom entries when the value is a map. When the value is absent or
// `false`, Enabled is false and Servers is nil (LSP off).
type LSPConfig struct {
	Enabled bool
	Servers map[string]LSPEntry
}

// ParseLSP extracts and decodes the `lsp` key from a loaded config map. A missing
// key yields the zero value (disabled). It mirrors config/lsp.ts: a bool toggles
// all built-ins, a map enables built-ins and layers per-server entries, and
// custom (non-built-in, non-disabled) entries must declare `extensions`
// (config/lsp.ts:26-37). builtinIDs is the set of recognised built-in server ids
// (passed in so config stays decoupled from the lsp package's server table).
func ParseLSP(cfg map[string]any, builtinIDs map[string]bool) (LSPConfig, error) {
	raw, ok := cfg["lsp"]
	if !ok || raw == nil {
		return LSPConfig{}, nil
	}

	// Bool form: true ⇒ all built-ins; false ⇒ disabled.
	if b, ok := raw.(bool); ok {
		return LSPConfig{Enabled: b}, nil
	}

	// Map form: re-marshal then decode into typed entries (the loaded config is
	// untyped any-maps from JSON).
	data, err := json.Marshal(raw)
	if err != nil {
		return LSPConfig{}, fmt.Errorf("encode lsp config: %w", err)
	}
	var servers map[string]LSPEntry
	if err := json.Unmarshal(data, &servers); err != nil {
		return LSPConfig{}, fmt.Errorf("decode lsp config: %w", err)
	}

	for id, entry := range servers {
		if entry.IsDisabled() {
			continue
		}
		if builtinIDs[id] {
			continue
		}
		if len(entry.Extensions) == 0 {
			return LSPConfig{}, fmt.Errorf("lsp server %q: 'extensions' array is required for custom servers", id)
		}
	}

	return LSPConfig{Enabled: true, Servers: servers}, nil
}
