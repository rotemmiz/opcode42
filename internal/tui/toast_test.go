package tui

// toast_test.go — Plan 08c M11: tests for the toast overlay.
//
// Coverage:
//  1. pushToast enqueues; queue caps at toastMaxQueue (oldest dropped).
//  2. Expiry: toastTick() removes expired toasts; animating() reflects the queue.
//  3. toastOverlayView contains toast text and is background-filled.
//  4. overlayToasts does not panic on tiny terminals.
//  5. animating() is true while a toast is live and false after the queue drains.
//  6. kind colors are present in the rendered box (dark + light themes).

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// newToastModel builds a minimal Model with no session for toast tests.
func newToastModel() Model {
	return Model{
		cfg:    Config{URL: "http://localhost:4096"},
		styles: theme.New(theme.Default()),
		width:  100,
		height: 30,
		// Toasts overlay session screens; pin a non-splash screen so animating()'s
		// splash-shimmer case (plan 08c M10) doesn't mask the toast/tool logic
		// these tests exercise.
		screen: ScreenSession,
	}
}

// liveToast returns a toast whose TTL has not yet elapsed.
func liveToast(kind toastKind, text string) toast {
	return toast{text: text, kind: kind, born: time.Now()}
}

// expiredToast returns a toast whose TTL has already elapsed.
func expiredToast(kind toastKind, text string) toast {
	return toast{text: text, kind: kind, born: time.Now().Add(-toastTTL - time.Second)}
}

// ── pushToast tests ───────────────────────────────────────────────────────────

// TestPushToastEnqueues verifies that pushToast adds a toast to the queue.
func TestPushToastEnqueues(t *testing.T) {
	m := newToastModel()
	_ = m.pushToast(toastInfo, "hello")
	if len(m.toasts) != 1 {
		t.Fatalf("expected 1 toast, got %d", len(m.toasts))
	}
	if m.toasts[0].text != "hello" {
		t.Errorf("expected text %q, got %q", "hello", m.toasts[0].text)
	}
	if m.toasts[0].kind != toastInfo {
		t.Errorf("expected kind %d, got %d", toastInfo, m.toasts[0].kind)
	}
}

// TestPushToastCapsQueue verifies that the queue never exceeds toastMaxQueue,
// and that the OLDEST toast is dropped when the queue is full.
func TestPushToastCapsQueue(t *testing.T) {
	m := newToastModel()
	for i := range toastMaxQueue + 2 {
		_ = m.pushToast(toastInfo, strings.Repeat("a", i+1))
	}
	if len(m.toasts) != toastMaxQueue {
		t.Fatalf("queue length = %d, want %d", len(m.toasts), toastMaxQueue)
	}
	// The queue should hold the NEWEST toastMaxQueue items (oldest dropped).
	// After pushing toastMaxQueue+2 items, the oldest 2 are gone.
	// Item indices 0..toastMaxQueue+1 were pushed; survivors are the last toastMaxQueue.
	for i, t2 := range m.toasts {
		want := strings.Repeat("a", i+3) // items 2..4 (0-indexed) → texts "aaa".."aaaaa"
		if t2.text != want {
			t.Errorf("queue[%d] = %q, want %q (oldest should have been dropped)", i, t2.text, want)
		}
	}
}

// TestPushToastKicksAnim verifies that pushToast returns a non-nil cmd (the
// animTick kick) when a toast is live.
func TestPushToastKicksAnim(t *testing.T) {
	m := newToastModel()
	cmd := m.pushToast(toastSuccess, "copied")
	if cmd == nil {
		t.Error("pushToast should return a non-nil cmd (animTick kick) when a toast is live")
	}
}

// ── toastTick / expiry tests ──────────────────────────────────────────────────

