package tui

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
)

// tinyPNG is a 2×2 solid-red PNG, base64-encoded. Generated once at init so
// the tests don't re-encode on every run; the bytes are deterministic.
var tinyPNGBase64 = func() string {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}()

func tinyPNGDataURL() string {
	return "data:image/png;base64," + tinyPNGBase64
}

// withEnv sets env vars for the duration of a test and restores them on
// cleanup. t.Setenv would be cleaner but we set multiple vars and want a
// single helper; the manual cleanup restores the prior value (or unsets).
func withEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for k, v := range vars {
		old, ok := os.LookupEnv(k)
		_ = os.Setenv(k, v)
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(k, old)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}

// TestRenderImagePart_PlaceholderWhenDisabled asserts that when
// viewState.images is OFF (the default), an image file part renders as the
// placeholder glyph — never as a sixel/iTerm escape. This is the CLAUDE.md
// non-negotiable: never emit graphics escapes unless explicitly enabled.
func TestRenderImagePart_PlaceholderWhenDisabled(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	m.view.images = false // default
	p := Part{ID: "prt_1", Type: "file", Mime: "image/png", Filename: "logo.png", URL: tinyPNGDataURL()}
	out := m.renderImagePart(p)
	if !strings.Contains(out, "🖼") {
		t.Fatalf("disabled images should render placeholder glyph, got: %q", out)
	}
	if strings.Contains(out, "\x1bP") {
		t.Fatalf("disabled images must not emit sixel DCS, got: %q", out)
	}
	if strings.Contains(out, "\x1b]1337") {
		t.Fatalf("disabled images must not emit iTerm2 escape, got: %q", out)
	}
	if !strings.Contains(out, "logo.png") {
		t.Fatalf("placeholder should show filename, got: %q", out)
	}
	if !strings.Contains(out, "image/png") {
		t.Fatalf("placeholder should show mime, got: %q", out)
	}
	// The placeholder shows dimensions decoded from the data URL (2×2).
	if !strings.Contains(out, "2×2") {
		t.Fatalf("placeholder should show decoded dimensions 2×2, got: %q", out)
	}
}

// TestRenderImagePart_PlaceholderWhenUnsupported asserts that when
// viewState.images is ON but no terminal capability is advertised (no
// TERM_PROGRAM, no $TERM match, no --sixel flag), the part falls back to
// the placeholder glyph. No sixel/iTerm escapes leak to an unsupported
// terminal (garbage on screen).
func TestRenderImagePart_PlaceholderWhenUnsupported(t *testing.T) {
	withEnv(t, map[string]string{"TERM_PROGRAM": "", "TERM": "xterm-256color", "OPCODE42_SIXEL": ""})
	m := New(Config{URL: "http://x"})
	m.width = 80
	m.view.images = true
	m.sixel = false
	p := Part{ID: "prt_1", Type: "file", Mime: "image/png", Filename: "logo.png", URL: tinyPNGDataURL()}
	out := m.renderImagePart(p)
	if !strings.Contains(out, "🖼") {
		t.Fatalf("unsupported terminal should fall back to placeholder, got: %q", out)
	}
	if strings.Contains(out, "\x1bP") {
		t.Fatalf("unsupported terminal must not emit sixel, got: %q", out)
	}
	if strings.Contains(out, "\x1b]1337") {
		t.Fatalf("unsupported terminal must not emit iTerm2 escape, got: %q", out)
	}
}

