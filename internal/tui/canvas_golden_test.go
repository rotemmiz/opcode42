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
//	go test ./internal/tui/ -run 'TestCanvas_Golden_(Splash|Session)' -args -update
//
// (the package path precedes -run; -update is a test-binary flag, so it goes
// after -args so `go test` forwards it rather than trying to parse it itself.)
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
		t.Fatalf("read %s: %v\nregenerate with: go test ./internal/tui/ -run %s -args -update",
			path, err, t.Name())
	}
	have := strings.TrimRight(string(b), "\n")
	if have != want {
		t.Fatalf("golden mismatch for %s\n--- want ---\n%s\n--- have ---\n%s\nregenerate with: go test ./internal/tui/ -run %s -args -update",
			path, have, want, t.Name())
	}
}

// TestCanvas_Golden_Splash pins the splash screen at 80×24 on opcode42-dark
// with --no-anim. This is the deterministic capture frame the tools/tui-shots
// harness produces (00-splash / 03-home-empty): the block-pixel opcode42
// wordmark at the peak shimmer frame, the empty composer, the hint line, and
// the connection status. A drift in the splash geometry (logo origin,
// composer placement, status text) surfaces as a golden mismatch.
//
// termDark is pinned true so applyThemeByName resolves the dark token variant
// deterministically regardless of the test runner's terminal background
// (New() probes os.Stdin, which is a non-TTY in CI and defaults to dark, but
// pinning removes the latent dependency on that default).
func TestCanvas_Golden_Splash(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.termDark = true
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
// geometry or content surfaces as a golden mismatch. termDark is pinned (see
// TestCanvas_Golden_Splash for rationale).
func TestCanvas_Golden_Session(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_golden"})
	m.termDark = true
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

// ── Plan 08e §F1: modal / overlay scene goldens ──────────────────────────────
//
// A4 added the splash + session goldens. F1 (the visual parity audit workstream)
// adds the modal scene goldens the plan enumerates: the command palette, the
// sessions list (with the subtree view added in §C4), the models list, the diff
// reviewer (with seeded diff data), and the permission + question overlays. Each
// renders at a fixed size + theme and asserts against a checked-in golden in
// internal/tui/testdata/. These are the deterministic equivalents of the VHS
// modal captures (15-slash-commands, 16-command-palette, 17-model-list,
// 19-session-list, 07-tools-diff); the goldens are the actual parity gate
// (per CLAUDE.md's "no fabricated numbers" rule, the VHS pixel-diff % is a
// guidance signal and the goldens are the deterministic assertion).
//
// All renders go through composeView() so the canvas compositor + z-order +
// bg-fill invariants are exercised on every modal frame (not just the modal's
// own card content): a regression in the modal's z-order (e.g. the body
// showing through) or the base Bg fill surfaces as a golden mismatch.
//
// termDark is pinned true (see TestCanvas_Golden_Splash for rationale).

// TestCanvas_Golden_ModalPalette pins the command-palette modal (Ctrl+P) at
// 80×24 on opcode42-dark. The palette is the first modal the F1 scene list
// names (scene 16: command-palette); its golden fixes the title row, the
// filter affordance, the first visible palette items, the selection bar on
// row 0, and the footer hint. A drift in any of those, or the modal's centered
// geometry, surfaces as a mismatch.
func TestCanvas_Golden_ModalPalette(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.termDark = true
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.store.sessions = []Session{{ID: "ses_1", Title: "Palette session"}}
	m.store.messages["ses_1"] = []Message{
		{ID: "msg_1", SessionID: "ses_1", Role: "user"},
	}
	m.store.parts["msg_1"] = []Part{{ID: "p1", MessageID: "msg_1", Type: "text", Text: "open the palette"}}
	m.modal = modalPalette
	m.modalSel = 0
	out := m.composeView()
	assertGolden(t, "canvas-modal-palette-80x24-dark.txt", stripANSI(out))
}

// TestCanvas_Golden_ModalSessions pins the sessions list modal (Ctrl+X l) at
// 80×24 on opcode42-dark with three seeded sessions. The golden fixes the
// title, the filter affordance, the seeded session rows (newest-first per
// orderedSessions), the selection bar on the first row, and the subtree
// toggle hint. Scene 19 (session-list).
func TestCanvas_Golden_ModalSessions(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_a"})
	m.termDark = true
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.store.sessions = []Session{
		{ID: "ses_a", Title: "First session"},
		{ID: "ses_b", Title: "Second session"},
		{ID: "ses_c", Title: "Third session"},
	}
	m.modal = modalSessions
	m.modalSel = 0
	out := m.composeView()
	assertGolden(t, "canvas-modal-sessions-80x24-dark.txt", stripANSI(out))
}

// TestCanvas_Golden_ModalModels pins the models list modal (Ctrl+X m) at 80×24
// on opcode42-dark with a seeded provider catalog. The golden fixes the title,
// the filter affordance, the seeded model rows with the active-model mark (●)
// on the current model, the selection bar, and the footer hint. Scene 17
// (model-list).
func TestCanvas_Golden_ModalModels(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1", Provider: "anthropic", Model: "claude-sonnet-4"})
	m.termDark = true
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.store.sessions = []Session{{ID: "ses_1", Title: "Models session"}}
	m.choices = []modelChoice{
		{Provider: "anthropic", Model: "claude-sonnet-4"},
		{Provider: "anthropic", Model: "claude-opus-4"},
		{Provider: "openai", Model: "gpt-4o"},
	}
	m.modal = modalModels
	m.modalSel = 0
	out := m.composeView()
	assertGolden(t, "canvas-modal-models-80x24-dark.txt", stripANSI(out))
}

// TestCanvas_Golden_Diff pins the full-screen diff reviewer at 100×40 on
// opcode42-dark with two seeded files (one modified, one added). The golden
// fixes the summary header (file count, +additions / -deletions), the file-tree
// pane, the separator, the selected file's unified patch, and the key hints.
// Scene 07 (tools-diff). This is the F1 plan's "add a golden for the diff
// reviewer (if feasible in a test — the diff needs seeded diff data)" item:
// the diff is seeded directly on m.diff.files (the post-load state), so no
// HTTP round-trip is needed.
func TestCanvas_Golden_Diff(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_diff"})
	m.termDark = true
	m.screen = ScreenSession
	m.width, m.height = 100, 40
	m = m.applyThemeByName("opcode42-dark")
	m.store.sessions = []Session{{ID: "ses_diff", Title: "Diff session"}}
	m.diff = diffState{
		open:     true,
		loading:  false,
		source:   sourceSession,
		showTree: true,
		folded:   map[int]bool{},
		files: []SnapshotFileDiff{
			{
				File:      "src/main.go",
				Patch:     "--- a/src/main.go\n+++ b/src/main.go\n@@ -1,3 +1,4 @@\n package main\n\n+import \"fmt\"\n func main() {}\n",
				Additions: 1,
				Deletions: 0,
				Status:    "modified",
			},
			{
				File:      "docs/README.md",
				Patch:     "--- /dev/null\n+++ b/docs/README.md\n@@ -0,0 +1 @@\n+opcode42 diff reviewer golden.\n",
				Additions: 1,
				Deletions: 0,
				Status:    "added",
			},
		},
	}
	out := m.composeView()
	assertGolden(t, "canvas-diff-100x40-dark.txt", stripANSI(out))
}

// TestCanvas_Golden_Permission pins the permission overlay at 80×24 on
// opcode42-dark with a pending bash permission. The golden fixes the
// "Permission required" header, the action line, the metadata detail, the
// three choice rows (Allow once / Allow always / Reject) with the selection
// bar on row 0, and the key hints. This is the F1 plan's "add a golden for
// the permission/question overlay" item.
func TestCanvas_Golden_Permission(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.termDark = true
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.store.sessions = []Session{{ID: "ses_1", Title: "Permission session"}}
	m.store.permissions = []Permission{{
		ID:         "perm_1",
		SessionID:  "ses_1",
		Permission: "bash",
		Metadata:   []byte(`{"command":"ls -la"}`),
		Tool:       []byte(`{"name":"bash"}`),
	}}
	m.permState = newPermissionState()
	out := m.composeView()
	assertGolden(t, "canvas-permission-80x24-dark.txt", stripANSI(out))
}

// TestCanvas_Golden_Question pins the question overlay at 80×24 on opcode42-dark
// with a pending single-select question. The golden fixes the header, the
// question text, the option rows with the selection bar on the first option,
// and the key hints. The F1 plan pairs this with the permission overlay golden
// ("the permission/question overlay"); both are covered.
func TestCanvas_Golden_Question(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.termDark = true
	m.screen = ScreenSession
	m.width, m.height = 80, 24
	m = m.applyThemeByName("opcode42-dark")
	m.store.sessions = []Session{{ID: "ses_1", Title: "Question session"}}
	m.store.questions = []Question{{
		ID:        "q_1",
		SessionID: "ses_1",
		Questions: []QuestionInfo{{
			Question: "Which file should the agent edit?",
			Header:   "Pick a file",
			Options: []QuestionOption{
				{Label: "src/main.go"},
				{Label: "src/util.go"},
				{Label: "docs/README.md"},
			},
			Multiple: false,
		}},
	}}
	m.qBody = questionBodyState{}
	out := m.composeView()
	assertGolden(t, "canvas-question-80x24-dark.txt", stripANSI(out))
}