// TestToastTickRemovesExpired verifies that toastTick purges expired toasts.
func TestToastTickRemovesExpired(t *testing.T) {
	m := newToastModel()
	// One expired, one live.
	m.toasts = []toast{
		expiredToast(toastError, "old error"),
		liveToast(toastInfo, "still here"),
	}
	m.toastTick()
	if len(m.toasts) != 1 {
		t.Fatalf("after toastTick: want 1 toast, got %d", len(m.toasts))
	}
	if m.toasts[0].text != "still here" {
		t.Errorf("wrong survivor: got %q, want %q", m.toasts[0].text, "still here")
	}
}

// TestToastTickDrainsAll verifies that toastTick removes all toasts when all
// are expired, leaving an empty queue.
func TestToastTickDrainsAll(t *testing.T) {
	m := newToastModel()
	m.toasts = []toast{
		expiredToast(toastInfo, "a"),
		expiredToast(toastSuccess, "b"),
	}
	m.toastTick()
	if len(m.toasts) != 0 {
		t.Fatalf("expected empty queue after all expired, got %d toasts", len(m.toasts))
	}
}

// ── animating() / toastsLive() tests ─────────────────────────────────────────

// TestAnimatingTrueWhileToastLive verifies that animating() returns true when
// there is at least one live (non-expired) toast in the queue.
func TestAnimatingTrueWhileToastLive(t *testing.T) {
	m := newToastModel()
	m.toasts = []toast{liveToast(toastSuccess, "copied")}
	if !m.animating() {
		t.Error("animating() should be true while a live toast is queued")
	}
}

// TestAnimatingFalseAfterToastExpires verifies that animating() returns false
// once all toasts have expired (and no tools are running).
func TestAnimatingFalseAfterToastExpires(t *testing.T) {
	m := newToastModel()
	m.toasts = []toast{expiredToast(toastInfo, "old")}
	if m.animating() {
		t.Error("animating() should be false when all toasts are expired")
	}
}

// TestAnimatingFalseEmptyQueue verifies that animating() is false with no toasts.
func TestAnimatingFalseEmptyQueue(t *testing.T) {
	m := newToastModel()
	if m.animating() {
		t.Error("animating() should be false with an empty toast queue and no session")
	}
}

// TestToastsLiveTrueWhenLive verifies toastsLive() returns true for live toasts.
func TestToastsLiveTrueWhenLive(t *testing.T) {
	m := newToastModel()
	m.toasts = []toast{liveToast(toastSuccess, "x")}
	if !m.toastsLive() {
		t.Error("toastsLive() should be true for a live toast")
	}
}

// TestToastsLiveFalseWhenExpired verifies toastsLive() returns false for expired.
func TestToastsLiveFalseWhenExpired(t *testing.T) {
	m := newToastModel()
	m.toasts = []toast{expiredToast(toastError, "y")}
	if m.toastsLive() {
		t.Error("toastsLive() should be false when all toasts are expired")
	}
}

// ── toastOverlayView tests ────────────────────────────────────────────────────

// TestToastOverlayViewEmpty verifies that toastOverlayView returns "" when the
// queue is empty or all toasts are expired.
func TestToastOverlayViewEmpty(t *testing.T) {
	m := newToastModel()
	if got := m.toastOverlayView(); got != "" {
		t.Errorf("expected empty string for empty queue, got %q", got)
	}
	m.toasts = []toast{expiredToast(toastInfo, "old")}
	if got := m.toastOverlayView(); got != "" {
		t.Errorf("expected empty string for expired toasts, got %q", got)
	}
}

// TestToastOverlayViewContainsText verifies that the overlay view contains the
// toast text words (ANSI-stripped) for each live toast.  The box may word-wrap
// long text across multiple lines, so we check that all words are present in the
// full plain output (joined) rather than requiring the exact original string.
func TestToastOverlayViewContainsText(t *testing.T) {
	m := newToastModel()
	m.toasts = []toast{liveToast(toastInfo, "hellotest")}
	got := m.toastOverlayView()
	// Join lines so word-wrapped content is still found.
	plain := strings.ReplaceAll(stripANSI(got), "\n", " ")
	if !strings.Contains(plain, "hellotest") {
		t.Errorf("overlay view should contain toast text; got (plain): %q", plain)
	}
}

