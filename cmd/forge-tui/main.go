// Command forge-tui is the Forge terminal client (plan 08): a Bubble Tea app
// that attaches to a running Forge or opencode daemon over its HTTP+SSE wire
// protocol. It owns no agent state — the daemon is the source of truth.
//
//	forge-tui --url http://127.0.0.1:4096 --dir "$PWD"
//	forge-tui --theme forge-light           # pin theme for deterministic capture
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rotemmiz/forge/internal/tui"
)

func main() {
	cwd, _ := os.Getwd()
	url := flag.String("url", "http://127.0.0.1:4096", "daemon base URL")
	dir := flag.String("dir", cwd, "project directory (x-opencode-directory routing)")
	session := flag.String("session", "", "session id to open on start")
	username := flag.String("username", "", "Basic auth username")
	password := flag.String("password", "", "Basic auth password")
	provider := flag.String("provider", "", "prompt model provider id (else resolved from /config)")
	modelID := flag.String("model", "", "prompt model id")
	themeFlag := flag.String("theme", "", "theme name override (e.g. forge-dark, forge-light, forge-mono); empty = auto-pick or KV-pinned")
	flag.Parse()

	model := tui.New(tui.Config{
		URL: *url, Directory: *dir, SessionID: *session,
		Username: *username, Password: *password,
		Provider: *provider, Model: *modelID,
		Theme: *themeFlag,
	}).Restore() // restore persisted theme/model/history + enable persistence

	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "forge-tui:", err)
		os.Exit(1)
	}
}
