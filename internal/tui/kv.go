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
	Theme        string   `json:"theme,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	Model        string   `json:"model,omitempty"`
	History      []string `json:"history,omitempty"`
	HideDiffTree bool     `json:"hideDiffTree,omitempty"` // diff reviewer file-tree pane off
}

func kvPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "forge", "tui-kv.json")
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
		Theme:        m.themeName,
		Provider:     m.model.Provider,
		Model:        m.model.Model,
		History:      m.history,
		HideDiffTree: m.diffTreeHidden,
	})
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