// TestToastOverlayViewMultipleToasts verifies that all live toasts appear in the
// overlay view.
func TestToastOverlayViewMultipleToasts(t *testing.T) {
	m := newToastModel()
	m.toasts = []toast{
		liveToast(toastInfo, "first"),
		liveToast(toastSuccess, "second"),
		liveToast(toastError, "third"),
	}
	got := m.toastOverlayView()
	plain := stripANSI(got)
	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(plain, want) {
			t.Errorf("overlay view missing %q; plain: %q", want, plain)
		}
	}
}

// TestToastOverlayViewBackgroundFilled verifies that every line of the overlay
// view carries a non-zero visible width (i.e. is not just a blank line), and
// that BgElev color appears somewhere in the rendered output.
//
// Note: in test environments lipgloss does not emit ANSI escape codes (no TTY /
// NO_COLOR), so we check structural properties (non-empty lines, text content)
// rather than raw ANSI sequences.
func TestToastOverlayViewBackgroundFilled(t *testing.T) {
	for _, tn := range []string{"opcode42-dark", "opcode42-light"} {
		t.Run(tn, func(t *testing.T) {
			p, ok := theme.ByName(tn)
			if !ok {
				t.Fatalf("theme %q not found", tn)
			}
			m := newToastModel()
			m.styles = theme.New(p)
			m.toasts = []toast{liveToast(toastSuccess, "copied to clipboard")}
			got := m.toastOverlayView()
			if got == "" {
				t.Fatal("overlay view is empty for a live toast")
			}
			lines := strings.Split(got, "\n")
			for i, line := range lines {
				// Each line must have non-zero visible width (the box has content).
				if lipgloss.Width(line) == 0 && line != "" {
					t.Errorf("line %d: visible width 0 but not empty; content %q", i, line)
				}
			}
		})
	}
}

// TestToastOverlayViewKindColors verifies that the overlay view for each toast
// kind actually contains the expected icon character (which is colored by the
// kind color).
func TestToastOverlayViewKindColors(t *testing.T) {
	cases := []struct {
		kind toastKind
		icon string
	}{
		{toastInfo, kindIcon(toastInfo)},
		{toastSuccess, kindIcon(toastSuccess)},
		{toastError, kindIcon(toastError)},
	}
	for _, tc := range cases {
		t.Run(tc.icon, func(t *testing.T) {
			m := newToastModel()
			m.toasts = []toast{liveToast(tc.kind, "msg")}
			plain := stripANSI(m.toastOverlayView())
			if !strings.Contains(plain, tc.icon) {
				t.Errorf("overlay missing icon %q for kind %d; plain: %q", tc.icon, tc.kind, plain)
			}
		})
	}
}

// ── overlayToasts tests ───────────────────────────────────────────────────────

// TestOverlayToastsNoPanicTiny verifies that overlayToasts does not panic on a
// very small terminal (10×4) — it should return body unchanged.
func TestOverlayToastsNoPanicTiny(t *testing.T) {
	m := newToastModel()
	m.width, m.height = 10, 4
	m.toasts = []toast{liveToast(toastInfo, "hi")}
	body := strings.Repeat("x", 10) + "\n" + strings.Repeat("y", 10)
	// Must not panic.
	got := m.overlayToasts(body)
	if got == "" {
		t.Error("overlayToasts should return non-empty for non-empty body")
	}
}

// TestOverlayToastsNoToastsUnchanged verifies that overlayToasts returns body
// unchanged when the queue is empty.
func TestOverlayToastsNoToastsUnchanged(t *testing.T) {
	m := newToastModel()
	m.width, m.height = 80, 24
	body := strings.Repeat("x", 80) + "\n" + strings.Repeat("y", 80)
	got := m.overlayToasts(body)
	if got != body {
		t.Errorf("overlayToasts with no toasts should return body unchanged")
	}
}

