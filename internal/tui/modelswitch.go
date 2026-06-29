package tui

import (
	"context"
	"encoding/json"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// modelChoice is one selectable provider/model pair in the model switcher.
type modelChoice struct {
	Provider string   // provider id (e.g. "anthropic")
	Model    string   // model id (e.g. "claude-sonnet-4")
	Variants []string // model-variant ids (e.g. "default", "thinking"); plan 08b §7
}

// label is the row text: "provider / model".
func (c modelChoice) label() string { return c.Provider + " / " + c.Model }

// providersLoadedMsg carries the flattened model catalog (or a load error).
type providersLoadedMsg struct {
	choices []modelChoice
	err     error
}

// providerResp is the GET /provider response: every provider, the per-provider
// default model, and the list of providers that have credentials (connected).
type providerResp struct {
	All       []providerWire    `json:"all"`
	Default   map[string]string `json:"default"`
	Connected []string          `json:"connected"`
}

type providerWire struct {
	ID     string               `json:"id"`
	Name   string               `json:"name"`
	Models map[string]modelWire `json:"models"`
}

type modelWire struct {
	ID       string                     `json:"id"`
	Name     string                     `json:"name"`
	Variants map[string]json.RawMessage `json:"variants,omitempty"` // variant id -> config
}

// variantIDs returns a model's variant ids, sorted (empty when it has none).
func variantIDs(m map[string]json.RawMessage) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// choices flattens the catalog to the usable set: only connected providers'
// models (the ones a prompt can actually reach), sorted by provider then model
// for a stable, navigable list.
func (r providerResp) choices() []modelChoice {
	connected := make(map[string]bool, len(r.Connected))
	for _, id := range r.Connected {
		connected[id] = true
	}
	var out []modelChoice
	for _, p := range r.All {
		if !connected[p.ID] {
			continue
		}
		for id, mw := range p.Models {
			out = append(out, modelChoice{Provider: p.ID, Model: id, Variants: variantIDs(mw.Variants)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// loadProvidersCmd fetches GET /provider and flattens it into model choices.
func loadProvidersCmd(ctx context.Context, c *opcode42client.Opcode42Client) tea.Cmd {
	return func() tea.Msg {
		var resp providerResp
		if err := c.GetJSON(ctx, "/provider", &resp); err != nil {
			return providersLoadedMsg{err: err}
		}
		return providersLoadedMsg{choices: resp.choices()}
	}
}

// modelSelIndex is the position of the active model in the choices list (0 when
// not found), so the switcher opens pre-highlighted on the current model.
func (m Model) modelSelIndex() int {
	for i, ch := range m.choices {
		if ch.Provider == m.model.Provider && ch.Model == m.model.Model {
			return i
		}
	}
	return 0
}
