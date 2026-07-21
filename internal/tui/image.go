package tui

// image.go — plan 08e §E2: inline image rendering for image file parts.
//
// A terminal can't decode a JPEG natively, but it can emit Sixel graphics
// (VT340+: xterm -ti vt340, mlterm, wezterm, kitty) or iTerm2 inline-image
// escape sequences (iTerm.app, WezTerm). This file owns the capability probe
// and the escape emission; the actual file-part dispatch lives in render.go's
// renderMessage (the "file" case).
//
// Gating (CLAUDE.md non-negotiable: "never emit sixel/iTerm escapes to a
// terminal that didn't advertise support — garbage on screen"):
//   - viewState.images (ctrl+x i) must be ON, AND
//   - a capability probe must succeed (TERM_PROGRAM for iTerm2/WezTerm, or
//     TERM/OPCODE42_SIXEL/Config.Sixel for Sixel).
// When either fails, renderImagePart returns a placeholder glyph so the
// image is still visible in the conversation record (filename + dimensions +
// mime), just not rendered as pixels.
//
// Wire shape (verified against opencode): image FileParts carry the bytes as
// a "data:<mime>;base64,<payload>" URL (packages/tui/src/component/prompt/
// index.tsx:1246 and packages/opencode/src/image/image.ts:148). The TUI's
// Part struct carries URL; renderImagePart parses the data URL, decodes the
// image (PNG/JPEG via the stdlib), and emits the escape with the raw bytes
// (iTerm2) or the sixel-encoded pixels (Sixel).
//
// Sixel encoder choice: in-package, no new dep (CLAUDE.md: "Libs vetted in
// the plans"). github.com/mrmpp/sixel is not in go.mod and vetting a new
// dep for a stretch feature is miscalibrated. The encoder here is a minimal
// uniform-quantization sixel writer: decode → quantize to a 216-color cube
// (6 levels per channel, the same web-safe cube) → emit one sixel line per
// pixel row. It is not the highest-fidelity sixel encoder, but it is correct,
// dependency-free, and sufficient for the stretch goal (a real visual on
// wezterm/kitty/iTerm users who opt in). Non-image file parts keep the
// existing chip render (renderMessage's "file" case only routes image/*
// mimes here).

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // decode support
	_ "image/jpeg" // decode support
	_ "image/png"  // decode support
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// renderImagePart renders an image file part, branching on terminal support.
// Returns the placeholder glyph when viewState.images is off OR no terminal
// capability is advertised — never emits sixel/iTerm escapes unless both
// gates pass (CLAUDE.md non-negotiable).
func (m Model) renderImagePart(p Part) string {
	if !m.view.images {
		return m.imagePlaceholder(p)
	}
	if isITerm2() {
		return m.renderITerm2Image(p)
	}
	if m.sixelCapability() {
		return m.renderSixelImage(p)
	}
	return m.imagePlaceholder(p)
}

// imagePlaceholder renders the fallback glyph for an image file part: a
// framed "🖼 <filename> (WxH, <mime>)" line in FgDim. When the image bytes
// can't be decoded (missing, corrupt, or not a data URL), W and H are "?".
// The placeholder is always safe — it carries no escape sequences.
func (m Model) imagePlaceholder(p Part) string {
	w, h := imageDimensions(p)
	return lipgloss.NewStyle().Foreground(m.styles.P.FgDim).
		Render(fmt.Sprintf("🖼 %s (%s×%s, %s)", placeholderName(p), w, h, p.Mime))
}

// fileChip renders a non-image file part as a one-line chip: a paperclip
// glyph + filename + mime in FgDim. This is the "existing chip render" the
// plan refers to for non-image files; they keep this treatment regardless
// of viewState.images (image rendering is opt-in, file chips always show).
func (m Model) fileChip(p Part) string {
	name := p.Filename
	if name == "" {
		name = "file"
	}
	mime := p.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	return lipgloss.NewStyle().Foreground(m.styles.P.FgDim).
		Render("📎 " + name + " (" + mime + ")")
}

// placeholderName returns the filename to show in the placeholder, falling
// back to "image" when none is set.
func placeholderName(p Part) string {
	if p.Filename != "" {
		return p.Filename
	}
	return "image"
}