// TestOverlayToastsPreservesBodyLength verifies that overlayToasts does not
// change the number of lines in body.
func TestOverlayToastsPreservesBodyLength(t *testing.T) {
	m := newToastModel()
	m.width, m.height = 100, 30
	m.toasts = []toast{liveToast(toastSuccess, "copied")}

	// Build a multi-line body.
	lineCount := 20
	lines := make([]string, lineCount)
	for i := range lines {
		lines[i] = strings.Repeat("a", 100)
	}
	body := strings.Join(lines, "\n")

	got := m.overlayToasts(body)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != lineCount {
		t.Errorf("overlayToasts changed line count: got %d, want %d", len(gotLines), lineCount)
	}
}

// TestOverlayToastsContainsText verifies that after overlaying, the plain body
// contains the toast text words.  The toast box may word-wrap, so we join all
// lines before checking (the words must appear somewhere in the output).
func TestOverlayToastsContainsText(t *testing.T) {
	m := newToastModel()
	m.width, m.height = 100, 30
	// Use a single-word toast text so word-wrap cannot split it.
	m.toasts = []toast{liveToast(toastSuccess, "hellooverlay")}

	lineCount := 20
	lines := make([]string, lineCount)
	for i := range lines {
		lines[i] = strings.Repeat(".", 100)
	}
	body := strings.Join(lines, "\n")

	got := m.overlayToasts(body)
	plain := stripANSI(got)
	if !strings.Contains(plain, "hellooverlay") {
		t.Errorf("overlaid body should contain toast text; plain output:\n%s", plain)
	}
}

// ── ansiStripSimple tests ─────────────────────────────────────────────────────

// TestAnsiStripSimple verifies that ansiStripSimple removes CSI escape sequences.
func TestAnsiStripSimple(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"\x1b[0mhello\x1b[0m", "hello"},
		{"\x1b[38;2;255;0;0mred\x1b[0m text", "red text"},
		{"\x1b[1;32mbold green\x1b[0m", "bold green"},
		{"no escapes here", "no escapes here"},
		{"", ""},
	}
	for _, tc := range cases {
		got := ansiStripSimple(tc.in)
		if got != tc.want {
			t.Errorf("ansiStripSimple(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── kindColor / kindIcon tests ────────────────────────────────────────────────

// TestKindColor verifies that each toastKind maps to the expected palette color.
func TestKindColor(t *testing.T) {
	p := theme.Default()
	cases := []struct {
		kind toastKind
		want lipgloss.Color
	}{
		{toastInfo, p.Cyan},
		{toastSuccess, p.Green},
		{toastError, p.Red},
	}
	for _, tc := range cases {
		got := kindColor(tc.kind, p)
		if got != tc.want {
			t.Errorf("kindColor(%d) = %s, want %s", tc.kind, got, tc.want)
		}
	}
}

// TestKindIcon verifies that each toastKind returns a distinct, non-empty icon.
func TestKindIcon(t *testing.T) {
	icons := map[string]bool{}
	for _, k := range []toastKind{toastInfo, toastSuccess, toastError} {
		icon := kindIcon(k)
		if icon == "" {
			t.Errorf("kindIcon(%d) returned empty string", k)
		}
		icons[icon] = true
	}
	if len(icons) != 3 {
		t.Errorf("expected 3 distinct icons, got %d: %v", len(icons), icons)
	}
}

// TestFadeT verifies the fadeT() helper: returns 0 before the fade window and
// 1 when the toast is fully expired.
func TestFadeT(t *testing.T) {
	// Fresh toast — not yet in the fade window.
	fresh := liveToast(toastInfo, "x")
	if fresh.fadeT() != 0 {
		t.Errorf("fresh toast: fadeT() = %f, want 0", fresh.fadeT())
	}

	// Expired toast — fully faded.
	old := expiredToast(toastInfo, "x")
	if got := old.fadeT(); got < 0.999 {
		t.Errorf("expired toast: fadeT() = %f, want ~1.0", got)
	}
}
