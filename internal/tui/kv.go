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
		Theme:                m.themeName,
		Provider:             m.model.Provider,
		Model:                m.model.Model,
		Variant:              m.model.Variant,
		ServerURL:            m.cfg.URL,
		History:              m.history,
		Stash:                m.stash,
		HideDiffTree:         m.diffTreeHidden,
		TerminalTitleEnabled: boolPtr(m.terminalTitleEnabled),
		PasteSummaryEnabled:  boolPtr(m.pasteSummaryEnabled),
	})
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
