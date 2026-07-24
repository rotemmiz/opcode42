package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Local persistence (plan 08a §H): a tiny JSON KV under the user config dir,
// remembering the chosen theme + model and a rolling prompt history across runs.
// Best-effort — any error leaves the TUI working with defaults.

const historyMax = 100

type kvData struct {
	Theme                string   `json:"theme,omitempty"`
	Provider             string   `json:"provider,omitempty"`
	Model                string   `json:"model,omitempty"`
	Variant              string   `json:"variant,omitempty"`    // active model variant (plan 08b §7)
	ServerURL            string   `json:"server_url,omitempty"` // pinned daemon URL (plan 08e §D3)
	History              []string `json:"history,omitempty"`
	Stash                []string `json:"stash,omitempty"`                  // parked prompt drafts (plan 08b §6)
	HideDiffTree         bool     `json:"hideDiffTree,omitempty"`           // diff reviewer file-tree pane off
	TerminalTitleEnabled *bool    `json:"terminal_title_enabled,omitempty"` // nil = default on (08f H6)
	PasteSummaryEnabled  *bool    `json:"paste_summary_enabled,omitempty"`  // nil = default on (08f H3)
	// Osc52WriteEnabled gates the OSC 52 clipboard-write escape (08f H11 /
	// plan G.13). nil = environment-based default (on locally, off over
	// SSH) rather than a fixed default, unlike the other toggles above.
	Osc52WriteEnabled *bool `json:"osc52_write_enabled,omitempty"`

	// Display toggles (08f H7 / plan G.11). All nil = default on, matching
	// opencode's kv.get(key, true) defaults (packages/tui/src/app.tsx).
	AnimationsEnabled       *bool `json:"animations_enabled,omitempty"`
	FileContextEnabled      *bool `json:"file_context_enabled,omitempty"`             // no render consumer yet — toggle+persist only (documented future work)
	SessionDirFilterEnabled *bool `json:"session_directory_filter_enabled,omitempty"` // no render consumer yet — toggle+persist only (documented future work)

	// ThemeModeLock pins the dark/light mode across launches (08f H7 /
	// plan G.12), mirroring opencode's theme_mode_lock kv key. nil = unlocked
	// (auto-detect the terminal background on every launch); non-nil = locked,
	// with the value carrying the locked mode itself (true = dark).
	ThemeModeLock *bool `json:"theme_mode_lock,omitempty"`
}

func kvPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "opcode42", "tui-kv.json")
}

func loadKV() kvData {
	var d kvData
	p := kvPath()
	if p == "" {
		return d
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return d
	}
	_ = json.Unmarshal(b, &d)
	return d
}

func saveKV(d kvData) {
	p := kvPath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o644)
}

// persist writes the current theme/model/history to the KV (best-effort).
// No-op unless persistence was enabled via Restore (keeps tests hermetic).
func (m Model) persist() {
	if !m.persistEnabled {
		return
	}
	saveKV(kvData{
		Theme:                   m.themeName,
		Provider:                m.model.Provider,
		Model:                   m.model.Model,
		Variant:                 m.model.Variant,
		ServerURL:               m.cfg.URL,
		History:                 m.history,
		Stash:                   m.stash,
		HideDiffTree:            m.diffTreeHidden,
		TerminalTitleEnabled:    boolPtr(m.terminalTitleEnabled),
		PasteSummaryEnabled:     boolPtr(m.pasteSummaryEnabled),
		Osc52WriteEnabled:       boolPtr(m.osc52Enabled),
		AnimationsEnabled:       boolPtr(!m.noAnim),
		FileContextEnabled:      boolPtr(m.fileContextEnabled),
		SessionDirFilterEnabled: boolPtr(m.sessionDirFilterEnabled),
		ThemeModeLock:           themeModeLockValue(m),
	})
}

