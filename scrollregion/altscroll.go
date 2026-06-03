package scrollregion

import "io"

// Alternate scroll mode (DECSET 1007). When enabled and the alternate screen is
// active, the terminal converts mouse-wheel motion into cursor-key input instead
// of requiring the app to enable mouse reporting. Because mouse reporting stays
// off, the terminal continues to handle text selection and copy natively.
const (
	enableSeq  = "\x1b[?1007h" // turn alternate scroll mode on
	disableSeq = "\x1b[?1007l" // turn alternate scroll mode off
)

// EnableSeq is the raw control sequence that turns alternate scroll mode on, for
// callers that manage their own terminal writes (e.g. emitting it via a renderer).
func EnableSeq() string { return enableSeq }

// DisableSeq is the raw control sequence that turns alternate scroll mode off.
func DisableSeq() string { return disableSeq }

// Guard turns alternate scroll mode on by writing to w and returns a function
// that turns it back off. Call the returned function before the process exits to
// restore the terminal. Typical use around a Bubble Tea program:
//
//	restore := scrollregion.Guard(os.Stdout)
//	_, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
//	restore()
//
// Note: prefer calling restore() explicitly (not via defer) when the surrounding
// code may os.Exit, since deferred calls do not run before os.Exit. Write errors
// are ignored — failing to toggle a terminal mode should never abort the app.
func Guard(w io.Writer) func() {
	io.WriteString(w, enableSeq) //nolint:errcheck // best-effort terminal mode
	return func() {
		io.WriteString(w, disableSeq) //nolint:errcheck // best-effort restore
	}
}
