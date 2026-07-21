// Package scrollregion provides reusable scrollback for a sub-region of a
// full-screen terminal app while leaving the terminal's native text selection
// and copy fully intact.
//
// # The problem it solves
//
// The usual way to scroll part of an alt-screen TUI is to enable mouse reporting
// and consume the wheel events. But the moment an app enables mouse reporting the
// terminal hands it every mouse gesture — including click-drag — and stops doing
// its own selection. The cursor changes to the app's mouse mode and the user can
// no longer select or copy text the normal way. Frameworks like opentui work
// around this by re-implementing selection inside the app; that is a large amount
// of machinery and the result is no longer the terminal's native selection.
//
// scrollregion takes the opposite approach: it never enables mouse reporting.
// Instead it relies on the terminal's *alternate scroll mode* (DECSET 1007),
// which, while the alternate screen is active, translates the mouse wheel into
// ordinary cursor-key input (Up/Down). The app receives plain Up/Down keys and
// scrolls its region; because the mouse is never grabbed, selection and copy keep
// working exactly as the user expects. See the altscroll.go helpers for enabling
// the mode around a program's lifetime.
//
// # What it provides
//
//   - Region: a tail-anchored viewport over a tall body of lines, with the
//     windowing + clamping math (this file).
//   - Action / Decode / Apply: map key strings (including the wheel, which arrives
//     as Up/Down under alternate scroll) to scroll moves (keys.go).
//   - Guard / EnableSeq / DisableSeq: turn alternate scroll mode on and off
//     (altscroll.go).
//
// The package has no dependencies beyond the standard library and is deliberately
// framework-agnostic so it can be lifted into its own module and reused by any
// terminal app (Bubble Tea or otherwise).
package scrollregion

// Region tracks the scroll position of a tail-anchored viewport over a body of
// lines. Offset is the number of lines hidden *below* the viewport: 0 means the
// live tail (newest content) is visible, and increasing Offset scrolls toward
// older content. The zero Region is ready to use and starts pinned to the tail.
//
// Region intentionally separates dimension-free moves (Back/Forward/ToTail),
// which let a caller adjust the position without knowing the body size yet, from
// dimension-aware operations (Clamp/ToTop/Window), which need the body length and
// viewport height. The dimension-free moves defer the top clamp to render time
// via Window, matching apps whose update and render passes are separate.
type Region struct {
	// Offset is the count of lines hidden below the viewport (0 == live tail).
	Offset int
}

// Back scrolls n lines toward older content. The top is not clamped here; Window
// (or an explicit Clamp) bounds it against the body at render time. A non-positive
// n is ignored.
func (r *Region) Back(n int) {
	if n > 0 {
		r.Offset += n
	}
}

// Forward scrolls n lines toward the tail, flooring at the live tail. A
// non-positive n is ignored.
func (r *Region) Forward(n int) {
	if n > 0 {
		r.Offset -= n
		if r.Offset < 0 {
			r.Offset = 0
		}
	}
}

// ToTail snaps the viewport back to the newest content.
func (r *Region) ToTail() { r.Offset = 0 }

// AtTail reports whether the viewport is showing the newest content.
func (r Region) AtTail() bool { return r.Offset <= 0 }

// MaxOffset is the largest valid Offset for a body of total lines shown in a
// viewport of height rows — i.e. how far back the viewport can scroll. It is 0
// when the body fits entirely.
func MaxOffset(total, height int) int {
	if m := total - height; m > 0 {
		return m
	}
	return 0
}

// Clamp constrains Offset to [0, MaxOffset(total, height)]. Call it when scrolling
// to an absolute position (e.g. ToTop) or after the body/viewport size changes so
// a stale offset can't point past the ends.
func (r *Region) Clamp(total, height int) {
	if hi := MaxOffset(total, height); r.Offset > hi {
		r.Offset = hi
	}
	if r.Offset < 0 {
		r.Offset = 0
	}
}

// ToTop scrolls fully to the oldest content for the given dimensions.
func (r *Region) ToTop(total, height int) { r.Offset = MaxOffset(total, height) }

// Window returns the lines visible in a viewport of height rows at the current
// Offset. When the body is shorter than height the result is padded with empty
// lines up to height, so a caller can reliably pin a footer to the bottom row.
// The returned slice always has exactly height lines (for height >= 1). Offset is
// clamped for the purpose of windowing but Window does not mutate the Region — to
// persist a clamp, call Clamp.
func (r Region) Window(lines []string, height int) []string {
	if height < 1 {
		height = 1
	}
	if len(lines) <= height {
		out := make([]string, len(lines), height)
		copy(out, lines)
		for len(out) < height {
			out = append(out, "")
		}
		return out
	}
	off := r.Offset
	if hi := len(lines) - height; off > hi {
		off = hi
	}
	if off < 0 {
		off = 0
	}
	end := len(lines) - off
	return lines[end-height : end]
}
