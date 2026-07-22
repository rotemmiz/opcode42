package tui

// markdown.go — Plan 08c M4: theme-driven glamour markdown rendering.
//
// WHY glamour: it is the canonical Charm-family markdown renderer that
// composes naturally with Bubble Tea + Lipgloss — same author, same ANSI
// model. Alternatives (blackfriday, goldmark plain) have no styled output;
// a hand-rolled ANSI renderer would duplicate glamour's goldmark integration.
//
// WHY a custom StyleConfig: glamour's built-in dark/light styles use hard-
// coded colors that bear no relation to the active Opcode42 palette. opencode
// drives all markdown colors from theme tokens (markdownHeading etc.); Opcode42
// must do the same so that a theme switch re-colors prose immediately.
//
// WHY a cache: glamour constructs a new goldmark parser + renderer and runs a
// full markdown parse + ANSI codegen every call. The streaming assistant
// transcript re-renders every frame (each new token triggers Update → View).
// Without caching, every frame re-renders every previous text part from
// scratch — O(n) glamour calls per frame where n grows with message length.
// The cache key is (text, width, themeName) so:
//   - A width change (terminal resize) produces a fresh render at the new width.
//   - A theme switch invalidates all previous renders (different colors).
//   - Identical text at the same width + theme serves directly from the map.
//
// WHY background fill: glamour emits ANSI SGR spans that reset to terminal
// default at the end of each styled run (\x1b[0m). Lipgloss does NOT re-apply
// a parent Background through those inner resets — each sub-span terminates
// with a bare default, leaving the rest of its terminal row transparent.
// On light terminals this means the row's trailing cells use the terminal's
// white background → visible bleed behind dark-themed text. The fix: pad
// every output line to contentWidth with a Background(p.Bg) Lipgloss style,
// which emits explicit bg SGR for the padded cells. See Tier 0 plan notes
// and viewSplash/applyTheme for the same pattern applied elsewhere.

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// mdCacheKey is the composite cache key for a rendered markdown block.
// All three fields are intentionally included:
//   - textHash: a SHA-256 prefix prevents storing unbounded keys in the map
//     while still being collision-resistant enough for a UI cache.
//   - width: glamour word-wraps at this column; a resize must re-render.
//   - themeName: a theme switch changes all colors; cached output is stale.
type mdCacheKey struct {
	textHash  string
	width     int
	themeName string
}

// mdCacheEntry holds the rendered string for a cache hit.
type mdCacheEntry struct {
	rendered string
}

// mdCache is the model-level render cache.  It is a plain map (not an LRU)
// because the number of distinct (text,width,theme) triples in a typical
// session is bounded by the number of assistant text parts (dozens, not
// thousands) — a plain map is both simpler and faster for this scale.
// Under a theme switch all entries for the old theme are effectively dead;
// they will be overwritten by new entries under the new themeName key, and
// the old entries are collected on the next GC cycle.  The total map size
// stays proportional to (messages × themes seen) which is small.
type mdCache map[mdCacheKey]mdCacheEntry

// mdBlockCacheKey is the incremental per-stable-block cache key (plan 17 §D3).
// Streaming markdown grows one delta at a time; rendering the whole part with
// glamour on every delta is O(n²) over the stream (each frame re-parses every
// prior block). The incremental cache stores one entry per stable block — a
// markdown block (paragraph, heading, list, code fence) followed by a blank
// line or end-of-text — keyed by (partID, blockIdx, width, theme). Only the
// trailing partial block (no trailing blank line, the "streaming block") is
// re-rendered each frame; stable blocks serve straight from the cache.
//
// Mirrors opencode's commitMarkdownBlocks + _stableBlockCount
// (run/scrollback.surface.ts:287-305): only new stable blocks are committed;
// the trailing streaming block re-settles each frame.
type mdBlockCacheKey struct {
	partID    string
	blockIdx  int
	width     int
	themeName string
}

// mdBlockCache is the per-stable-block cache. Like mdCache it is a plain map
// (reference type) shared across Model copies via the same underlying map.
// An entry is the rendered ANSI string for one stable block (no trailing
// newline; the caller joins blocks with "\n\n"). Block indexes are stable:
// block 0 is always the first blank-line-terminated block in the part's
// accumulated text, so a hit on (partID, blockIdx, width, theme) means the
// rendered output is current — no need to invalidate on text growth.
type mdBlockCache map[mdBlockCacheKey]string

