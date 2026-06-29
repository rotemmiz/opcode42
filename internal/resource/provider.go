package resource

import (
	"os"
	"sort"

	"github.com/rotemmiz/opcode42/internal/credstore"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
)

// Provider is the wire shape served in GET /provider's `all` array (openapi
// Provider). Env, Options, and Models are always serialized (required).
type Provider struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Source  string           `json:"source"`
	Env     []string         `json:"env"`
	Options map[string]any   `json:"options"`
	Models  map[string]Model `json:"models"`
}

// Model is opencode's transformed Model wire shape (openapi Model). It is built
// from a raw catalog.Model via toWireModel — opencode's fromModelsDevModel
// (provider/provider.ts:1083-1126). Unlike the raw models.dev shape, capability
// flags live under a nested `capabilities` object and cache prices live under a
// nested `cost.cache{read,write}`.
type Model struct {
	ID           string            `json:"id"`
	ProviderID   string            `json:"providerID"`
	Name         string            `json:"name"`
	Family       string            `json:"family,omitempty"`
	API          ModelAPI          `json:"api"`
	Status       string            `json:"status"`
	Headers      map[string]string `json:"headers"`
	Options      map[string]any    `json:"options"`
	Cost         ModelCost         `json:"cost"`
	Limit        ModelLimit        `json:"limit"`
	Capabilities ModelCapabilities `json:"capabilities"`
	ReleaseDate  string            `json:"release_date"`
	Variants     map[string]any    `json:"variants"`
}

// ModelAPI is the model's API descriptor (id/url/npm). url/npm fall back to the
// provider's api/npm, then opencode's "@ai-sdk/openai-compatible" default.
type ModelAPI struct {
	ID  string `json:"id"`
	URL string `json:"url"`
	NPM string `json:"npm"`
}

// ModelCost mirrors opencode's nested cost shape: flat input/output plus a
// nested cache{read,write}.
type ModelCost struct {
	Input  float64        `json:"input"`
	Output float64        `json:"output"`
	Cache  ModelCostCache `json:"cache"`
}

// ModelCostCache is the nested cache pricing block.
type ModelCostCache struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

// ModelLimit is the model's context/output token limits.
type ModelLimit struct {
	Context int `json:"context"`
	Input   int `json:"input,omitempty"`
	Output  int `json:"output"`
}

// ModelCapabilities is opencode's nested capabilities object: capability flags
// plus per-modality input/output booleans.
type ModelCapabilities struct {
	Temperature bool               `json:"temperature"`
	Reasoning   bool               `json:"reasoning"`
	Attachment  bool               `json:"attachment"`
	ToolCall    bool               `json:"toolcall"`
	Input       ModelModalityFlags `json:"input"`
	Output      ModelModalityFlags `json:"output"`
	Interleaved bool               `json:"interleaved"`
}

// ModelModalityFlags is the per-modality boolean set under capabilities.
type ModelModalityFlags struct {
	Text  bool `json:"text"`
	Audio bool `json:"audio"`
	Image bool `json:"image"`
	Video bool `json:"video"`
	PDF   bool `json:"pdf"`
}

// ProviderList is the GET /provider response (provider/provider.ts ListResult).
type ProviderList struct {
	All       []Provider        `json:"all"`
	Default   map[string]string `json:"default"`
	Connected []string          `json:"connected"`
}

// BuildProviderList assembles the /provider response from the models.dev catalog,
// the merged config (disabled_providers/enabled_providers/provider overlay), and
// credential detection (env vars + opencode's auth.json). It mirrors opencode's
// provider list handler: `all` is every (filtered) catalog provider, `connected`
// is the subset with resolvable credentials, `default` maps each provider to its
// lowest-id model.
func BuildProviderList(cat catalog.Catalog, cfg map[string]any) ProviderList {
	disabled := stringSet(cfg["disabled_providers"])
	enabled := stringSet(cfg["enabled_providers"])
	hasEnabled := enabled != nil

	auth := credstore.Load()
	configProviders := configProviderKeys(cfg)

	result := ProviderList{All: []Provider{}, Default: map[string]string{}, Connected: []string{}}
	connectedSet := map[string]bool{}

	for id, p := range cat {
		if disabled[id] || (hasEnabled && !enabled[id]) {
			continue
		}
		source := "env"
		conn := false
		if envAny(p.Env) {
			conn = true
		}
		if _, ok := auth[id]; ok {
			conn, source = true, authSource(auth[id])
		}
		if configProviders[id] {
			conn, source = true, "config"
		}

		result.All = append(result.All, Provider{
			ID: id, Name: p.Name, Source: source,
			Env: nonNil(p.Env), Options: map[string]any{}, Models: toWireModels(p),
		})
		if def := lowestModelID(p.Models); def != "" {
			result.Default[id] = def
		}
		if conn {
			connectedSet[id] = true
		}
	}

	sort.Slice(result.All, func(i, j int) bool { return result.All[i].ID < result.All[j].ID })
	for id := range connectedSet {
		result.Connected = append(result.Connected, id)
	}
	sort.Strings(result.Connected)
	return result
}

