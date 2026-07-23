package tui

import (
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// clipboardCopiedMsg is emitted after a copy attempt (no payload — best effort).
type clipboardCopiedMsg struct{}

// clipboardReadMsg is the result of a ctrl+v / prompt.paste clipboard read
// (plan 08f H2 / G.2). Mime is "text/plain" or an image/* type; Data is plain
// text for text/plain, or raw image bytes for images (base64-encoded into a
// data URL when attached).
type clipboardReadMsg struct {
	Mime string
	Data []byte
	Err  error
}

// pendingFile is a composer-side file attachment staged by clipboard image
// paste (opencode pasteAttachment). Sent as a file part on the next submit.
type pendingFile struct {
	Filename string
	Mime     string
	URL      string // data:<mime>;base64,<payload>
}

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

// readClipboardCmd reads the system clipboard (plan 08f H2). Mirrors
// opencode packages/tui/src/clipboard.ts:29-74: try a platform image
// clipboard first, then fall back to text. Empty clipboard → Err set.
func readClipboardCmd() tea.Cmd {
	return func() tea.Msg {
		if mime, data, ok := readClipboardImage(); ok {
			return clipboardReadMsg{Mime: mime, Data: data}
		}
		text, err := readClipboardText()
		if err != nil {
			return clipboardReadMsg{Err: err}
		}
		if text == "" {
			return clipboardReadMsg{Err: errClipboardEmpty}
		}
		return clipboardReadMsg{Mime: "text/plain", Data: []byte(text)}
	}
}

var errClipboardEmpty = errString("clipboard empty")

type errString string

func (e errString) Error() string { return string(e) }

func readClipboardText() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("pbpaste").Output()
		return string(out), err
	case "linux":
		if out, err := exec.Command("wl-paste", "-n").Output(); err == nil {
			return string(out), nil
		}
		if out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output(); err == nil {
			return string(out), nil
		}
		out, err := exec.Command("xsel", "--clipboard", "--output").Output()
		return string(out), err
	case "windows":
		out, err := exec.Command("powershell.exe", "-NonInteractive", "-NoProfile", "-Command", "Get-Clipboard").Output()
		return strings.TrimRight(string(out), "\r\n"), err
	default:
		return "", errString("clipboard read unsupported on " + runtime.GOOS)
	}
}

// readClipboardImage returns PNG bytes when the clipboard holds an image.
// Best-effort; failures fall through to text. Darwin uses osascript PNGf
// (opencode clipboard.ts:30-50); Linux tries wl-paste / xclip image/png.
func readClipboardImage() (mime string, data []byte, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		tmp, err := os.CreateTemp("", "opcode42-clipboard-*.png")
		if err != nil {
			return "", nil, false
		}
		path := tmp.Name()
		_ = tmp.Close()
		defer func() { _ = os.Remove(path) }()
		script := `set imageData to the clipboard as "PNGf"
set fileRef to open for access POSIX file "` + path + `" with write permission
set eof fileRef to 0
write imageData to fileRef
close access fileRef`
		if err := exec.Command("osascript", "-e", script).Run(); err != nil {
			return "", nil, false
		}
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			return "", nil, false
		}
		return "image/png", b, true
	case "linux":
		if out, err := exec.Command("wl-paste", "-t", "image/png").Output(); err == nil && len(out) > 0 {
			return "image/png", out, true
		}
		if out, err := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output(); err == nil && len(out) > 0 {
			return "image/png", out, true
		}
	}
	return "", nil, false
}

// dataURL builds a data:<mime>;base64,<payload> URL for a pending file part.
func dataURL(mime string, raw []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}