// sp returns a *string holding s — glamour's StylePrimitive fields use
// pointer semantics so that zero (unset) differs from an explicit value.
func sp(s string) *string { return &s }

// bp returns a *bool holding b — same reason.
func bp(b bool) *bool { return &b }

// colStr converts a theme.Color (which is a string typedef) to a plain
// *string for glamour's StylePrimitive.Color fields. Empty color (zero value)
// is returned as nil so glamour inherits from the parent element instead of
// emitting an empty SGR sequence.
func colStr(c theme.Color) *string {
	if string(c) == "" {
		return nil
	}
	s := string(c)
	return &s
}

// uintPtr returns a *uint — glamour's Margin/Indent fields are *uint.
func uintPtr(u uint) *uint { return &u }

// buildStyleConfig builds a glamour ansi.StyleConfig that derives every color
// from the active theme's MarkdownPalette + SyntaxPalette.  The mapping table
// is documented alongside each field below; it mirrors opencode's token names
// so that swapping a theme JSON produces the correct palette.
//
// Token mapping (opencode name → MarkdownPalette field → glamour key):
//
//	markdownText        → p.Markdown.Text        → Document.Color, Text
//	markdownHeading     → p.Markdown.Heading     → Heading.Color (bold)
//	markdownLink        → p.Markdown.Link        → Link.Color (underline)
//	markdownLinkText    → p.Markdown.LinkText    → LinkText.Color
//	markdownCode        → p.Markdown.Code        → Code.Color
//	markdownCodeBlock   → p.Markdown.CodeBlock   → CodeBlock.Color
//	markdownBlockQuote  → p.Markdown.BlockQuote  → BlockQuote.Color
//	markdownEmph        → p.Markdown.Emph        → Emph.Color (italic)
//	markdownStrong      → p.Markdown.Strong      → Strong.Color (bold)
//	markdownHorizontalRule → p.Markdown.HorizontalRule → HorizontalRule.Color
//	markdownListItem    → p.Markdown.ListItem    → Item.Color
//	markdownListEnumeration → p.Markdown.ListEnumeration → Enumeration.Color
//	markdownImage       → p.Markdown.Image       → Image.Color
//	markdownImageText   → p.Markdown.ImageText   → ImageText.Color
//	background          → p.Bg                   → Document.BackgroundColor (anti-bleed)
//
// Syntax (for chroma code blocks):
//
//	syntaxComment    → p.Syntax.Comment   → Chroma.Comment
//	syntaxKeyword    → p.Syntax.Keyword   → Chroma.Keyword + KeywordReserved + KeywordNamespace
//	syntaxFunction   → p.Syntax.Function  → Chroma.NameFunction + Name
//	syntaxType       → p.Syntax.Type      → Chroma.KeywordType + NameClass
//	syntaxString     → p.Syntax.String    → Chroma.LiteralString
//	syntaxNumber     → p.Syntax.Number    → Chroma.LiteralNumber
//	syntaxOperator   → p.Syntax.Operator  → Chroma.Operator
//	syntaxPunctuation → p.Syntax.Punctuation → Chroma.Punctuation
//	syntaxVariable   → p.Syntax.Variable  → Chroma.Name (general identifier)
func buildStyleConfig(p theme.Palette) gansi.StyleConfig {
	md := p.Markdown
	sy := p.Syntax
	bgHex := string(p.Bg)

	return gansi.StyleConfig{
		// Document is the outermost container.  BackgroundColor is set to
		// the theme Bg so glamour paints the background of the document block;
		// this helps, though we still pad lines ourselves (see renderMarkdown).
		Document: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{
				BlockPrefix:     "\n",
				BlockSuffix:     "\n",
				Color:           colStr(md.Text),
				BackgroundColor: &bgHex,
			},
			Margin: uintPtr(0), // we handle width ourselves via contentWidth
		},

		// BlockQuote: indent with a pipe, colored in BlockQuote token.
		BlockQuote: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{
				Color: colStr(md.BlockQuote),
			},
			Indent:      uintPtr(1),
			IndentToken: sp("│ "),
		},

		// Paragraph: no special treatment beyond Document color.
		Paragraph: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{},
		},

		// List: use a small indent consistent with the stream column width.
		List: gansi.StyleList{
			StyleBlock: gansi.StyleBlock{
				StylePrimitive: gansi.StylePrimitive{
					Color: colStr(md.ListItem),
				},
			},
			LevelIndent: 2,
		},

		// Heading: base heading style (applies to all h1–h6 unless overridden).
		// Bold + BlockSuffix newline to visually separate the heading from body.
		Heading: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       colStr(md.Heading),
				Bold:        bp(true),
			},
		},
		// H1–H6: prefix markers; h1 gets extra bold emphasis.
		H1: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "# ", Bold: bp(true)}},
		H2: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "## "}},
		H3: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "### "}},
		H4: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "#### "}},
		H5: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "##### "}},
		H6: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Prefix: "###### "}},

		// Inline text styles.
		Text: gansi.StylePrimitive{Color: colStr(md.Text)},
		Strikethrough: gansi.StylePrimitive{
			CrossedOut: bp(true),
		},
		Emph: gansi.StylePrimitive{
			Color:  colStr(md.Emph),
			Italic: bp(true),
		},
		Strong: gansi.StylePrimitive{
			Color: colStr(md.Strong),
			Bold:  bp(true),
		},
		HorizontalRule: gansi.StylePrimitive{
			Color:  colStr(md.HorizontalRule),
			Format: "\n────────────────────────────────────────\n",
		},

		// List item markers.
		Item: gansi.StylePrimitive{
			BlockPrefix: "• ",
			Color:       colStr(md.ListItem),
		},
		Enumeration: gansi.StylePrimitive{
			BlockPrefix: ". ",
			Color:       colStr(md.ListEnumeration),
		},
		Task: gansi.StyleTask{
			StylePrimitive: gansi.StylePrimitive{},
			Ticked:         "[✓] ",
			Unticked:       "[ ] ",
		},

		// Links: URL part underlined, link text without underline.
		Link: gansi.StylePrimitive{
			Color:     colStr(md.Link),
			Underline: bp(true),
		},
		LinkText: gansi.StylePrimitive{
			Color: colStr(md.LinkText),
		},

		// Images (displayed as alt-text when terminal can't show images).
		Image: gansi.StylePrimitive{
			Color:     colStr(md.Image),
			Underline: bp(true),
		},
		ImageText: gansi.StylePrimitive{
			Color:  colStr(md.ImageText),
			Format: "Image: {{.text}} →",
		},

		// Inline code span.
		Code: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{
				Color: colStr(md.Code),
			},
		},

		// Fenced code block with chroma syntax highlighting sourced from the
		// theme's SyntaxPalette. The Background chroma primitive sets the code
		// block's own background to p.Bg so no terminal bleed within the block.
		CodeBlock: gansi.StyleCodeBlock{
			StyleBlock: gansi.StyleBlock{
				StylePrimitive: gansi.StylePrimitive{
					Color:           colStr(md.CodeBlock),
					BackgroundColor: &bgHex,
				},
				Margin: uintPtr(0),
			},
			Chroma: &gansi.Chroma{
				Text: gansi.StylePrimitive{
					Color: colStr(md.CodeBlock),
				},
				Error: gansi.StylePrimitive{
					Color: colStr(p.Red),
				},
				Comment: gansi.StylePrimitive{
					Color: colStr(sy.Comment),
				},
				CommentPreproc: gansi.StylePrimitive{
					Color: colStr(sy.Keyword),
				},
				Keyword: gansi.StylePrimitive{
					Color: colStr(sy.Keyword),
				},
				KeywordReserved: gansi.StylePrimitive{
					Color: colStr(sy.Keyword),
				},
				KeywordNamespace: gansi.StylePrimitive{
					Color: colStr(sy.Keyword),
				},
				KeywordType: gansi.StylePrimitive{
					Color: colStr(sy.Type),
				},
				Operator: gansi.StylePrimitive{
					Color: colStr(sy.Operator),
				},
				Punctuation: gansi.StylePrimitive{
					Color: colStr(sy.Punctuation),
				},
				Name: gansi.StylePrimitive{
					Color: colStr(sy.Variable),
				},
				NameBuiltin: gansi.StylePrimitive{
					Color: colStr(sy.Function),
				},
				NameClass: gansi.StylePrimitive{
					Color: colStr(sy.Type),
				},
				NameFunction: gansi.StylePrimitive{
					Color: colStr(sy.Function),
				},
				NameDecorator: gansi.StylePrimitive{
					Color: colStr(sy.Function),
				},
				LiteralNumber: gansi.StylePrimitive{
					Color: colStr(sy.Number),
				},
				LiteralString: gansi.StylePrimitive{
					Color: colStr(sy.String),
				},
				LiteralStringEscape: gansi.StylePrimitive{
					Color: colStr(sy.Operator),
				},
				GenericDeleted: gansi.StylePrimitive{
					Color: colStr(p.Red),
				},
				GenericEmph: gansi.StylePrimitive{
					Italic: bp(true),
				},
				GenericInserted: gansi.StylePrimitive{
					Color: colStr(p.Green),
				},
				GenericStrong: gansi.StylePrimitive{
					Bold: bp(true),
				},
				GenericSubheading: gansi.StylePrimitive{
					Color: colStr(md.Heading),
				},
				// Background for the code block itself.
				Background: gansi.StylePrimitive{
					BackgroundColor: &bgHex,
				},
			},
		},

		// Table: use default separators; glamour draws a plain text table.
		Table: gansi.StyleTable{
			StyleBlock: gansi.StyleBlock{
				StylePrimitive: gansi.StylePrimitive{
					Color: colStr(md.Text),
				},
			},
			CenterSeparator: sp("┼"),
			ColumnSeparator: sp("│"),
			RowSeparator:    sp("─"),
		},
	}
}