// themeModeLockValue returns the persisted theme_mode_lock value: nil when
// unlocked (auto-detect on every launch), else a pointer to the locked
// dark/light mode (08f H7 / plan G.12).
func themeModeLockValue(m Model) *bool {
	if !m.themeModeLocked {
		return nil
	}
	return boolPtr(m.termDark)
}

func boolPtr(v bool) *bool { return &v }

// kvTitleEnabled returns the persisted terminal-title preference (default on).
func kvTitleEnabled(kv kvData) bool {
	if kv.TerminalTitleEnabled == nil {
		return true
	}
	return *kv.TerminalTitleEnabled
}

// kvPasteSummaryEnabled returns the smart-paste preference (default on).
func kvPasteSummaryEnabled(kv kvData) bool {
	if kv.PasteSummaryEnabled == nil {
		return true
	}
	return *kv.PasteSummaryEnabled
}

// kvOsc52WriteEnabled returns the persisted OSC 52 clipboard-write
// preference (plan 08f H11 / G.13). Unlike the other kv* toggles above, a
// nil value here does not fall back to a fixed default — it falls back to
// the environment-based default (on locally, off over SSH), since the
// escape leaks to the wrong terminal over an SSH hop.
func kvOsc52WriteEnabled(kv kvData) bool {
	if kv.Osc52WriteEnabled == nil {
		return defaultOsc52WriteEnabled()
	}
	return *kv.Osc52WriteEnabled
}

// kvAnimationsEnabled returns the animations preference (default on). The
// CLI --no-anim flag still forces animations off regardless of this value —
// callers only consult it when the flag was not passed (08f H7 / plan G.11).
func kvAnimationsEnabled(kv kvData) bool {
	if kv.AnimationsEnabled == nil {
		return true
	}
	return *kv.AnimationsEnabled
}

// kvFileContextEnabled returns the file-context preference (default on).
// No renderer consumes this yet — the KV round-trip is future work (08f H7).
func kvFileContextEnabled(kv kvData) bool {
	if kv.FileContextEnabled == nil {
		return true
	}
	return *kv.FileContextEnabled
}

// kvSessionDirFilterEnabled returns the session-directory-filter preference
// (default on). No renderer consumes this yet — future work (08f H7).
func kvSessionDirFilterEnabled(kv kvData) bool {
	if kv.SessionDirFilterEnabled == nil {
		return true
	}
	return *kv.SessionDirFilterEnabled
}

// kvThemeModeLocked reports whether the theme mode is pinned and, if so, the
// locked dark/light value (08f H7 / plan G.12). ok is false when unlocked
// (kv.ThemeModeLock == nil), matching the other kv* helpers' "default"
// convention but surfacing the two-value tri-state explicitly since there is
// no meaningful bool default for a lock's target mode.
func kvThemeModeLocked(kv kvData) (dark bool, ok bool) {
	if kv.ThemeModeLock == nil {
		return false, false
	}
	return *kv.ThemeModeLock, true
}

// persistServerURL pins a daemon URL to the KV (server_url key, plan 08e §D3)
// so subsequent runs skip the connect overlay. Best-effort — a write failure
// leaves the TUI working; the overlay just re-opens next time. No-op unless
// persistence was enabled via Restore (keeps tests hermetic).
func (m Model) persistServerURL(url string) {
	if !m.persistEnabled || url == "" {
		return
	}
	kv := loadKV()
	kv.ServerURL = url
	saveKV(kv)
}

// pushHistory appends a submitted prompt (dedup-adjacent, capped) and resets the
// browse cursor.
func (m Model) pushHistory(text string) Model {
	if text == "" {
		return m
	}
	if n := len(m.history); n > 0 && m.history[n-1] == text {
		m.histIdx = -1
		return m
	}
	m.history = append(m.history, text)
	if len(m.history) > historyMax {
		m.history = m.history[len(m.history)-historyMax:]
	}
	m.histIdx = -1
	return m
}