// TestRenderImagePart_ITerm2Escape asserts that when viewState.images is ON
// and TERM_PROGRAM == "iTerm.app", the part emits the iTerm2 inline-image
// escape (ESC ] 1337 ; File = inline=1 : <base64> ESC \). The base64 payload
// must be the raw image bytes (the data URL decoded), not re-encoded.
func TestRenderImagePart_ITerm2Escape(t *testing.T) {
	withEnv(t, map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM": "xterm-256color"})
	m := New(Config{URL: "http://x"})
	m.width = 80
	m.view.images = true
	p := Part{ID: "prt_1", Type: "file", Mime: "image/png", Filename: "logo.png", URL: tinyPNGDataURL()}
	out := m.renderImagePart(p)
	if !strings.HasPrefix(out, "\x1b]1337;File=inline=1") {
		t.Fatalf("iTerm2 escape should start with ESC ] 1337 ; File=inline=1, got: %q", out)
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("iTerm2 escape should end with ST (ESC \\), got: %q", out)
	}
	if !strings.Contains(out, tinyPNGBase64) {
		t.Fatalf("iTerm2 escape should carry the raw image base64, got: %q", out)
	}
	if strings.Contains(out, "🖼") {
		t.Fatalf("iTerm2 path should not fall back to placeholder, got: %q", out)
	}
}

// TestRenderImagePart_SixelEscape asserts that when viewState.images is ON
// and the --sixel flag forces Sixel capability, the part emits a sixel DCS
// escape (starts with ESC P). The output must not be the placeholder.
func TestRenderImagePart_SixelEscape(t *testing.T) {
	withEnv(t, map[string]string{"TERM_PROGRAM": "", "TERM": "xterm-256color", "OPCODE42_SIXEL": ""})
	m := New(Config{URL: "http://x", Sixel: true})
	m.width = 80
	m.view.images = true
	p := Part{ID: "prt_1", Type: "file", Mime: "image/png", Filename: "logo.png", URL: tinyPNGDataURL()}
	out := m.renderImagePart(p)
	if !strings.HasPrefix(out, "\x1bP") {
		t.Fatalf("sixel escape should start with DCS (ESC P), got: %q", out)
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("sixel escape should end with ST (ESC \\), got: %q", out)
	}
	if strings.Contains(out, "🖼") {
		t.Fatalf("sixel path should not fall back to placeholder, got: %q", out)
	}
}

// TestRenderImagePart_SixelFromEnv asserts OPCODE42_SIXEL=1 enables sixel
// without the --sixel flag (the env-var opt-in for scripts / wrapper terms).
func TestRenderImagePart_SixelFromEnv(t *testing.T) {
	withEnv(t, map[string]string{"TERM_PROGRAM": "", "TERM": "xterm-256color", "OPCODE42_SIXEL": "1"})
	m := New(Config{URL: "http://x"})
	m.width = 80
	m.view.images = true
	m.sixel = false
	p := Part{ID: "prt_1", Type: "file", Mime: "image/png", URL: tinyPNGDataURL()}
	out := m.renderImagePart(p)
	if !strings.HasPrefix(out, "\x1bP") {
		t.Fatalf("OPCODE42_SIXEL=1 should emit sixel, got: %q", out)
	}
}

// TestRenderImagePart_SixelFromTerm asserts a $TERM advertising a sixel-
// capable terminal (wezterm) enables sixel without the flag or env var.
func TestRenderImagePart_SixelFromTerm(t *testing.T) {
	withEnv(t, map[string]string{"TERM_PROGRAM": "", "TERM": "wezterm", "OPCODE42_SIXEL": ""})
	m := New(Config{URL: "http://x"})
	m.width = 80
	m.view.images = true
	p := Part{ID: "prt_1", Type: "file", Mime: "image/png", URL: tinyPNGDataURL()}
	out := m.renderImagePart(p)
	if !strings.HasPrefix(out, "\x1bP") {
		t.Fatalf("$TERM=wezterm should emit sixel, got: %q", out)
	}
}

// TestFileChip_NonImage asserts non-image file parts render as a chip
// (paperclip + filename + mime) regardless of viewState.images — the chip
// is always shown; only image rendering is opt-in.
func TestFileChip_NonImage(t *testing.T) {
	m := New(Config{URL: "http://x"})
	m.width = 80
	p := Part{ID: "prt_1", Type: "file", Mime: "text/plain", Filename: "notes.md"}
	out := m.fileChip(p)
	if !strings.Contains(out, "📎") {
		t.Fatalf("file chip should show paperclip glyph, got: %q", out)
	}
	if !strings.Contains(out, "notes.md") {
		t.Fatalf("file chip should show filename, got: %q", out)
	}
	if !strings.Contains(out, "text/plain") {
		t.Fatalf("file chip should show mime, got: %q", out)
	}
}

// TestFileChip_FallsBackWhenNoFilename asserts the chip falls back to
// "file" when no filename is set, and "application/octet-stream" when no
// mime is set.
func TestFileChip_FallsBackWhenNoFilename(t *testing.T) {
	m := New(Config{URL: "http://x"})
	out := m.fileChip(Part{Type: "file"})
	if !strings.Contains(out, "📎 file (application/octet-stream)") {
		t.Fatalf("chip should fall back to 'file' + octet-stream, got: %q", out)
	}
}

// TestEncodeSixel_Shape asserts the sixel encoder produces a well-formed
// escape: DCS introducer (ESC P), a color register for the single red pixel,
// sixel data lines, and the ST terminator (ESC \). A 2×2 red image should
// produce exactly one color register (red) and two sixel lines.
func TestEncodeSixel_Shape(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	out, err := encodeSixel(img)
	if err != nil {
		t.Fatalf("encodeSixel failed: %v", err)
	}
	if !strings.HasPrefix(out, "\x1bP") {
		t.Fatalf("sixel should start with DCS (ESC P), got: %q", out)
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("sixel should end with ST (ESC \\), got: %q", out)
	}
	// One color register for the red pixels: #0;2;100;0;0 (r=255→100%).
	if !strings.Contains(out, "#0;2;100;0;0") {
		t.Fatalf("sixel should emit a red color register, got: %q", out)
	}
	// Two sixel lines (one per pixel row), each terminated by '-'.
	// The run-length form should collapse the 2-pixel run into !2?.
	if !strings.Contains(out, "!2?") {
		t.Fatalf("sixel should RLE the 2-pixel run as !2?, got: %q", out)
	}
}

// TestImageDimensions_DecodesFromDataURL asserts imageDimensions reads the
// pixel size from the data URL and returns "?" for undecodable parts.
func TestImageDimensions_DecodesFromDataURL(t *testing.T) {
	w, h := imageDimensions(Part{URL: tinyPNGDataURL()})
	if w != "2" || h != "2" {
		t.Fatalf("dimensions should be 2×2, got %s×%s", w, h)
	}
	w, h = imageDimensions(Part{URL: "not a data url"})
	if w != "?" || h != "?" {
		t.Fatalf("undecodable should be ?×?, got %s×%s", w, h)
	}
}

// TestIsITerm2_Detection asserts the iTerm2 probe matches iTerm.app and
// WezTerm and rejects other TERM_PROGRAM values.
func TestIsITerm2_Detection(t *testing.T) {
	withEnv(t, map[string]string{"TERM_PROGRAM": "iTerm.app"})
	if !isITerm2() {
		t.Fatal("iTerm.app should be detected as iTerm2")
	}
	withEnv(t, map[string]string{"TERM_PROGRAM": "WezTerm"})
	if !isITerm2() {
		t.Fatal("WezTerm should be detected as iTerm2 (reuses 1337 escape)")
	}
	withEnv(t, map[string]string{"TERM_PROGRAM": "Apple_Terminal"})
	if isITerm2() {
		t.Fatal("Apple_Terminal should not be detected as iTerm2")
	}
}

// TestSixelCapability_FlagForcesOn asserts the --sixel flag (Config.Sixel)
// forces sixel capability on even when $TERM doesn't advertise it.
func TestSixelCapability_FlagForcesOn(t *testing.T) {
	withEnv(t, map[string]string{"TERM": "xterm-256color", "OPCODE42_SIXEL": ""})
	m := New(Config{URL: "http://x", Sixel: true})
	if !m.sixelCapability() {
		t.Fatal("--sixel flag should force sixel capability on")
	}
}

// TestRenderMessage_FilePartRoutes asserts renderMessage dispatches file
// parts to renderImagePart (image/*) or fileChip (non-image), and that the
// result appears in the rendered block.
func TestRenderMessage_FilePartRoutes(t *testing.T) {
	m := New(Config{URL: "http://x", SessionID: "ses_1"})
	m.screen = ScreenSession
	m.width = 80
	m.store.messages["ses_1"] = []Message{{ID: "msg_1", SessionID: "ses_1", Role: "user"}}
	m.store.parts["msg_1"] = []Part{
		{ID: "prt_1", MessageID: "msg_1", Type: "file", Mime: "image/png", Filename: "logo.png", URL: tinyPNGDataURL()},
		{ID: "prt_2", MessageID: "msg_1", Type: "file", Mime: "text/plain", Filename: "notes.md"},
	}
	out := m.renderMessage(m.store.messages["ses_1"][0], m.store.parts["msg_1"])
	// Image part → placeholder (images off by default).
	if !strings.Contains(out, "🖼") {
		t.Fatalf("image file part should render placeholder (images off), got: %q", out)
	}
	// Non-image part → chip.
	if !strings.Contains(out, "📎 notes.md") {
		t.Fatalf("non-image file part should render chip, got: %q", out)
	}
}