// newMarkdownRenderer constructs a glamour TermRenderer configured for the
// given palette and content width.  Called once per (palette, width) pair by
// renderMarkdown; the result is used immediately and not stored — caching is
// done at the rendered-string level (mdCache) which is cheaper than caching
// live TermRenderer instances.
func newMarkdownRenderer(p theme.Palette, width int) (*glamour.TermRenderer, error) {
	if width <= 0 {
		width = 80
	}
	return glamour.NewTermRenderer(
		glamour.WithStyles(buildStyleConfig(p)),
		glamour.WithWordWrap(width),
	)
}

// hashText returns a short hex prefix of the SHA-256 of s — used as the
// text component of the cache key.  A 16-byte (128-bit) prefix is more than
// adequate for collision resistance in a UI cache keyed by short text strings.
func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:16])
}

// renderMarkdown renders text as markdown using glamour, themed from the active
// palette.  Results are cached by (text, width, themeName) so repeated renders
// of the same part (the streaming transcript re-renders every frame) are free.
//
// Receiver is value (not pointer) like all other Model view methods; the cache
// map survives copies because Go maps are reference types — all Model copies
// derived from the same root share the same underlying map.  The cache is
// initialised on first use via ensureMDCache() which writes through the shared
// reference.
//
// Anti-bleed: glamour emits ANSI SGR resets inside spans, which terminate the
// background color for the rest of that terminal row.  On light terminals this
// leaves the row tail transparent — the terminal's white background shows
// through behind dark-themed text.  We counter this by padding every line that
// is shorter than width to exactly width cells using a lipgloss style whose
// Background is p.Bg.  This ensures every row is fully painted to the theme
// background, matching the full-canvas paint that View() enforces at the outer
// level (bgfill_test.go regression guard).
//
// For streaming parts (text/reasoning whose content grows one delta at a time)
// prefer renderMarkdownStreaming, which keys on (partID, blockIdx, width, theme)
// and only re-renders the trailing partial block each frame — avoiding the
// O(n²) full-text re-parse this path incurs on every delta (plan 17 §D3).
func (m Model) renderMarkdown(text string) string {
	width := m.contentWidth()
	key := mdCacheKey{
		textHash:  hashText(text),
		width:     width,
		themeName: m.themeName,
	}

	// Fast path: return cached render.
	if entry, ok := m.mdCache[key]; ok {
		return entry.rendered
	}

	rendered := m.renderMarkdownUncached(text, width)

	// Store in cache: m.mdCache is a map (reference type) so this write is
	// visible through all copies of the Model that share this map.
	// ensureMDCache must have been called on the *original* Model before View
	// is called; New() calls it in the constructor.
	if m.mdCache != nil {
		m.mdCache[key] = mdCacheEntry{rendered: rendered}
	}

	return rendered
}