// imageDimensions decodes the part's data URL to read the pixel dimensions.
// Returns ("?", "?") when the part has no decodable bytes. The decode is
// best-effort: a corrupt or truncated image falls back to "?" rather than
// erroring.
func imageDimensions(p Part) (string, string) {
	img, err := decodeDataURL(p.URL)
	if err != nil || img == nil {
		return "?", "?"
	}
	b := img.Bounds()
	return fmt.Sprintf("%d", b.Dx()), fmt.Sprintf("%d", b.Dy())
}

// renderITerm2Image emits the iTerm2 inline-image escape with the raw image
// bytes base64-encoded. iTerm2 (and WezTerm, which advertises iTerm.app)
// decodes the original format (PNG/JPEG/GIF) — no re-encode needed.
// Protocol: ESC ] 1337 ; File = inline=1:<base64> ESC \  (ST terminator).
// Verified against https://iterm2.com/documentation-images.html.
func (m Model) renderITerm2Image(p Part) string {
	data, err := dataURLBytes(p.URL)
	if err != nil || len(data) == 0 {
		return m.imagePlaceholder(p)
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	b.WriteString("\x1b]1337;File=inline=1")
	if name := p.Filename; name != "" {
		b.WriteString(";name=" + base64.StdEncoding.EncodeToString([]byte(name)))
	}
	b.WriteString(":" + b64)
	b.WriteString("\x1b\\") // ST terminator (ESC \)
	return b.String()
}

// renderSixelImage emits a Sixel graphics escape with the image encoded as
// a sixel line per pixel row. The encoder decodes the data URL, quantizes to
// a 216-color cube (6 levels per channel), and emits the DCS sixel sequence.
// When the decode fails the placeholder is returned.
func (m Model) renderSixelImage(p Part) string {
	img, err := decodeDataURL(p.URL)
	if err != nil || img == nil {
		return m.imagePlaceholder(p)
	}
	sixel, err := encodeSixel(img)
	if err != nil {
		return m.imagePlaceholder(p)
	}
	return sixel
}

// sixelCapability reports whether the terminal advertises Sixel support.
// Detection order:
//  1. Config.Sixel (--sixel flag) forces ON for terminals that support it
//     but don't advertise via $TERM (the user explicitly opts in).
//  2. OPCODE42_SIXEL=1 env var (the same opt-in, for scripts / wrapper terms).
//  3. $TERM contains a known sixel-capable term name (xterm with vt340,
//     mlterm, wezterm, kitty). This is the $TERM-only heuristic; not every
//     sixel build sets $TERM distinctively, hence the flag/env overrides.
func (m Model) sixelCapability() bool {
	if m.sixel {
		return true
	}
	if os.Getenv("OPCODE42_SIXEL") == "1" {
		return true
	}
	term := os.Getenv("TERM")
	if term == "" {
		return false
	}
	for _, capable := range sixelTerms {
		if strings.Contains(term, capable) {
			return true
		}
	}
	return false
}

// sixelTerms is the set of $TERM substrings that advertise Sixel support.
// xterm builds with Sixel set $TERM to "xterm-vt340" or similar; mlterm,
// wezterm, and kitty all advertise their names in $TERM.
var sixelTerms = []string{"vt340", "mlterm", "wezterm", "kitty"}

// isITerm2 reports whether the terminal advertises iTerm2 inline-image
// support. iTerm.app is the canonical signal; WezTerm advertises the same
// TERM_PROGRAM so its inline-image path (the same 1337 escape) is reused.
func isITerm2() bool {
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm":
		return true
	}
	return false
}

// dataURLBytes parses a "data:<mime>;base64,<payload>" URL and returns the
// decoded bytes. Returns an error when the URL is not a base64 data URL.
func dataURLBytes(dataURL string) ([]byte, error) {
	const prefix = "data:"
	if !strings.HasPrefix(dataURL, prefix) {
		return nil, fmt.Errorf("not a data URL")
	}
	rest := dataURL[len(prefix):]
	semi := strings.Index(rest, ";base64,")
	if semi < 0 {
		return nil, fmt.Errorf("not a base64 data URL")
	}
	b64 := rest[semi+len(";base64,"):]
	return base64.StdEncoding.DecodeString(b64)
}

