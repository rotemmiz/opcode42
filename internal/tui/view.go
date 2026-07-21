package tui

// viewState holds the stream display toggles (plan 08a §D) and per-tool
// collapse state (plan 08c M7). Timestamps are omitted — the TUI store doesn't
// carry per-message time — leaving the toggles backed by data the renderer has.
type viewState struct {
	hideThinking bool // hide reasoning ("Thought …") lines
	hideTools    bool // hide tool rows (collapse all tool output)

	// collapsedTools tracks tool part IDs whose output panel is collapsed.
	// Keys are Part.ID strings; an absent entry means "expanded" (default).
	// Populated by toggleToolCollapse (plan 08c M7).
	collapsedTools map[string]bool

	// expandedThinking, when true, shows the full reasoning text instead of
	// the one-liner "Thought …" summary (plan 08c M7). Default is collapsed.
	expandedThinking bool

	// bgPulse toggles the ambient background pulse behind the splash logo
	// (plan 08e §B2). On by default for the splash; turned off when the
	// session screen is entered. Ported from opencode's bg-pulse-render.ts
	// breath field: a slow sin ramp over the logo rows, synchronized to the
	// shimmer period. No keybind is wired (the plan marked the toggle
	// optional; ctrl+x p is taken by the palette and ctrl+shift+p is
	// encoding-ambiguous across terminals) — the field is managed by the
	// splash/session screen transitions in model.go.
	bgPulse bool

	// sessionsSubtree toggles the sessions modal between flat (default) and
	// subtree rendering (plan 08e §C4). In subtree mode, children are
	// rendered indented under their parent with a └─/├─ prefix instead of
	// being filtered out (the flat main list filters parentID != nil; the
	// subtree view shows them). Toggled by `t` in the sessions modal only.
	sessionsSubtree bool
}

// toggleHint returns a one-line status string describing a toggle's new value.
func toggleHint(name string, on bool) string {
	if on {
		return name + ": on"
	}
	return name + ": off"
}

// isToolCollapsed reports whether the tool part with the given ID should show
// only its header (collapsed). Absent from the map → expanded (default).
func (v viewState) isToolCollapsed(partID string) bool {
	return v.collapsedTools[partID]
}

// toggleToolCollapse flips the collapse state for a single tool part.
func (v viewState) toggleToolCollapse(partID string) viewState {
	if v.collapsedTools == nil {
		v.collapsedTools = map[string]bool{}
	}
	v.collapsedTools[partID] = !v.collapsedTools[partID]
	return v
}