// renderMarkdownUncached runs glamour over text and applies the anti-bleed
// background fill. Shared by the full-text cache path (renderMarkdown) and
// the incremental streaming path (renderMarkdownStreaming), which use the
// same renderer config + bg fill but different cache strategies.
func (m Model) renderMarkdownUncached(text string, width int) string {
	// Build a renderer for the active palette + width.
	r, err := newMarkdownRenderer(m.styles.P, width)
	if err != nil {
		// Renderer construction should never fail (no I/O, no file reads);
		// if it does, fall back to plain text so the UI stays up.
		return text
	}

	rendered, err := r.Render(text)
	if err != nil {
		return text
	}

	// Trim the trailing newlines glamour appends — they appear as blank lines
	// between consecutive prose parts and make the stream look double-spaced.
	rendered = strings.TrimRight(rendered, "\n")

	// Background fill: pad each line to width with the theme Bg so no
	// transparent trailing cells are left after glamour's inner SGR resets.
	//
	// Design choice: we use lipgloss.NewStyle().Background().Width() to pad
	// rather than manual space-stuffing, so we benefit from lipgloss's own
	// ANSI-width accounting (handles multi-byte / wide characters correctly).
	bgFill := lipgloss.NewStyle().
		Background(m.styles.P.Bg).
		Foreground(m.styles.P.Fg).
		Width(width)

	lines := strings.Split(rendered, "\n")
	filled := make([]string, len(lines))
	for i, line := range lines {
		filled[i] = bgFill.Render(line)
	}
	return strings.Join(filled, "\n")
}