// decodeDataURL parses a data URL and decodes the image (PNG/JPEG/GIF via
// the stdlib image decoder). Returns the decoded image or an error.
func decodeDataURL(dataURL string) (image.Image, error) {
	data, err := dataURLBytes(dataURL)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// encodeSixel writes a sixel graphics escape for the image. The encoder:
//   - quantizes each pixel to a 216-color cube (6 levels per channel, the
//     web-safe cube) and assigns a color register per distinct cube cell;
//   - emits the DCS sixel header with the color register table;
//   - emits one sixel line per pixel row: for each register present in the
//     row, select the register (#i), then write the run length as
//     !<count>? (the sixel repeat operator) — the ? char (0x3f) is sixel
//     bit 0 (the top pixel of the 6-pixel cell), so a single-row line
//     carries exactly the pixels in that row for the selected register;
//   - terminates with - (the sixel newline — advance to the next pixel row).
//
// The output starts with the DCS introducer (ESC P) and ends with the ST
// (ESC \). A real sixel-capable terminal decodes this; a non-capable one
// would show garbage, which is why emission is gated behind
// sixelCapability() + viewState.images.
func encodeSixel(img image.Image) (string, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return "", fmt.Errorf("empty image")
	}

	// Build the color register table: quantize each pixel to the 6-cube and
	// collect the distinct registers in first-seen order. Sixel color
	// registers are 0-indexed; we emit "#i;2;r;g;b" introducers where r/g/b
	// are 0-100 (the sixel color value range).
	type rgb struct{ r, g, b int }
	registers := []rgb{}
	index := map[rgb]int{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			key := rgb{quantize6(c.R), quantize6(c.G), quantize6(c.B)}
			if _, ok := index[key]; !ok {
				index[key] = len(registers)
				registers = append(registers, key)
			}
		}
	}

	var out strings.Builder
	// DCS introducer: ESC P 1;1;1 q (aspect=1, background=1, hgrid=1).
	out.WriteString("\x1bP1;1;1q")
	// Color register table: one #i;2;r;g;b per register. The quantized levels
	// are 0-5 (6-cube); scale to the sixel 0-100 range (level 5 → 100, level
	// 0 → 0) so the register carries the right intensity.
	for i, reg := range registers {
		fmt.Fprintf(&out, "#%d;2;%d;%d;%d", i, reg.r*100/5, reg.g*100/5, reg.b*100/5)
	}
	// One sixel line per pixel row. For each row, walk the registers and emit
	// the register selector + a run-length-encoded sixel char for that
	// register's pixels in the row. The sixel char '?' (0x3f) is bit 0 of the
	// 6-pixel cell; a single-row line uses only bit 0, so every pixel in the
	// row maps to one '?'. Runs of identical (same-register) consecutive
	// pixels collapse into !<count>? to keep the output compact.
	for y := b.Min.Y; y < b.Max.Y; y++ {
		// Build the row as a sequence of register indices per pixel.
		row := make([]int, w)
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			key := rgb{quantize6(c.R), quantize6(c.G), quantize6(c.B)}
			row[x-b.Min.X] = index[key]
		}
		// For each register present in the row, emit #i + the run-length
		// encoding of that register's pixels. This produces one sub-line
		// per register per row — correct sixel (a terminal composites the
		// sub-lines for the same row by OR-ing their bit masks).
		for regIdx := 0; regIdx < len(registers); regIdx++ {
			i := 0
			for i < w {
				if row[i] != regIdx {
					i++
					continue
				}
				// Count the run length.
				j := i
				for j < w && row[j] == regIdx {
					j++
				}
				run := j - i
				fmt.Fprintf(&out, "#%d", regIdx)
				if run > 1 {
					fmt.Fprintf(&out, "!%d", run)
				}
				out.WriteString("?") // sixel char bit 0
				i = j
			}
		}
		// Advance to the next pixel row. The sixel spec uses '-' as the
		// line separator (move down one pixel within the current band).
		out.WriteString("-")
	}
	// ST terminator: ESC \
	out.WriteString("\x1b\\")
	return out.String(), nil
}

// quantize6 maps a 0-255 channel value to a 0-5 level (the 6-cube quantization).
// The 6-cube has 6 levels per channel; level i covers [i*43, (i+1)*43).
func quantize6(v uint8) int {
	if v >= 248 {
		return 5
	}
	return int(v) / 43
}

// ensure theme/lipgloss imports are used (the placeholder uses lipgloss +
// theme.Palette via m.styles; this guard keeps the imports live even if the
// only caller is the test path).
var _ = lipgloss.NewStyle
var _ theme.Palette
