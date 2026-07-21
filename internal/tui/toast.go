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

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
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
func kindColor(kind toastKind, p theme.Palette) theme.Color {
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
	var fgColor, iconColor, accentColor theme.Color
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
		// lipgloss v2: Width is the total box width (content + padding + border).
		// Add back the 2 padding + 2 border columns so the content area stays innerW.
		Width(innerW + 4).
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
