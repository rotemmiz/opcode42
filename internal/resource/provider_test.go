package resource

import (
	"testing"

	"github.com/rotemmiz/opcode42/internal/engine/catalog"
)

// TestBuildProviderListModelWireShape asserts BuildProviderList transforms raw
// models.dev catalog models into opencode's Model wire shape: a providerID, the
// nested api/capabilities objects, and a nested cost.cache{read,write} (rather
// than the flat models.dev fields). Mirrors fromModelsDevModel
// (opencode provider/provider.ts:1083-1126).
func TestBuildProviderListModelWireShape(t *testing.T) {
	list := BuildProviderList(catalog.Fixture(), map[string]any{})

	var openai *Provider
	for i := range list.All {
		if list.All[i].ID == "openai" {
			openai = &list.All[i]
			break
		}
	}
	if openai == nil {
		t.Fatal("openai provider missing from all")
	}

	m, ok := openai.Models["gpt-4o"]
	if !ok {
		t.Fatal("gpt-4o model missing")
	}

	if m.ProviderID != "openai" {
		t.Errorf("ProviderID = %q, want openai", m.ProviderID)
	}
	if m.Status != "active" {
		t.Errorf("Status = %q, want active (default)", m.Status)
	}
	if m.Headers == nil {
		t.Error("Headers must be a non-nil object")
	}
	if m.Options == nil {
		t.Error("Options must be a non-nil object")
	}
	if m.Variants == nil {
		t.Error("Variants must be a non-nil object")
	}

	// api: id is the model id; url/npm fall back to the provider.
	if m.API.ID != "gpt-4o" {
		t.Errorf("API.ID = %q, want gpt-4o", m.API.ID)
	}
	if m.API.URL != "https://api.openai.com/v1" {
		t.Errorf("API.URL = %q, want provider api", m.API.URL)
	}
	if m.API.NPM != "@ai-sdk/openai" {
		t.Errorf("API.NPM = %q, want provider npm", m.API.NPM)
	}

	// Nested cost.cache{read,write} (flat cache_read/cache_write are gone).
	if m.Cost.Input != 2.5 || m.Cost.Output != 10 {
		t.Errorf("Cost input/output = %v/%v, want 2.5/10", m.Cost.Input, m.Cost.Output)
	}
	if m.Cost.Cache.Read != 1.25 {
		t.Errorf("Cost.Cache.Read = %v, want 1.25", m.Cost.Cache.Read)
	}
	if m.Cost.Cache.Write != 0 {
		t.Errorf("Cost.Cache.Write = %v, want 0", m.Cost.Cache.Write)
	}

	// Capabilities: flat models.dev flags mapped into the nested object.
	if !m.Capabilities.Attachment {
		t.Error("Capabilities.Attachment = false, want true")
	}
	if m.Capabilities.Reasoning {
		t.Error("Capabilities.Reasoning = true, want false")
	}
	if !m.Capabilities.Temperature {
		t.Error("Capabilities.Temperature = false, want true")
	}
	if !m.Capabilities.ToolCall {
		t.Error("Capabilities.ToolCall = false, want true")
	}
	// modalities → per-modality booleans.
	if !m.Capabilities.Input.Text || !m.Capabilities.Input.Image {
		t.Errorf("Capabilities.Input = %+v, want text+image", m.Capabilities.Input)
	}
	if m.Capabilities.Input.Audio || m.Capabilities.Input.Video || m.Capabilities.Input.PDF {
		t.Errorf("Capabilities.Input = %+v, want only text+image", m.Capabilities.Input)
	}
	if !m.Capabilities.Output.Text {
		t.Error("Capabilities.Output.Text = false, want true")
	}

	if m.Limit.Context != 128000 || m.Limit.Output != 16384 {
		t.Errorf("Limit = %+v, want context=128000 output=16384", m.Limit)
	}
	if m.ReleaseDate != "2024-05-13" {
		t.Errorf("ReleaseDate = %q, want 2024-05-13", m.ReleaseDate)
	}
}

// TestToWireModelNpmFallback asserts a model with no provider npm gets opencode's
// "@ai-sdk/openai-compatible" default, and a nil cost yields a zero nested cost.
func TestToWireModelNpmFallback(t *testing.T) {
	p := catalog.Provider{ID: "custom", API: "https://example.com"}
	m := catalog.Model{ID: "x", Name: "X", Limit: catalog.Limit{Context: 100, Output: 50}}
	w := toWireModel(p, m)

	if w.API.NPM != "@ai-sdk/openai-compatible" {
		t.Errorf("API.NPM = %q, want @ai-sdk/openai-compatible", w.API.NPM)
	}
	if w.API.URL != "https://example.com" {
		t.Errorf("API.URL = %q, want provider api", w.API.URL)
	}
	if w.Cost.Input != 0 || w.Cost.Output != 0 || w.Cost.Cache.Read != 0 || w.Cost.Cache.Write != 0 {
		t.Errorf("Cost = %+v, want zero", w.Cost)
	}
	if w.Status != "active" {
		t.Errorf("Status = %q, want active", w.Status)
	}
}
