package tui

// viewState holds the stream display toggles (plan 08a §D) and per-tool
// collapse state (plan 08c M7). Timestamps are omitted — the TUI store doesn't
// carry per-message time — leaving the toggles backed by data the renderer has.
type viewState struct {
	// hideThinking is the opencode full-TUI ThinkingMode toggle
	// (tui/context/thinking.ts:4 — type ThinkingMode = "show" | "hide").
	// Default true ("hide"): reasoning parts render as a 1-line header
	// while the body is hidden, preserving the reasoning's *presence* in the
	// conversation record (plan 17 §D1 — matching opencode's full TUI, NOT
	// the run mini-TUI which drops the part entirely).
	// Toggled by ctrl+x r.
	hideThinking bool

	// expandedThinking corresponds to opencode's per-ReasoningPart `expanded`
	// signal (tui/routes/session/index.tsx:1577). In hide mode the body is
	// collapsed to the 1-line header; expandedThinking flips the body open
	// (still under the hide-mode header). Toggled by ctrl+x f. In show mode
	// the body is always shown and expandedThinking is a no-op (show mode
	// renders header + body unconditionally, like opencode's
	// !inMinimal() || expanded()).
	expandedThinking bool

	hideTools bool // hide tool rows (collapse all tool output)

	// collapsedTools tracks tool part IDs whose output panel is collapsed.
	// Keys are Part.ID strings; an absent entry means "expanded" (default).
	// Populated by toggleToolCollapse (plan 08c M7).
	collapsedTools map[string]bool

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

	// images toggles inline image rendering for image file parts (plan 08e
	// §E2). Default off: most terminals can't decode Sixel or iTerm2 inline
	// escapes, and emitting those escapes to an unsupported terminal produces
	// garbage on screen. When on, renderImagePart probes the terminal
	// (TERM_PROGRAM for iTerm2/WezTerm, TERM/OPCODE42_SIXEL/Config.Sixel for
	// Sixel) and emits the matching escape; when the probe fails or the part
	// has no decodable bytes, it falls back to a placeholder glyph. Toggled
	// by ctrl+x i.
	images bool
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