// renderMarkdownStreaming is the incremental streaming render path (plan 17
// §D3). For a part whose text is growing one delta at a time, the full-text
// mdCache misses every frame (the SHA-256 key changes on every append) →
// O(n²) glamour parses over the whole stream. This path keys instead on
// (partID, blockIdx, width, theme) and only re-renders the trailing partial
// block each frame; stable blocks (terminated by a blank line) serve
// straight from the per-block cache.
//
// Mirrors opencode's commitMarkdownBlocks + _stableBlockCount
// (run/scrollback.surface.ts:287-305): only new stable blocks are committed;
// the trailing streaming block re-settles each frame.
//
// Block splitting: a "stable block" is one or more consecutive non-blank
// lines (paragraph, heading, list, code fence, …) followed by a blank line
// or end-of-text. The trailing partial block (no trailing blank line) is the
// "streaming block" — it re-renders each call. Once a blank line arrives, the
// streaming block finalizes into a new stable block and is cached under its
// block index; subsequent calls serve it from the cache. Block indexes are
// stable: block 0 is always the first blank-line-terminated block in the
// accumulated text, so (partID, blockIdx) is a stable identifier regardless
// of how much text is appended later.
//
// Fallback: when partID is empty (no streaming context — e.g. a one-off
// render) this falls back to the full-text cache. The benchmark in
// markdown_test.go (BenchmarkRenderMarkdown_StreamingPart) exercises the
// incremental path and asserts sub-quadratic scaling over a growing text.
func (m Model) renderMarkdownStreaming(partID, text string) string {
	if partID == "" {
		return m.renderMarkdown(text)
	}
	width := m.contentWidth()
	stable, streaming := splitMarkdownBlocks(text)

	// Stable blocks: serve from the per-block cache; render on miss.
	out := make([]string, 0, len(stable)+1)
	for i, blk := range stable {
		key := mdBlockCacheKey{partID: partID, blockIdx: i, width: width, themeName: m.themeName}
		if m.mdBlockCache != nil {
			if cached, ok := m.mdBlockCache[key]; ok {
				out = append(out, cached)
				continue
			}
		}
		rendered := m.renderMarkdownUncached(blk, width)
		if m.mdBlockCache != nil {
			m.mdBlockCache[key] = rendered
		}
		out = append(out, rendered)
	}

	// Trailing streaming block: re-render each call (it changes every frame).
	// The streaming block's text is hashed for a tiny one-shot cache so a
	// re-render of the SAME streaming text (e.g. View() called twice on the
	// same Model state) is free; the next delta invalidates it. This mirrors
	// opencode's "the trailing streaming block re-settles each frame".
	if streaming != "" {
		streamKey := mdCacheKey{
			textHash:  hashText(streaming),
			width:     width,
			themeName: m.themeName,
		}
		if m.mdCache != nil {
			if cached, ok := m.mdCache[streamKey]; ok {
				out = append(out, cached.rendered)
			} else {
				rendered := m.renderMarkdownUncached(streaming, width)
				m.mdCache[streamKey] = mdCacheEntry{rendered: rendered}
				out = append(out, rendered)
			}
		} else {
			out = append(out, m.renderMarkdownUncached(streaming, width))
		}
	}

	return strings.Join(out, "\n\n")
}

