package tui

// logo.go — Plan 08c M10: block-pixel "opcode42" wordmark + left→right brightness
// shimmer sweep, animated via M9's animTick infrastructure.
//
// Design reference: opencode's component/logo.tsx ShimmerConfig (period 4600ms,
// rings 2, sweepFraction 1, coreWidth 1.2, coreAmp 1.9, softWidth 10, tail 5).
// The idle sweep in logo.tsx computes a "head" position that travels from 0 to
// reach (max corner distance from origin) over one period; each column's glyph is
// colored by how far the head is from that column.  We port the key numerics:
//
//   period        = 4600 ms → at animPeriod (100 ms/tick) = 46 ticks/cycle
//   sweepFraction = 1.0     → head covers the full span each period
//   coreAmp       = 1.9     → bright core flash strength
//   softAmp       = 1.6     → softer glow shoulder
//   softWidth     = 10      → width of the glow shoulder in column units
//   tail          = 5       → tail width behind head (trailing edge in columns)
//   tailAmp       = 0.64    → tail brightness
//   haloAmp       = 0.16    → halo brightness (faint leading fringe)
//   breathBase    = 0.04    → always-on ambient floor
//   ambientAmp    = 0.36    → ambient bell centered at mid-sweep
//
// We work entirely in column space (x = 0..logoWidth-1) rather than pixel space.
// "head" position = (phase * totalSpan) where phase = (frame*100ms / 4600ms) % 1.
// Brightness at column x = coreGaussian(dist) + softGaussian(dist) + tail(dist)
// mapped to a lerp from FgDim (base) → Fg (body) → Accent (peak) → white (flash).

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// ── Block-pixel glyph matrix ────────────────────────────────────────────────
//
// Each letter is 5 rows × 3 columns with 1-column gaps between letters.
// Cells drawn with '█' (full block U+2588) or ' ' (space).
//
// Visual layout (5 rows, '#' = filled, '.' = empty):
//
//	f      o       r      g       e
//	##.    ###    ##.    ###    ###
//	#..    # #    #..    # #    #..
//	##.    # #    #..    # #    ##.
//	#..    # #    #..    ###    #..
//	#..    ###    #..    ..#    ###
//
// The 'r' has no right-side pixel in the traditional 3-wide block font;
// 'g' has a descender on col 2 of its row 4.
// Total width: 5 × 3 + 4 gaps = 19 columns.
//
// Column map (letters separated by ' '):
//
//	cols 0-2: f   cols 3: gap   cols 4-6: o
//	cols 7: gap   cols 8-10: r  cols 11: gap  cols 12-14: g
//	cols 15: gap  cols 16-18: e
var opcode42Glyph = [5]string{
	"██  ███ ██  ███ ███", // row 0 (top)
	"█   █ █ █   █ █ █  ", // row 1
	"██  █ █ █   █ █ ██ ", // row 2 (mid)
	"█   █ █ █   ███ █  ", // row 3
	"█   ███ █     █ ███", // row 4 (bottom)
}

// logoWidth is the number of columns in a opcode42Glyph row.
// All rows are padded to this width in logoFrame.
const logoWidth = 19

// ── Shimmer math ─────────────────────────────────────────────────────────────

// shimmerPeriodFrames is how many animTick frames make one full shimmer period,
// matching opencode's ShimmerConfig.period = 4600ms at animPeriod = 100ms/tick.
//
//	4600ms / 100ms = 46 frames per cycle
const shimmerPeriodFrames = 46

