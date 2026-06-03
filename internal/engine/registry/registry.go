// Package registry assembles the agent's tool set: it holds the built-in tools
// plus a dynamic slot (MCP/config tools land here in plan 03), filters them for
// a given model, and produces both the llm.ToolDefinition list sent to the model
// and a permission-checking processor.ToolExecutor. It also owns the system
// prompt variants and the <env> block (system.go).
//
// Mirrors opencode's ToolRegistry (packages/opencode/src/tool/registry.ts).
package registry

import (
	"sort"
	"strings"

	"github.com/rotemmiz/forge/internal/engine/llm"
	"github.com/rotemmiz/forge/internal/engine/tool"
)

// Flags gate optional tools.
type Flags struct {
	// WebSearch enables the websearch tool (provider/flag-gated in opencode).
	WebSearch bool
	// LSPTool enables the lsp tool. opencode gates it behind the
	// OPENCODE_EXPERIMENTAL_LSP_TOOL env flag (registry.ts:264,
	// runtime-flags.ts:47), so it is OFF by default; wire it from that env var to
	// match opencode's default tool surface.
	LSPTool bool
	// RepoClone, RepoOverview, Plan are reserved for later milestones/plans.
}

// Registry holds the available tools.
type Registry struct {
	builtin map[string]tool.Tool
	dynamic map[string]tool.Tool
	order   []string // stable registration order of builtins
}

// New builds a registry from the given built-in tools (registration order is
// preserved for deterministic definition listing).
func New(builtins ...tool.Tool) *Registry {
	r := &Registry{builtin: map[string]tool.Tool{}, dynamic: map[string]tool.Tool{}}
	for _, t := range builtins {
		id := t.Info().ID
		if _, dup := r.builtin[id]; !dup {
			r.order = append(r.order, id)
		}
		r.builtin[id] = t
	}
	return r
}

// RegisterDynamic adds (or replaces) a dynamic tool (MCP/config-sourced).
func (r *Registry) RegisterDynamic(t tool.Tool) {
	r.dynamic[t.Info().ID] = t
}

// Get returns a tool by id (dynamic shadows builtin), and whether it exists.
func (r *Registry) Get(id string) (tool.Tool, bool) {
	if t, ok := r.dynamic[id]; ok {
		return t, true
	}
	t, ok := r.builtin[id]
	return t, ok
}

// FilterInput selects the tool set for a model.
type FilterInput struct {
	ProviderID string
	ModelID    string
	Flags      Flags
}

// active returns the tool ids active for the model, applying model-specific
// routing (patch vs edit/write) and flag gating, in deterministic order.
func (r *Registry) active(in FilterInput) []string {
	usePatch := usePatch(in.ModelID)
	var ids []string
	for _, id := range r.order {
		switch id {
		case "patch":
			if !usePatch {
				continue
			}
		case "edit", "write":
			if usePatch {
				continue
			}
		case "websearch":
			if !in.Flags.WebSearch {
				continue
			}
		case "lsp":
			// Gated behind OPENCODE_EXPERIMENTAL_LSP_TOOL (registry.ts:264); off by
			// default to match opencode's default tool surface.
			if !in.Flags.LSPTool {
				continue
			}
		}
		ids = append(ids, id)
	}
	dyn := make([]string, 0, len(r.dynamic))
	for id := range r.dynamic {
		if _, isBuiltin := r.builtin[id]; !isBuiltin {
			dyn = append(dyn, id)
		}
	}
	sort.Strings(dyn)
	return append(ids, dyn...)
}

// Definitions returns the llm.ToolDefinition list advertised to the model.
func (r *Registry) Definitions(in FilterInput) []llm.ToolDefinition {
	ids := r.active(in)
	defs := make([]llm.ToolDefinition, 0, len(ids))
	for _, id := range ids {
		t, ok := r.Get(id)
		if !ok {
			continue
		}
		info := t.Info()
		defs = append(defs, llm.ToolDefinition{Name: info.ID, Description: info.Description, InputSchema: info.Parameters})
	}
	return defs
}

// usePatch mirrors registry.ts:316-327: GPT-class non-oss, non-gpt-4 models get
// the apply-patch tool instead of edit/write.
func usePatch(modelID string) bool {
	id := strings.ToLower(modelID)
	return strings.Contains(id, "gpt-") && !strings.Contains(id, "oss") && !strings.Contains(id, "gpt-4")
}
