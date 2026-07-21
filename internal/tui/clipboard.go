package tui

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// clipboardCopiedMsg is emitted after a copy attempt (no payload — best effort).
type clipboardCopiedMsg struct{}

// copyClipboardCmd copies text to the system clipboard via the OSC-52 escape
// sequence, written straight to the controlling terminal (/dev/tty) so it
// bypasses Bubble Tea's renderer instead of corrupting a frame. OSC-52 works
// over SSH and needs no platform clipboard binary; terminals that don't support
// it simply ignore the sequence. Failure (no tty) is a silent no-op.
func copyClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		if f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			_, _ = f.WriteString(ansi.SetSystemClipboard(text))
			_ = f.Close()
		}
		return clipboardCopiedMsg{}
	}
}
