// Command opcode-tui is the Opcode42 terminal client (plan 08): a Bubble Tea app
// that attaches to a running Opcode42 or opencode daemon over its HTTP+SSE wire
// protocol. It owns no agent state — the daemon is the source of truth.
//
//	opcode-tui --url http://127.0.0.1:4096 --dir "$PWD"
//	opcode-tui --theme opcode42-light           # pin theme for deterministic capture
//	opcode-tui --no-anim                        # static logo + frozen spinner (capture / a11y)
package main

import (
	"flag"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/rotemmiz/opcode42/internal/tui"
)

func main() {
	cwd, _ := os.Getwd()
	url := flag.String("url", "", "daemon base URL (empty = KV-pinned server_url or first-run connect overlay)")
	dir := flag.String("dir", cwd, "project directory (x-opencode-directory routing)")
	session := flag.String("session", "", "session id to open on start")
	username := flag.String("username", "", "Basic auth username")
	password := flag.String("password", "", "Basic auth password")
	provider := flag.String("provider", "", "prompt model provider id (else resolved from /config)")
	modelID := flag.String("model", "", "prompt model id")
	themeFlag := flag.String("theme", "", "theme name override (e.g. opcode42-dark, opcode42-light, monochrome); empty = auto-pick or KV-pinned")
	noDiscover := flag.Bool("no-discover", false, "disable mDNS browsing in the connect overlay (plan 08e §D3)")
	noAnim := flag.Bool("no-anim", false, "disable per-frame animation (static logo, frozen spinner, peak bg-pulse) for capture / accessibility")
	sixel := flag.Bool("sixel", false, "force Sixel capability on for image rendering (plan 08e §E2); images still require ctrl+x i to display")
	flag.Parse()

	// When --url is omitted, the TUI defers to tui.Restore: a KV-pinned
	// server_url is applied directly, otherwise the connect overlay opens on
	// startup (plan 08e §D3). --no-discover suppresses the overlay's mDNS
	// browser but the manual URL field still works. We pass "" so Restore
	// can distinguish "no --url" from "--url=http://…".
	urlVal := *url

	model := tui.New(tui.Config{
		URL: urlVal, Directory: *dir, SessionID: *session,
		Username: *username, Password: *password,
		Provider: *provider, Model: *modelID,
		Theme:      *themeFlag,
		NoDiscover: *noDiscover,
		NoAnim:     *noAnim,
		Sixel:      *sixel,
	}).Restore() // restore persisted theme/model/history + enable persistence

	// AltScreen (and other terminal toggles) moved from NewProgram options to
	// fields on the View struct returned by Model.View() in bubbletea v2.
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "opcode-tui:", err)
		os.Exit(1)
	}
}
