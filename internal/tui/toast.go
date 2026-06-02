package tui

// toast.go — Plan 08c M11: transient bottom-right overlay notices.
//
// Design (plan 08c §3c / opencode ui/toast.tsx):
//   - A queue of {text, kind, born} rendered bottom-right with the elevated
//     background, expiring via M9's animTick (which ticks at animPeriod = 100ms).
//   - toastKind ∈ {toastInfo, toastSuccess, toastError} mapped to theme palette
//     colors (info→Cyan/Accent, success→Green, error→Red).
//   - pushToast enqueues up to toastMaxQueue items (oldest dropped), then calls
//     maybeKickAnim() so the tick loop starts if it was idle.
//   - toastTick() (called by animTickMsg handler) removes expired toasts.
//   - animating() is extended in spinner.go to return true while any toast is live,
//     so TTL countdown keeps ticking until the queue drains.
//   - toastOverlayView() renders the stack; overlayToasts() composites it onto
//     the Bg-filled frame by replacing the rightmost W columns of the bottom K rows.
//
// Background-fill strategy (Tier 0 / plan 08c §0):
//   Each toast box carries Background(BgElev) on every rendered cell — set via the
//   lipgloss Border+Width style in toastBoxView(). The outer View() Bg fill runs first
//   so the full frame is m.width wide; overlayToasts runs after, replacing the toast
//   cells in-place. Since toast cells have their own BgElev SGR, they render correctly
//   on any terminal color scheme.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/forge/internal/tui/theme"
)

// toastKind distinguishes the three semantic categories of a notice.
type toastKind int

const (
	toastInfo    toastKind = iota // neutral information (clipboard copy, etc.)
	toastSuccess                  // positive outcome (share URL, etc.)
	toastError                    // failure or "interrupted" notices
)

// toastMaxQueue is the maximum number of live toasts.  When the queue is full,
// the oldest toast is dropped to make room for a new one (FIFO bounded queue).
const toastMaxQueue = 3

// toastTTL is the wall-clock duration a toast remains visible before expiring.
// 3.5 s matches opencode's default toast.duration feel.
const toastTTL = 3500 * time.Millisecond

// toastFadeFrames is the number of animTick frames (×100ms ≈ 0.5s) during which
// the toast text is dimmed toward the background for a subtle fade-out effect.
const toastFadeFrames = 5

// toastMaxWidth is the maximum visible content width of the toast box.
// opencode caps at min(60, termWidth-6); we use 38 characters to stay safe on
// narrow (40-column) terminals while still showing meaningful messages.
const toastMaxWidth = 38

// toast is one transient notice in the live queue.
type toast struct {
	text string
	kind toastKind
	born time.Time // wall-clock birth time for TTL calculation
}

// expired reports whether the toast's TTL has elapsed.
func (t toast) expired() bool {
	return time.Since(t.born) >= toastTTL
}

// fadeT returns the fade fraction ∈ [0,1]: 0 = fully visible, 1 = fully faded.
// The fade window starts toastFadeFrames×animPeriod before expiry and reaches 1.0
// exactly at toastTTL.  Returns 0 outside the fade window (no fade yet).
func (t toast) fadeT() float64 {
	elapsed := time.Since(t.born)
	start := toastTTL - time.Duration(toastFadeFrames)*animPeriod
	if elapsed < start {
		return 0
	}
	remaining := toastTTL - elapsed
	if remaining <= 0 {
		return 1
	}
	window := time.Duration(toastFadeFrames) * animPeriod
	return 1 - float64(remaining)/float64(window)
}

// toastsLive reports whether at least one non-expired toast is queued.
// Used by the extended animating() predicate in spinner.go.
func (m Model) toastsLive() bool {
	for i := range m.toasts {
		if !m.toasts[i].expired() {
			return true
		}
	}
	return false
}

// pushToast enqueues a new toast of the given kind and text.
//
// Cap: if the queue already holds toastMaxQueue items, the oldest is dropped
// first (FIFO).  maybeKickAnim() is returned so the animTick loop starts (or
// continues) counting down the TTL.  Callers batch this with other cmds.
func (m *Model) pushToast(kind toastKind, text string) tea.Cmd {
	if len(m.toasts) >= toastMaxQueue {
		m.toasts = m.toasts[1:] // drop oldest
	}
	m.toasts = append(m.toasts, toast{
		text: text,
		kind: kind,
		born: time.Now(),
	})
	return m.maybeKickAnim()
}