// authSource maps a stored credential's type to the provider's source label.
func authSource(r credstore.Record) string {
	if credstore.TypeOf(r) == "api" {
		return "api"
	}
	return "env"
}

// envAny reports whether any of the named env vars is set (non-empty).
func envAny(names []string) bool {
	for _, n := range names {
		if os.Getenv(n) != "" {
			return true
		}
	}
	return false
}

// lowestModelID returns the alphabetically-first model id (opencode's default
// model per provider; provider.ts defaultModelIDs).
func lowestModelID(models map[string]catalog.Model) string {
	ids := make([]string, 0, len(models))
	for id := range models {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)
	return ids[0]
}

// toWireModels transforms a provider's raw catalog models into opencode's
// transformed Model wire shape (provider/provider.ts:1083-1126).
func toWireModels(p catalog.Provider) map[string]Model {
	out := make(map[string]Model, len(p.Models))
	for id, m := range p.Models {
		out[id] = toWireModel(p, m)
	}
	return out
}

// toWireModel maps one catalog.Model to opencode's Model wire shape. It mirrors
// fromModelsDevModel: flat capability flags become a nested `capabilities`
// object, flat cache prices become nested `cost.cache{read,write}`, and api
// url/npm fall back to the provider then "@ai-sdk/openai-compatible".
func toWireModel(p catalog.Provider, m catalog.Model) Model {
	url := p.API
	npm := p.NPM
	if npm == "" {
		npm = "@ai-sdk/openai-compatible"
	}
	status := m.Status
	if status == "" {
		status = "active"
	}

	var cost ModelCost
	if m.Cost != nil {
		cost = ModelCost{
			Input:  m.Cost.Input,
			Output: m.Cost.Output,
			Cache:  ModelCostCache{Read: m.Cost.CacheRead, Write: m.Cost.CacheWrite},
		}
	}

	return Model{
		ID:         m.ID,
		ProviderID: p.ID,
		Name:       m.Name,
		Family:     m.Family,
		API:        ModelAPI{ID: m.ID, URL: url, NPM: npm},
		Status:     status,
		Headers:    map[string]string{},
		Options:    map[string]any{},
		Cost:       cost,
		Limit: ModelLimit{
			Context: m.Limit.Context,
			Input:   m.Limit.Input,
			Output:  m.Limit.Output,
		},
		Capabilities: ModelCapabilities{
			Temperature: m.Temperature,
			Reasoning:   m.Reasoning,
			Attachment:  m.Attachment,
			ToolCall:    m.ToolCall,
			Input:       modalityFlags(m.Modalities, true),
			Output:      modalityFlags(m.Modalities, false),
			Interleaved: false,
		},
		ReleaseDate: m.ReleaseDate,
		Variants:    map[string]any{},
	}
}

// modalityFlags converts a models.dev modality list into opencode's per-modality
// boolean set. input=true selects the input list, otherwise the output list.
func modalityFlags(mod *catalog.Modalities, input bool) ModelModalityFlags {
	if mod == nil {
		return ModelModalityFlags{}
	}
	list := mod.Output
	if input {
		list = mod.Input
	}
	f := ModelModalityFlags{}
	for _, v := range list {
		switch v {
		case "text":
			f.Text = true
		case "audio":
			f.Audio = true
		case "image":
			f.Image = true
		case "video":
			f.Video = true
		case "pdf":
			f.PDF = true
		}
	}
	return f
}

// configProviderKeys returns the provider IDs declared in the config `provider`
// map (those are configured/connected regardless of env).
func configProviderKeys(cfg map[string]any) map[string]bool {
	out := map[string]bool{}
	if raw, ok := cfg["provider"].(map[string]any); ok {
		for id := range raw {
			out[id] = true
		}
	}
	return out
}

// stringSet converts a config string array (any) to a set; nil when absent.
func stringSet(v any) map[string]bool {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	set := map[string]bool{}
	for _, item := range arr {
		if s, ok := item.(string); ok {
			set[s] = true
		}
	}
	return set
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