// shimmerBrightness returns a brightness value in [0,1] for column x given the
// current animation frame.  Ported from opencode logo.tsx buildIdleState and the
// idle() function — specifically the head/eased/glow/peak path for a single ring.
//
// Port of logo.tsx numerics (shimmerConfig):
//
//	period=4600ms → shimmerPeriodFrames=46 ticks
//	sweepFraction=1 → phase covers [0,1) each cycle
//	softWidth=10, softAmp=1.6  → wide glow shoulder (Gaussian σ≈3.3 cols)
//	coreWidth=1.2, coreAmp=1.9 → tight bright peak (σ≈0.4 cols)
//	tail=5, tailAmp=0.64       → trailing ramp-down behind the head
//	haloWidth=4.3, haloOffset=0.6, haloAmp=0.16 → faint leading fringe
//	breathBase=0.04, ambientAmp=0.36, ambientCenter=0.5, ambientWidth=0.34
//	rings=2 (each ring offset by 1/rings = 0.5 in phase)
//
// The two rings are averaged so the result is smooth across the full cycle.
func shimmerBrightness(x int, frame int) float64 {
	const rings = 2
	var total float64
	for i := range rings {
		offset := float64(i) / float64(rings)
		// cyclePhase: fractional position within the current cycle (0..1).
		cyclePhase := math.Mod(float64(frame%shimmerPeriodFrames)/float64(shimmerPeriodFrames)+offset, 1.0)

		// sweepFraction = 1, so the ring is active across the full cycle.
		phase := cyclePhase // phase ∈ [0,1)

		// Envelope: eased sin — matches logo.tsx eased = env*env*(3-2*env)
		// where env = sin(phase*π).
		env := math.Sin(phase * math.Pi)
		eased := env * env * (3 - 2*env)

		// Head position: sweep from 0 to reach over the phase.
		// reach = (logoWidth - 1 + tail) so the head visibly exits the right edge.
		reach := float64(logoWidth-1) + 5.0 // tail=5 exit margin
		headX := phase * reach

		dx := float64(x) - headX // positive = ahead of head, negative = behind

		// Soft shoulder (Gaussian, exponent 1.6 for softer edges, logo.tsx comment):
		//   "Use shallower exponent (1.6 vs 2) for softer edges on the Gaussians"
		σs := 10.0 / 3.0 // softWidth / 3
		soft := math.Exp(-math.Pow(math.Abs(dx/σs), 1.6)) * 1.6

		// Core flash (tighter Gaussian, exponent 1.8):
		σc := 1.2 / 3.0 // coreWidth / 3
		core := math.Exp(-math.Pow(math.Abs(dx/σc), 1.8)) * 1.9

		// Tail: behind the head (dx < 0), quadratic ramp over tailWidth columns.
		// logo.tsx: tail = dx < 0 && dx > -tailRange ? (1+dx/tailRange)^2.6 : 0
		// We use tailRange = tail * 2.6 (logo.tsx constant).
		var tail float64
		tailRange := 5.0 * 2.6 // tail * 2.6 from logo.tsx
		if dx < 0 && dx > -tailRange {
			t := 1 + dx/tailRange
			tail = math.Pow(clamp01(t), 2.6) * 0.64
		}

		// Halo: faint fringe just behind the head (haloOffset = 0.6 means the halo
		// center trails the head by 0.6 columns — logo.tsx: haloDelta = delta + haloOffset).
		haloDelta := dx + 0.6 // positive haloOffset → halo trails behind head
		halo := math.Exp(-math.Pow(math.Abs(haloDelta/4.3), 1.6)) * 0.16

		// Per-ring contribution: (soft*softAmp + tail*tailAmp)*eased for glow
		// plus (core + halo)*eased for peak (logo.tsx idle() decomposition).
		total += (soft + core + tail + halo) * eased
	}

	// Average over rings + ambient bell + breath floor (logo.tsx idle() ambient path).
	const ambientAmp = 0.36
	const ambientCenter = 0.5
	const ambientWidth = 0.34
	const breathBase = 0.04

	phase := math.Mod(float64(frame%shimmerPeriodFrames)/float64(shimmerPeriodFrames), 1.0)
	d := (phase - ambientCenter) / ambientWidth
	var ambient float64
	if math.Abs(d) < 1 {
		ambient = (1 - d*d) * (1 - d*d) * ambientAmp
	}

	brightness := total/float64(rings) + ambient + breathBase
	return clamp01(brightness)
}

// columnColor maps a shimmerBrightness value to a lipgloss.Color by lerping
// through a three-zone brightness ramp:
//
//	zone 1 [0.0, 0.3): dim base      — lerp FgDim → Fg
//	zone 2 [0.3, 0.7): body glow     — lerp Fg    → Accent
//	zone 3 [0.7, 1.0]: peak flash    — lerp Accent → white
//
// This mirrors opencode logo.tsx's layered approach: the halo/tail pulls toward
// theme.primary (our Accent), while the bright core stays near-white — achieved
// in logo.tsx by tinting ink → primary first, then tinting toward PEAK (white).
func columnColor(x int, frame int, p theme.Palette) lipgloss.Color {
	b := shimmerBrightness(x, frame)
	dim := string(p.FgDim)
	fg := string(p.Fg)
	acc := string(p.Accent())
	white := "#ffffff"

	switch {
	case b < 0.3:
		t := b / 0.3
		return lerpHex(dim, fg, t)
	case b < 0.7:
		t := (b - 0.3) / 0.4
		return lerpHex(fg, acc, t)
	default:
		t := (b - 0.7) / 0.3
		return lerpHex(acc, white, t)
	}
}

// logoFrame renders the 5-row block-pixel "opcode42" wordmark with a left→right
// brightness shimmer sweep, returning one string per row.  It is pure and
// deterministic given (frame, palette) — no global state, safe to call from
// View() on any goroutine.
//
// Each returned string contains per-column styled runes.  The caller (viewSplash)
// must wrap each row in a full-width Background-painted style to prevent
// transparent-cell bleed on light terminals (plan 08c Tier 0 invariant).
//
// frame is m.animFrame (monotonic, incremented per animTickMsg at 10fps).
func logoFrame(frame int, p theme.Palette) []string {
	rows := make([]string, len(opcode42Glyph))
	for row, line := range opcode42Glyph {
		// Pad row to logoWidth so all rows have identical column count.
		padded := line
		for len([]rune(padded)) < logoWidth {
			padded += " "
		}
		runes := []rune(padded)

		var sb strings.Builder
		for x, r := range runes {
			col := columnColor(x, frame, p)
			sb.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(r)))
		}
		rows[row] = sb.String()
	}
	return rows
}
