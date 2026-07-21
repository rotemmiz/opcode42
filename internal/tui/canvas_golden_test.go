package tui

// canvas_golden_test.go — Plan 08e §A4 canvas golden tests.
//
// These are the checked-in render goldens the plan mandates: a deterministic
// splash frame and a deterministic seeded-session frame, captured on the v2
// canvas with --no-anim so the logo shimmer and bg-pulse are frozen at the peak
// frame (logoPeakFrame). The goldens live in internal/tui/testdata/ and are
// diffed against m.composeView() output; a mismatch fails the test and the
// golden must be regenerated with -update.
//
// Regenerate the goldens:
//
//	go test -run 'TestCanvas_Golden_(Splash|Session)' -update ./internal/tui/
//
// The goldens are plain-text (ANSI-stripped) frames so they diff cleanly in
// code review and don't depend on the terminal's color profile. The full
// ANSI render is already covered by TestCanvas_FullFill_NoTransparentCell
// (width invariant) and TestCanvas_LogoCellsHaveShimmerColor (per-cell color
// invariant); the golden is the *content* snapshot — what the user actually
// sees on a given frame.
//
// Determinism contract: the renders these goldens pin are a pure function of
// (theme, width, height, screen, store state, noAnim). The model carries no
// time-dependent state under noAnim (the animation tick is what makes the
// animated path non-deterministic; --no-anim freezes it), so the same inputs
// always yield the same frame. If a future change makes the render depend on
// wall-clock time, os.Stdin, or a random source, these goldens will fail —
// which is the point.

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGoldens is the -update flag: when set, the golden tests write the
// rendered frame to the testdata file instead of comparing. Mirrors the
// standard Go golden-test idiom (e.g. cmd/api's -update flag).
var updateGoldens = flag.Bool("update", false, "regenerate canvas golden files in testdata/")

// goldenPath resolves a testdata path relative to this package.
func goldenPath(name string) string {
	return filepath.Join("testdata", name)
}

// assertGolden compares got against the checked-in golden at testdata/name,
// writing got to the file when -update is set. The golden is stored
// ANSI-stripped (plain text) so it diffs cleanly in review and is independent
// of the terminal color profile (the color invariants are pinned by the
// per-cell canvas tests in canvas_test.go; the golden is the content
// snapshot). t.Fatal is used on both read/write failure so the test names the
// golden file in the failure.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(name)
	want := strings.TrimRight(got, "\n")
	if *updateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(want+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("updated %s", path)
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nregenerate with: go test -run %s -update ./internal/tui/",
			path, err, t.Name())
	}
	have := strings.TrimRight(string(b), "\n")
	if have != want {
		t.Fatalf("golden mismatch for %s\n--- want ---\n%s\n--- have ---\n%s\nregenerate with: go test -run %s -update ./internal/tui/",
			path, have, want, t.Name())
	}
}

// TestCanvas_Golden_Splash pins the splash screen at 80×24 on opcode42-dark
// with --no-anim. This is the deterministic capture frame the tools/tui-shots
// harness produces (00-splash / 03-home-empty): the block-pixel opcode42
// wordmark at the peak shimmer frame, the empty composer, the hint line, and
// the connection status. A drift in the splash geometry (logo origin,
// composer placement, status text) surfaces as a golden mismatch.
func TestCanvas_Golden_Splash(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.noAnim = true
	out := m.composeView()
	assertGolden(t, "canvas-splash-80x24-dark.txt", stripANSI(out))
}

// TestCanvas_Golden_Session pins a seeded session at 100×60 on opcode42-dark.
// The session carries a known user/assistant message pair so the stream body,
// sidebar (context gauge + LSP section), footer composer, and status bar are
// all exercised. This is the deterministic content frame the harness's
// conversation tapes (01-conversation) capture; a drift in any pane's
// geometry or content surfaces as a golden mismatch.
func TestCanvas_Golden_Session(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_golden"})
	m.screen = ScreenSession
	m.width, m.height = 100, 60
	m = m.applyThemeByName("opcode42-dark")
	m.store.sessions = []Session{{ID: "ses_golden", Title: "Golden session"}}
	m.store.messages["ses_golden"] = []Message{
		{ID: "msg_u1", SessionID: "ses_golden", Role: "user"},
		{ID: "msg_a1", SessionID: "ses_golden", Role: "assistant"},
	}
	m.store.parts["msg_u1"] = []Part{{
		ID: "pu1", MessageID: "msg_u1", Type: "text",
		Text: "Hello from the golden test.",
	}}
	m.store.parts["msg_a1"] = []Part{{
		ID: "pa1", MessageID: "msg_a1", Type: "text",
		Text: "Hi! This is a deterministic assistant reply.",
	}}
	out := m.composeView()
	assertGolden(t, "canvas-session-100x60-dark.txt", stripANSI(out))
}