// splitMarkdownBlocks splits text into stable blocks + a trailing streaming
// block. A "stable block" is one or more consecutive non-blank lines followed
// by a blank line. The trailing partial block (no trailing blank line) is the
// "streaming block" — the part that is still accumulating deltas.
//
// Mirrors opencode's _stableBlockCount semantics (run/scrollback.surface.ts).
// Returns (stable, streaming) where stable is a slice of block texts (each
// trimmed of leading/trailing blank lines) and streaming is the trailing
// partial block (or "" when the text ends on a blank line — nothing is
// currently streaming).
//
// Fence-aware: a line starting with ``` (3+ backticks) opens a fenced code
// block; blank lines INSIDE the fence do NOT finalize the block. Without this
// guard, a code fence with an internal blank line (e.g. a function with a
// blank line between statements) would be split mid-fence, and the two pieces
// would each render as broken markdown (an unclosed fence + a stray closing
// fence). The fence is tracked by counting ``` openings: an odd count means
// we're inside a fence; the closing ``` flips it back. This is a simple
// heuristic (it doesn't honor indented code blocks or tilde fences) but
// covers the common case for streaming assistant output.
//
// Example:
//
//	"# H1\n\nbody para\n\nstill streaming…" →
//	  stable: ["# H1", "body para"], streaming: "still streaming…"
//
// The blank lines finalize each prior block; the missing trailing blank line
// keeps "still streaming…" as the streaming block (it is not yet finalized).
func splitMarkdownBlocks(text string) (stable []string, streaming string) {
	if text == "" {
		return nil, ""
	}
	// Walk the text line-by-line, grouping non-blank runs into blocks. A
	// block is finalized when a blank line is seen OUTSIDE a code fence; the
	// block (including any intra-block blank lines that are part of a
	// multi-line construct like a code fence) becomes a stable block.
	lines := strings.Split(text, "\n")
	var blocks []string
	var cur []string
	inFence := false
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		// A line starting with ``` (possibly with a language tag) toggles the
		// fence state. We count openings so a stray ``` inside a non-fence
		// paragraph still toggles — heuristic, but covers the common case.
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
		}
		isBlank := trimmed == ""
		if isBlank && !inFence {
			// Blank line outside a fence. If we have content in cur, this
			// blank finalizes it as a stable block; otherwise it's a
			// leading/inter-block blank (collapsed — opencode strips leading
			// blanks before commit too).
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = nil
			}
			continue
		}
		cur = append(cur, ln)
	}
	if len(cur) > 0 {
		// Trailing non-blank content with no following blank line: this is
		// the streaming block. It is NOT appended to `blocks` (the stable
		// list) — it is returned separately. (If a code fence is still open,
		// the streaming block carries the open fence — glamour will render it
		// as an in-progress code block, which is the desired streaming
		// behavior.)
		return blocks, strings.Join(cur, "\n")
	}
	// Text ends on a blank line: no streaming block.
	return blocks, ""
}

// ensureMDCache initialises the markdown render caches if they are nil.
// Called from New() so that all Model copies share non-nil maps from birth.
// Both the full-text cache (mdCache) and the per-stable-block streaming
// cache (mdBlockCache) are reference-typed maps, so a single ensure on the
// root Model is enough for all copies to share state. Also initialises the
// plan-20 derived-state maps (childStatusMap).
func (m *Model) ensureMDCache() {
	if m.mdCache == nil {
		m.mdCache = make(mdCache)
	}
	if m.mdBlockCache == nil {
		m.mdBlockCache = make(mdBlockCache)
	}
	if m.childStatusMap == nil {
		m.childStatusMap = make(map[string]string)
	}
}
