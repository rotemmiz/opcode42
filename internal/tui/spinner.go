package tui

// spinner.go — Plan 08c M9: gradient-scanner spinner + animTick animation infra.
//
// Design goals (plan 08c §3a / Tier 3 intro):
//  1. scannerFrame(label, frame, palette) — pure, deterministic per-rune coloring
//     that sweeps a bright head + fading trail across the label text.  Port of
//     opencode's spinner.ts getScannerState + createKnightRiderTrail math.
//  2. animTick infrastructure — a single low-frequency tea.Tick (~100ms) gated
//     to fire ONLY while something is animating.  Nothing schedules at idle;
//     no wasted renders, no battery drain.
//  3. animating() predicate — true when the current session has a running/pending
//     tool or an in-progress text part (streaming assistant turn).

// ── Scanner math (ported from spinner.ts) ─────────────────────────────────────
//
// opencode's spinner sweeps a bright "head" across the label characters using a
// frame counter.  The head position is (frame % len) for a forward scan.  Each
// character's color is determined by its distance from the head:
//
//   dist = headPos − charIdx          (for forward sweep)
//   dist == 0  → head: full Accent brightness
//   dist == 1  → bloom: slightly brighter (alpha 0.9, brightnessFactor 1.15)
//   dist 2..N  → exponential-alpha fall-off: alpha = 0.65^(dist-1)
//   dist < 0 or > trailLen → inactive: FgDim
//
// The trail length is trailLen = 6 (matching spinner.ts deriveTrailColors default).
//
// Color representation: we work entirely in 24-bit hex strings that lipgloss
// accepts directly as lipgloss.Color values.  The lerp helper interpolates two
// hex colors so we can express the trail gradient purely in the theme tokens.

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// animPeriod is the tick interval for the animation loop.  100ms ≈ 10fps is fast
// enough for a smooth-looking sweep without burning CPU at idle.  opencode's
// opentui renders at a variable rate but the spinner period (4600ms / ~46chars
// = ~100ms/char) maps naturally onto this interval.
const animPeriod = 100 * time.Millisecond

// trailLen is the number of character positions behind the sweep head that carry
// a colored trail.  Matches spinner.ts deriveTrailColors default of 6 steps.
const trailLen = 6

// animTickMsg is the message type emitted by the animation ticker.
// Each tick increments m.animFrame and reschedules if still animating.
type animTickMsg struct{}

// animTickCmd returns a single tea.Tick that fires after animPeriod.
func animTickCmd() tea.Cmd {
	return tea.Tick(animPeriod, func(time.Time) tea.Msg { return animTickMsg{} })
}

// ── animating predicate ────────────────────────────────────────────────────────

// animating reports whether any animation is currently in progress.  The tick
// reschedules only when this returns true, so at idle no further ticks are queued
// and the render loop is never woken unnecessarily.
//
// "Animating" means one of:
//   - The splash screen is visible (ScreenSplash) — the logo shimmer sweep runs
//     continuously while on the home screen (plan 08c M10).  The tick cadence is
//     the same 100ms as the spinner; the shimmer advances slowly (46 frames per
//     full sweep) so CPU impact is negligible.
//     (tool spinner in toolRow / scannerFrame spinner label).
//   - At least one live (non-expired) toast is in the queue (plan 08c M11).
//     The animTick drives the toast TTL countdown; toastsLive() stays true until
//     all toasts expire, at which point the tick self-stops and the queue drains.
//
// Idle-safety: on ScreenSession with no running tools and no live toasts this
// returns false, stopping the tick.  The splash case only fires while screen ==
// ScreenSplash; switching to a session screen with no active tools/toasts
// immediately goes idle again.
//
// Rationale: the SSE stream delivers message.part.updated events continuously
// during a turn; as long as there is a running tool the assistant is active.
// Once all tools reach "completed"/"error" the animation stops naturally on the
// next animTickMsg check.
func (m Model) animating() bool {
	// Logo shimmer: keep ticking while the splash/home screen is visible so the
	// shimmer sweep advances each frame.  ScreenSplash is the initial state and
	// re-entered when the user closes all sessions (modal.go, model.go).
	if m.screen == ScreenSplash {
		return true
	}
	// Live toasts need the tick to count down their TTL (plan 08c M11).
	if m.toastsLive() {
		return true
	}
	sid := m.cfg.SessionID
	if sid == "" {
		return false
	}
	for _, msg := range m.store.messages[sid] {
		if msg.Role != "assistant" {
			continue
		}
		for _, p := range m.store.parts[msg.ID] {
			if p.Type == "tool" {
				var st toolState
				if decode(p.State, &st) {
					if st.Status == "running" || st.Status == "pending" {
						return true
					}
				}
			}
		}
	}
	return false
}

// maybeKickAnim returns an animTickCmd if the model is currently animating.
// Callers that receive events that may start a new turn should batch this with
// other returned cmds.  Bubble Tea queues ticks as goroutines; calling this
// multiple times is safe — only one extra tick is live at a time since each
// tick only reschedules when animating() is still true.
func (m *Model) maybeKickAnim() tea.Cmd {
	if m.animating() {
		return animTickCmd()
	}
	return nil
}

