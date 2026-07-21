package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// paintBackground composites the rendered frame onto a full-screen canvas painted
// with the theme background, so every cell is owned by the theme — mirroring
// opencode's opentui per-cell compositor.
//
// lipgloss cannot do this on its own: it resets all SGR (including background)
// after every styled span, so the gaps between spans, the trailing cells, and the
// empty lines fall back to the terminal-default background. On a themed dark TUI
// over a black terminal that reads as a black void with text-colored boxes (the
// "background stays black, text has its own background" bug). We fix it at the
// frame level, after everything else has been composed:
//
//   - Truncate every row to `width` (ANSI-aware). No line may wrap: a wrapped line
//     renders as two terminal rows, which pushed the total height past `height` and
//     made the *terminal* scroll the whole frame — footer and sidebar included —
//     instead of the in-app viewport (the "footer/sidebar scroll with the content"
//     and "not scrollable" bugs).
//   - Clamp/pad to exactly `height` rows.
//   - Emit the base background at the start of every row and re-emit it after each
//     reset, then pad the row to full width under that background. Re-emitting only
//     colors cells that have no explicit background of their own — gaps, padding,
//     empty lines. Tightly-packed colored spans (selection bars, diff tints, the
//     sidebar panel, toasts) set their own background and are unaffected: no bare
//     cell follows their reset before the next SGR, so the re-emit paints nothing.
func paintBackground(frame string, width, height int, bg theme.Color) string {
	if width <= 0 || height <= 0 {
		return frame
	}
	bgSeq := bgSGR(bg)
	const reset = "\x1b[0m"
	lines := strings.Split(frame, "\n")
	var b strings.Builder
	for i := 0; i < height; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		// No wrapping: a row wider than the screen is truncated, not folded.
		line = ansi.Truncate(line, width, "")
		// Re-establish the base bg after every inner reset so post-reset gap cells
		// are painted rather than left at the terminal default.
		if bgSeq != "" && strings.Contains(line, reset) {
			line = strings.ReplaceAll(line, reset, reset+bgSeq)
		}
		pad := width - ansi.StringWidth(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(bgSeq)
		b.WriteString(line)
		if pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString(reset)
	}
	return b.String()
}

// bgSGR returns the SGR escape that sets c as the background color, honoring the
// active lipgloss color profile (so truecolor degrades to 256/ANSI when the
// terminal can't do better, and yields "" when color is unavailable). It works by
// rendering a single space with the background and isolating the opening sequence.
func bgSGR(c theme.Color) string {
	rendered := lipgloss.NewStyle().Background(c).Render(" ")
	if i := strings.IndexByte(rendered, ' '); i > 0 {
		return rendered[:i]
	}
	return ""
}
