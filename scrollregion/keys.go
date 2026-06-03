package scrollregion

// Action is a scroll intent decoded from a key. None means the key is not a
// scroll key and the host should handle it normally.
type Action int

// Scroll actions a key can decode to. None means "not a scroll key".
const (
	None     Action = iota
	Up              // one step toward older content
	Down            // one step toward the tail
	PageUp          // one page toward older content
	PageDown        // one page toward the tail
	Top             // jump to the oldest content
	Bottom          // jump to the newest content (the tail)
)

// Decode maps a key name to a scroll Action. The names match the common
// Bubble Tea key strings, but the package does not depend on Bubble Tea — any
// host that produces these strings can use it.
//
// Note that the mouse wheel is decoded here too: with alternate scroll mode on
// (see Guard), the terminal delivers wheel motion as "up"/"down" keys, so they
// decode to Up/Down exactly like the cursor keys. That is the whole point — the
// app handles one code path for both the wheel and the keyboard, and the mouse is
// never grabbed, so native selection survives.
func Decode(key string) Action {
	switch key {
	case "up":
		return Up
	case "down":
		return Down
	case "pgup":
		return PageUp
	case "pgdown", "pgdn":
		return PageDown
	case "home":
		return Top
	case "end":
		return Bottom
	default:
		return None
	}
}

// Apply moves the Region for a decoded Action: step lines for Up/Down and a page
// of the given size for PageUp/PageDown. Up/Down/PageUp/PageDown are
// dimension-free (the top is clamped by Window at render time); Top sets a
// deliberately large offset that Window/Clamp bound to the oldest line. Pass page
// as the viewport height (or height-1 to keep a line of overlap).
func (r *Region) Apply(a Action, step, page int) {
	switch a {
	case Up:
		r.Back(step)
	case Down:
		r.Forward(step)
	case PageUp:
		r.Back(page)
	case PageDown:
		r.Forward(page)
	case Top:
		// Bounded to the oldest line by Window/Clamp once dimensions are known.
		r.Offset = 1 << 30
	case Bottom:
		r.ToTail()
	}
}
