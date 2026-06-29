// Package catalog provides the model catalog and pricing Opcode42 reads from
// models.dev. It mirrors the models.dev api.json shape
// (opencode packages/core/src/models-dev.ts:44-107) and exposes capability and
// cost lookups for token/cost accounting (M4) and tool routing (M8).
//
// The live source fetches https://models.dev/api.json with an on-disk cache and
// a last-good offline fallback (Source, source.go); tests inject a checked-in
// fixture so they never touch the network.
package catalog

import "github.com/rotemmiz/opcode42/internal/engine/message"

// Catalog is the top-level models.dev map: providerID -> Provider.
type Catalog map[string]Provider

// Provider is one model provider and its models.
type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	API    string           `json:"api,omitempty"`
	NPM    string           `json:"npm,omitempty"`
	Env    []string         `json:"env,omitempty"`
	Models map[string]Model `json:"models"`
}

// Model is a single model's metadata, capabilities, pricing, and limits.
type Model struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Family      string      `json:"family,omitempty"`
	ReleaseDate string      `json:"release_date,omitempty"`
	Attachment  bool        `json:"attachment"`
	Reasoning   bool        `json:"reasoning"`
	Temperature bool        `json:"temperature"`
	ToolCall    bool        `json:"tool_call"`
	Cost        *Cost       `json:"cost,omitempty"`
	Limit       Limit       `json:"limit"`
	Modalities  *Modalities `json:"modalities,omitempty"`
	Status      string      `json:"status,omitempty"`
}

// Cost holds per-million-token prices (USD), matching models.dev.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`
}

// Limit is the model's context/output token limits.
type Limit struct {
	Context int `json:"context"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output"`
}

// Modalities lists supported input/output modalities ("text","image","pdf",…).
type Modalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// Lookup returns the model for a provider/model id pair, and whether it exists.
func (c Catalog) Lookup(providerID, modelID string) (Model, bool) {
	p, ok := c[providerID]
	if !ok {
		return Model{}, false
	}
	m, ok := p.Models[modelID]
	return m, ok
}

// CostOf computes the USD cost of a token usage block for the given model,
// mirroring opencode's accounting (session.ts:432-438): reasoning tokens bill at
// the output rate, all prices are per-million tokens. A model with no pricing
// (or an unknown model) costs 0.
func (c Catalog) CostOf(providerID, modelID string, t message.TokenCounts) float64 {
	m, ok := c.Lookup(providerID, modelID)
	if !ok || m.Cost == nil {
		return 0
	}
	return ModelCost(m, t)
}

// ModelCost computes the USD cost of a usage block against a model's pricing.
func ModelCost(m Model, t message.TokenCounts) float64 {
	if m.Cost == nil {
		return 0
	}
	const perMillion = 1_000_000.0
	return (t.Input*m.Cost.Input +
		t.Output*m.Cost.Output +
		t.Cache.Read*m.Cost.CacheRead +
		t.Cache.Write*m.Cost.CacheWrite +
		t.Reasoning*m.Cost.Output) / perMillion
}

// HasModality reports whether the model accepts the given input modality.
func (m Model) HasModality(modality string) bool {
	if m.Modalities == nil {
		return modality == "text"
	}
	for _, in := range m.Modalities.Input {
		if in == modality {
			return true
		}
	}
	return false
}