// toastTick is called from the animTickMsg handler.  It removes any expired
// toasts from the queue in-place.  The animTick self-stops once animating()
// returns false (no running tools AND no live toasts).
func (m *Model) toastTick() {
	live := m.toasts[:0]
	for i := range m.toasts {
		if !m.toasts[i].expired() {
			live = append(live, m.toasts[i])
		}
	}
	m.toasts = live
}

// ── Rendering ─────────────────────────────────────────────────────────────────

// kindColor returns the accent color for the border and icon of a toast.
//
// Mapping (mirrors opencode variant→theme token pattern, toast.tsx):
//
//	toastInfo    → Cyan  (opencode variant "info"    → theme.info)
//	toastSuccess → Green (opencode variant "success" → theme.success)
//	toastError   → Red   (opencode variant "error"   → theme.error)
func kindColor(kind toastKind, p theme.Palette) lipgloss.Color {
	switch kind {
	case toastSuccess:
		return p.Green
	case toastError:
		return p.Red
	default:
		return p.Cyan // toastInfo
	}
}

// kindIcon returns a single-character status glyph for the toast kind.
func kindIcon(kind toastKind) string {
	switch kind {
	case toastSuccess:
		return "✓"
	case toastError:
		return "✗"
	default:
		return "i"
	}
}

// toastBoxView renders a single toast as a bordered, background-filled box.
//
// Layout (adapted from opencode toast.tsx box style):
//   - Background(BgElev) on every cell so no transparent bleed-through.
//   - Left+right borders colored by kind (SplitBorder pattern from opencode).
//   - Icon glyph + text on one padded line; text truncated to toastMaxWidth.
//   - The fade-out effect dims foreground and border toward BgElev.
//
// Every rendered line in the output carries BgElev background, satisfying the
// "every line background-filled" requirement from plan 08c Tier 0.
func (m Model) toastBoxView(t toast) string {
	s := m.styles
	p := s.P
	col := kindColor(t.kind, p)
	ft := t.fadeT()

	// Lerp foreground and accent colors toward BgElev for fade-out.
	var fgColor, iconColor, accentColor lipgloss.Color
	bgStr := string(p.BgElev)
	if ft > 0 {
		fgColor = lerpHex(string(p.Fg), bgStr, ft)
		iconColor = lerpHex(string(col), bgStr, ft)
		accentColor = lerpHex(string(col), bgStr, ft)
	} else {
		fgColor = p.Fg
		iconColor = col
		accentColor = col
	}

	// Build the icon + text content line.
	iconStr := lipgloss.NewStyle().
		Foreground(iconColor).
		Background(p.BgElev).
		Render(kindIcon(t.kind))

	// Truncate text to toastMaxWidth minus space for icon (1) + separator (1).
	textTrunc := truncate(t.text, toastMaxWidth-2)
	textStr := lipgloss.NewStyle().
		Foreground(fgColor).
		Background(p.BgElev).
		Render(textTrunc)

	content := iconStr + " " + textStr

	// Wrap in a left+right bordered box with padding, all on BgElev.
	// The Width is set to the content's visible width so lipgloss pads it evenly.
	innerW := lipgloss.Width(content)
	if innerW < 4 {
		innerW = 4 // minimum sensible box
	}

	return lipgloss.NewStyle().
		Background(p.BgElev).
		Foreground(p.Fg).
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderRight(true).
		BorderForeground(accentColor).
		BorderBackground(p.BgElev).
		PaddingLeft(1).
		PaddingRight(1).
		Width(innerW).
		Render(content)
}

// toastOverlayView renders the live toast stack (oldest at top, newest at bottom)
// as a joined string, or "" when there are no live toasts.
//
// Each toast box is rendered through toastBoxView() which sets BgElev background
// on all cells — satisfying the "every line background-filled" requirement.
func (m Model) toastOverlayView() string {
	var live []toast
	for i := range m.toasts {
		if !m.toasts[i].expired() {
			live = append(live, m.toasts[i])
		}
	}
	if len(live) == 0 {
		return ""
	}

	var boxes []string
	for _, t := range live {
		boxes = append(boxes, m.toastBoxView(t))
	}
	return strings.Join(boxes, "\n")
}

