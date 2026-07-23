package tui

import (
	"os/exec"
	"runtime"

	tea "charm.land/bubbletea/v2"
)

// docsURL is the documentation site opened by docs.open / /docs
// (plan 08f H8 / G.1; opencode app.tsx:805-810).
const docsURL = "https://opencode.ai/docs"

// openURLDoneMsg is the result of a background URL-open attempt.
type openURLDoneMsg struct {
	URL string
	Err error
}

// openURLFn is the platform opener; overridden in tests.
var openURLFn = openURL

// openDocsCmd opens the docs URL in the system browser (fire-and-forget;
// does not suspend the TUI).
func openDocsCmd() tea.Cmd {
	return openURLCmd(docsURL)
}

// openURLCmd opens url via the platform browser helper.
func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		return openURLDoneMsg{URL: url, Err: openURLFn(url)}
	}
}

// openURL launches the system URL handler. Returns an error when no
// suitable helper is available.
func openURL(url string) error {
	name, args := browserCommand(runtime.GOOS, url)
	if name == "" {
		return errString("open URL unsupported on " + runtime.GOOS)
	}
	cmd := exec.Command(name, args...)
	return cmd.Start()
}

// browserCommand returns the platform helper + args for opening a URL
// (macOS `open`, Linux `xdg-open`, Windows `rundll32 url.dll,FileProtocolHandler`).
func browserCommand(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "linux":
		return "xdg-open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "", nil
	}
}