// ── Scanner color math ─────────────────────────────────────────────────────────

// hexToRGB parses a "#rrggbb" or "#rgb" hex color string to (r,g,b) in [0,1].
// Returns (0,0,0) on any parse error — safe default that degrades to black.
func hexToRGB(hex string) (r, g, b float64) {
	h := strings.TrimPrefix(hex, "#")
	switch len(h) {
	case 6:
		var ri, gi, bi int
		if _, err := fmt.Sscanf(h, "%02x%02x%02x", &ri, &gi, &bi); err != nil {
			return 0, 0, 0
		}
		return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255
	case 3:
		var ri, gi, bi int
		if _, err := fmt.Sscanf(h, "%1x%1x%1x", &ri, &gi, &bi); err != nil {
			return 0, 0, 0
		}
		return float64(ri*17) / 255, float64(gi*17) / 255, float64(bi*17) / 255
	default:
		return 0, 0, 0
	}
}

// lerpHex linearly interpolates between hex colors a and b by t ∈ [0.0, 1.0].
// Returns a "#rrggbb" hex string suitable for lipgloss.Color.
//
// Math: each channel is lerped independently in linear RGB space:
//
//	result.channel = a.channel*(1-t) + b.channel*t
//
// We clamp each channel to [0,1] before re-encoding to avoid out-of-range values
// from rounding.
func lerpHex(a, b string, t float64) lipgloss.Color {
	ar, ag, ab := hexToRGB(a)
	br, bg, bb := hexToRGB(b)
	t = clamp01(t)
	rr := ar + (br-ar)*t
	rg := ag + (bg-ag)*t
	rb := ab + (bb-ab)*t
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
		int(math.Round(clamp01(rr)*255)),
		int(math.Round(clamp01(rg)*255)),
		int(math.Round(clamp01(rb)*255)),
	))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// trailColor returns the lipgloss.Color for a character at the given trail
// distance from the sweep head.  Mirrors spinner.ts deriveTrailColors:
//
//	dist == 0: head — full Accent color (bright, alpha=1.0)
//	dist == 1: bloom — Accent + 15% brightness boost, slight dim (alpha ≈ 0.9),
//	           approximated by lerping Accent → white by 0.15.
//	dist 2..N: exponential alpha fall-off lerped toward FgDim.
//	           alpha(dist) = 0.65^(dist−1); we convert to a lerp t from Accent→FgDim.
//	dist > trailLen or dist < 0: inactive — FgDim.
//
// The bloom at dist==1 exceeds the head brightness in the TS implementation (it
// uses brightnessFactor=1.15 on the RGBA).  In 24-bit hex we approximate by
// lerping the accent color toward white (#ffffff) by 0.15, which achieves the
// same "slight glare" effect without needing float RGB arithmetic at call time.
func trailColor(dist int, p theme.Palette) lipgloss.Color {
	accent := string(p.Accent())
	dim := string(p.FgDim)
	if dist < 0 || dist >= trailLen {
		return p.FgDim // inactive
	}
	switch dist {
	case 0:
		return p.Accent() // head: full brightness
	case 1:
		// Bloom: Accent → white by 0.15 (brightness boost approximation).
		return lerpHex(accent, "#ffffff", 0.15)
	default:
		// Exponential alpha fall-off: alpha = 0.65^(dist-1).
		// We map alpha ∈ [0,1] to a lerp between Accent and FgDim:
		//   t=0 → Accent (full brightness), t=1 → FgDim (no brightness).
		// At dist=2: alpha = 0.65^1 = 0.65 → t = 1-0.65 = 0.35
		// At dist=3: alpha = 0.65^2 ≈ 0.42 → t ≈ 0.58
		// At dist=4: alpha ≈ 0.27 → t ≈ 0.73
		// At dist=5: alpha ≈ 0.18 → t ≈ 0.82
		alpha := math.Pow(0.65, float64(dist-1))
		t := 1 - alpha
		return lerpHex(accent, dim, t)
	}
}

// scannerFrame renders label with per-rune fg colors implementing the
// gradient-scanner effect.  It is pure and deterministic given (label, frame, p)
// — no global state, safe to call from View() on any goroutine.
//
// Algorithm (ported from spinner.ts getScannerState + createKnightRiderTrail):
//  1. Compute head position = frame % len(runes) for a simple forward sweep.
//     (We use the "forward" direction from getScannerState — simplest, most legible.)
//  2. For each rune at charIdx: dist = headPos − charIdx (trail is to the left).
//  3. Look up trailColor(dist, palette) and wrap the rune's string in a lipgloss
//     Foreground style.
//  4. Join all per-rune strings and return — the host line's background style
//     (set by the caller) provides the bg fill so no transparent cells escape.
//
// Note: lipgloss.NewStyle() allocations per rune are cheap for a label of
// typical length (8–30 chars); the spinner is not on a hot rendering path.
func scannerFrame(label string, frame int, p theme.Palette) string {
	runes := []rune(label)
	n := len(runes)
	if n == 0 {
		return ""
	}
	headPos := frame % n

	var sb strings.Builder
	for i, r := range runes {
		dist := headPos - i
		col := trailColor(dist, p)
		sb.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(r)))
	}
	return sb.String()
}