// overlayToasts composites the toast stack onto the bottom-right of a
// fully-rendered (Bg-filled) body string.
//
// It is called AFTER the outer View() Bg fill so that all body lines are
// already exactly m.width visible columns wide.  The overlay replaces the
// rightmost oW columns of the last len(oLines) body rows with the toast box.
//
// Returns body unchanged when:
//   - Terminal is too small (width < 20 or height < 5).
//   - There are no live toasts.
//   - The overlay would be wider than the terminal.
//
// Column-replace algorithm:
//
//	For each overlay line i, the corresponding body line bLines[bIdx] is split
//	at visual column (m.width - oW):
//	  - leftPart = first (m.width-oW) visible columns — kept as-is (with ANSI).
//	    We reconstruct it from the plain rune content since we only need to
//	    preserve the background (which the body has via the outer Bg fill);
//	    the toast box cells will carry BgElev, so there is no visible seam.
//	  - overlayLine replaces the right oW columns.
//
// Note: since body is Bg-filled, every line is already exactly m.width wide, so
// we can reliably extract the left (m.width-oW) columns by counting plain runes.
func (m Model) overlayToasts(body string) string {
	if m.width < 20 || m.height < 5 {
		return body
	}

	overlay := m.toastOverlayView()
	if overlay == "" {
		return body
	}

	oLines := strings.Split(overlay, "\n")

	// Measure the overlay width (max visible width across all lines).
	oW := 0
	for _, ol := range oLines {
		if w := lipgloss.Width(ol); w > oW {
			oW = w
		}
	}
	if oW == 0 || oW >= m.width {
		return body
	}

	bLines := strings.Split(body, "\n")
	n := len(oLines) // number of rows the overlay needs
	if n > len(bLines) {
		n = len(bLines)
	}
	if n == 0 {
		return body
	}

	// left part width: total minus the overlay width.
	leftW := m.width - oW

	for i := 0; i < n; i++ {
		bIdx := len(bLines) - n + i
		oLine := oLines[i]

		// Reconstruct left part: take the first leftW visible columns from the
		// body line.  The body line is Bg-filled so we can safely drop the right
		// portion — the toast box replaces it.
		left := runeWidthTrim(bLines[bIdx], leftW)

		// Pad left to exactly leftW plain spaces so the splice point is clean.
		// The outer Bg fill has already set the background on the full line;
		// these spaces inherit the terminal's current background which, after
		// the Bg fill SGR, is the theme Bg color.  Using plain spaces here is
		// correct — the Bg SGR from the outer fill is still active at this point.
		curW := lipgloss.Width(left)
		if curW < leftW {
			left += strings.Repeat(" ", leftW-curW)
		}

		bLines[bIdx] = left + oLine
	}

	return strings.Join(bLines, "\n")
}

// runeWidthTrim returns the string s (which may contain ANSI escape codes)
// trimmed to at most targetW visible columns from the left.
//
// Strategy:
//  1. Strip ANSI from s to get the plain text.
//  2. Walk runes and accumulate visible width until we reach targetW.
//  3. Return the plain prefix (without ANSI) — acceptable because the outer
//     Bg fill already set the background for the entire row before overlayToasts
//     is called.  The left portion is just whitespace + plain text; losing ANSI
//     color spans is visually acceptable at the right-edge splice point.
func runeWidthTrim(s string, targetW int) string {
	if targetW <= 0 {
		return ""
	}
	// Fast path: plain string (no ANSI) or already short enough.
	plain := ansiStripSimple(s)
	if lipgloss.Width(plain) <= targetW {
		return plain
	}
	runes := []rune(plain)
	w := 0
	for i, r := range runes {
		rw := lipgloss.Width(string(r))
		if w+rw > targetW {
			return string(runes[:i])
		}
		w += rw
	}
	return plain
}

// ansiStripSimple removes ANSI SGR escape sequences from s using a simple
// state machine.  It is intentionally minimal — only strips CSI sequences
// (ESC[…m) which are all that lipgloss and glamour emit.  This avoids
// importing an external ANSI package in the production code path; the test
// helper stripANSI (ptypane_test.go) uses a regex and is equivalent for
// the sequences we generate.
func ansiStripSimple(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inEsc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inEsc {
			// End of CSI sequence on any byte in range 0x40–0x7E.
			if c >= 0x40 && c <= 0x7E {
				inEsc = false
			}
			continue
		}
		if c == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			inEsc = true
			i++ // consume the '['
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
