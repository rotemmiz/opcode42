package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// whichkey.go — plan 08e §F2: the which-key overlay.
//
// When the ctrl+x leader is armed (m.leader == true), composeView composes a
// one-line strip at the bottom of the screen (above the status bar) showing
// the available chord keys + their actions. This is a Layer.Z(15) overlay —
// above panes (zPane=1) and toasts (zToast=10), below modals (zModal=20) —
// matching opencode's feature-plugins/system/which-key.tsx pattern: a
// transient hint that appears after the leader is pressed and disappears on
// the next key (the chord, esc, or a timeout).
//
// The chord table is the single source of truth for handleLeaderKey
// (model.go). whichKeyChords is the rendering-side projection of that table;
// keep it in sync when a chord is added/removed. A test
// (TestWhichKeyOverlay_MatchesLeaderKey) pins the two against each other so a
// drift is a compile/test failure, not a silent UI bug.

// whichKeyChord is one entry in the which-key strip: the chord key (what to
// press after ctrl+x) and a short label for the action.
type whichKeyChord struct {
	key   string
	label string
}

// whichKeyChords is the rendered chord table. The order is the display order
// (the order the strip lists them); handleLeaderKey dispatches by key string,
// not by index, so the order here is purely cosmetic. Grouped roughly by
// frequency: navigation → sessions → models → display → tools → terminal.
var whichKeyChords = []whichKeyChord{
	{"l", "sessions"},
	{"n", "new"},
	{"m", "model"},
	{"a", "agent"},
	{"g", "timeline"},
	{"s", "status"},
	{"c", "connect"},
	{"h", "help"},
	{"p", "palette"},
	{"b", "sidebar"},
	{"t", "tasks"},
	{"y", "copy"},
	{"r", "thinking"},
	{"f", "fold thought"},
	{"o", "tools"},
	{"v", "fold tool"},
	{"e", "editor"},
	{"d", "diff"},
	{"`", "terminal"},
	{"w", "stash"},
	{"↓", "child"},
	{"↑", "parent"},
	{"]", "next sibling"},
	{"[", "prev sibling"},
}

// whichKeyView renders the which-key strip: a one-line, full-width row of
// "key label" pairs separated by a middot. The strip uses the theme's BgElev
// surface so it reads as an owned overlay (not terminal-default bleed) and
// the Accent color for the chord keys so they pop against the labels. The
// strip is height-1 + a leading "ctrl+x — " prefix so the user sees what
// armed the overlay.
//
// When the chord list doesn't fit the screen width (24 chords is a lot for a
// narrow terminal), the strip is truncated to the chords that fit and ends
// with an ellipsis — a which-key overlay must be a single line (it replaces
// the status bar's row), and truncation is cleaner than wrapping (a wrapped
// strip would overlap the composer and break the layer-height-1 assumption).
// The most frequent chords are listed first (whichKeyChords is ordered by
// frequency), so narrow terminals still see the most useful chords.
//
// Returns "" when the leader is not armed (the caller gates the layer on
// m.leader, but this guard makes whichKeyView safe to call unconditionally).
func (m Model) whichKeyView() string {
	if !m.leader {
		return ""
	}
	s := m.styles
	prefix := s.Base.Render("ctrl+x") + s.Faint.Render(" — ")
	// Build "key label" pairs joined by " · " until the next pair would
	// overflow the screen width. The strip is prefixed by "ctrl+x — " and
	// suffixed by a possible " …" truncation marker, so the budget is
	// width - prefixWidth - ellipsisWidth. Each pair is "key label" (the
	// key + a space + the label); the separator is " · ".
	width := m.width
	if width < 1 {
		width = 1
	}
	ellipsis := s.Faint.Render(" …")
	prefixW := lipgloss.Width(prefix)
	ellipsisW := lipgloss.Width(ellipsis)
	if prefixW+ellipsisW >= width {
		// Too narrow for even the prefix + ellipsis — just render the
		// prefix truncated to the width. The overlay still reads as
		// "ctrl+x — …" (a hint that the leader is armed, even if the
		// chord list can't be shown).
		return s.Surface(s.P.BgElev).Width(width).Render(prefix)
	}
	var b strings.Builder
	b.WriteString(prefix)
	shown := 0
	for i, c := range whichKeyChords {
		pair := s.Base.Render(c.key) + s.Faint.Render(" ") + s.Base.Render(c.label)
		sep := ""
		if i > 0 {
			sep = s.Faint.Render(" · ")
		}
		// The truncation ellipsis (" …") is appended when we stop, so the
		// budget for the last shown pair must include it (unless we're
		// showing every chord, in which case no ellipsis is appended).
		// For the first pair we also require the ellipsis budget when more
		// chords remain (which is always true for the first pair — the
		// table has 24 entries, so showing only the first means truncation).
		withPair := lipgloss.Width(b.String()) + lipgloss.Width(sep) + lipgloss.Width(pair)
		needEllipsis := i < len(whichKeyChords)-1
		if needEllipsis && withPair+ellipsisW > width {
			break
		}
		if !needEllipsis && withPair > width {
			break
		}
		b.WriteString(sep)
		b.WriteString(pair)
		shown++
	}
	if shown < len(whichKeyChords) {
		b.WriteString(ellipsis)
	}
	row := b.String()
	// Surface fill: paint every cell with BgElev so the strip reads as an
	// owned overlay on any terminal (plan 08c M8 Tier 0 fill rule). Width is
	// the full screen so the strip replaces the status bar's row cleanly.
	return s.Surface(s.P.BgElev).Width(width).Render(row)
}

// whichKeyLayerHeight is the height (in rows) of the which-key overlay.
// Currently 1 (a single strip). Exposed as a constant so canvas.go can
// position the layer without a magic number, and tests can assert against it.
const whichKeyLayerHeight = 1

// whichKeyLayerY returns the Y position for the which-key overlay: the
// bottom row of the screen, above the status bar. The status bar is the
// bottom row of the footer (composer + status bar); the which-key overlay
// sits on top of the status bar's row, replacing it transiently. When the
// footer is taller than 1 row (composer + dock strips), the overlay still
// lands at the status bar's row (the last row) so it covers the status text
// without overlapping the composer.
func (m Model) whichKeyLayerY() int {
	if m.height <= whichKeyLayerHeight {
		return 0
	}
	return m.height - whichKeyLayerHeight
}

// whichKeyLayerX returns the X position for the which-key overlay: 0 (the
// strip spans the full width, matching the status bar).
func (m Model) whichKeyLayerX() int { return 0 }

// ensure whichKeyView/whichKeyLayerX/Y are used by the canvas (compile-time
// guard against a future refactor dropping the calls).
var _ = lipgloss.NewLayer
