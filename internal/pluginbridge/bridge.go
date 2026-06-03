// Package pluginbridge is the Go side of Forge's plugin host (plan 05). It
// spawns a Node/Bun sidecar that loads opencode-format TypeScript/JavaScript
// plugins and bridges their hooks to the daemon over JSON-RPC 2.0 on a local
// unix socket.
//
// The bridge is FLAG-GATED: it does nothing unless explicitly enabled
// (--plugin-host / FORGE_PLUGIN_HOST=1). When disabled — the default — every
// method is a cheap no-op that returns the caller's unmodified output, so the
// default daemon path and CI never spawn a subprocess. This realises plan 05's
// "deferral story": the flag-gate means no code paths change when plugins are
// off, and opencode-format plugins simply do not run.
//
// Hook failures are always non-fatal: a crashed, missing, or slow host yields
// the original output and a logged warning, matching opencode's per-hook error
// tolerance (opencode/packages/opencode/src/plugin/index.ts:288-300).
package pluginbridge

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
)

// ToolSpec is one plugin-registered tool announced by the host's `plugin.tools`
// notification (plan 05 §"Plugin-Registered Tools in the Go Registry"). The
// daemon registers a stub for each and routes invocation through tool.execute.
type ToolSpec struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolResult is the value a plugin tool returns from tool.execute.
type ToolResult struct {
	Title    string          `json:"title"`
	Output   string          `json:"output"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// Bridge is the per-instance handle the engine holds. The zero value (and a nil
// *Bridge) is a valid disabled bridge: all methods no-op. Construct an enabled
// bridge with New and start it with Start.
type Bridge struct {
	cfg Config
	log *slog.Logger

	mu        sync.RWMutex
	conn      *conn
	ready     bool
	tools     []ToolSpec
	onTools   func([]ToolSpec)
	host      *hostProcess
	startedAt bool
}

// Config configures an enabled Bridge.
type Config struct {
	// Enabled gates the entire bridge. When false, New still returns a usable
	// (no-op) Bridge so callers need no nil checks.
	Enabled bool
	// Directory is the instance working directory; forwarded to the host as
	// FORGE_DIRECTORY and used to discover {plugin,plugins}/*.{ts,js}.
	Directory string
	// ServerURL is the daemon's own base URL; the host's plugins receive an SDK
	// client pointed here (plan 05 §"Plugin Host Implementation").
	ServerURL string
	// AuthHeader is the full "Basic <b64>" value the host's SDK client sends.
	AuthHeader string
	// PluginSpecs are the configured plugin specifiers (config `plugin` array).
	PluginSpecs []string
	// Runtime overrides runtime auto-detection ("bun" | "node"); empty = auto.
	Runtime string
	// HostScript overrides the path to the plugin-host entry script. Empty uses
	// the default resolution (FORGE_PLUGIN_HOST_SCRIPT or the bundled package).
	HostScript string
	// Logger; defaults to slog.Default().
	Logger *slog.Logger
	// OnTools is invoked when the host announces its plugin tool set so the
	// daemon can register stubs. May be nil.
	OnTools func([]ToolSpec)
}

// New builds a Bridge. When cfg.Enabled is false the returned Bridge is a
// permanent no-op (Start does nothing, Trigger echoes its output).
func New(cfg Config) *Bridge {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Bridge{cfg: cfg, log: log.With("component", "pluginbridge"), onTools: cfg.OnTools}
}

// Enabled reports whether the bridge will attempt to host plugins.
func (b *Bridge) Enabled() bool { return b != nil && b.cfg.Enabled }

// Ready reports whether the host has connected and sent host.ready. Trigger
// safely returns unmodified output before the host is ready.
func (b *Bridge) Ready() bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ready
}

// Tools returns the plugin tools announced by the host (nil before ready).
func (b *Bridge) Tools() []ToolSpec {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.tools
}

// Trigger invokes a blocking, output-mutating hook. It marshals input/output,
// awaits the host's mutated output, and unmarshals it back into out. Per plan
// 05 the input is read-only and the output is mutated in place; on any failure
// (disabled, not ready, RPC error, timeout) out is left untouched and the
// caller proceeds with the unmodified value.
//
// out must be a non-nil pointer to the hook's output struct.
func (b *Bridge) Trigger(ctx context.Context, name string, input any, out any) {
	if b == nil || !b.cfg.Enabled {
		return
	}
	b.mu.RLock()
	c, ready := b.conn, b.ready
	b.mu.RUnlock()
	if c == nil || !ready {
		return
	}

	callCtx, cancel := context.WithTimeout(ctx, hookTimeout(name))
	defer cancel()

	params := map[string]any{"input": input, "output": out}
	res, err := c.call(callCtx, "plugin.trigger:"+name, params)
	if err != nil {
		b.log.Warn("plugin hook failed; using unmodified output", "hook", name, "err", err)
		return
	}
	if len(res) == 0 || string(res) == "null" {
		return
	}
	// The host returns the (possibly mutated) output object. Unmarshal it over
	// the caller's struct; on a shape mismatch keep the original output.
	if err := json.Unmarshal(res, out); err != nil {
		b.log.Warn("plugin hook returned unparseable output; using unmodified", "hook", name, "err", err)
	}
}

// Event forwards a bus event to the host as a fire-and-forget notification
// (plan 05: the `event` hook is non-blocking). It never blocks the publisher.
func (b *Bridge) Event(eventType string, properties any) {
	if b == nil || !b.cfg.Enabled {
		return
	}
	b.mu.RLock()
	c, ready := b.conn, b.ready
	b.mu.RUnlock()
	if c == nil || !ready {
		return
	}
	if err := c.notify("plugin.event", map[string]any{
		"event": map[string]any{"type": eventType, "properties": properties},
	}); err != nil {
		b.log.Debug("plugin event notify failed", "type", eventType, "err", err)
	}
}

// ExecuteTool invokes a plugin-registered tool over the host's tool.execute
// method (plan 05 §"How the Go Agent Loop Calls Hooks"). Returns an error if
// the bridge is disabled, not ready, or the host call fails.
func (b *Bridge) ExecuteTool(ctx context.Context, id string, args json.RawMessage, toolCtx map[string]any) (ToolResult, error) {
	if b == nil || !b.cfg.Enabled {
		return ToolResult{}, errClosed
	}
	b.mu.RLock()
	c, ready := b.conn, b.ready
	b.mu.RUnlock()
	if c == nil || !ready {
		return ToolResult{}, errClosed
	}
	res, err := c.call(ctx, "tool.execute", map[string]any{
		"id": id, "args": args, "context": toolCtx,
	})
	if err != nil {
		return ToolResult{}, err
	}
	var out ToolResult
	if err := json.Unmarshal(res, &out); err != nil {
		return ToolResult{}, err
	}
	return out, nil
}

// onNotify dispatches host-initiated notifications.
func (b *Bridge) onNotify(method string, params json.RawMessage) {
	switch method {
	case "host.ready":
		b.mu.Lock()
		b.ready = true
		b.mu.Unlock()
		b.log.Info("plugin host ready")
	case "plugin.tools":
		var p struct {
			Tools []ToolSpec `json:"tools"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			b.log.Warn("plugin.tools notification unparseable", "err", err)
			return
		}
		b.mu.Lock()
		b.tools = p.Tools
		cb := b.onTools
		b.mu.Unlock()
		b.log.Info("plugin tools registered", "count", len(p.Tools))
		if cb != nil {
			cb(p.Tools)
		}
	default:
		b.log.Debug("ignoring unknown host notification", "method", method)
	}
}
