package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// renderSession draws the conversation stream for the selected session: a title,
// the message blocks (user/assistant parts → user-turn/prose/thinking/tool-row),
// and the status line, scrolled to the newest content.
//
// This is the pre-resize fallback (the canvas path lives in sessionLayers).
// It composes the body + footer into a single joined string — the join is
// acceptable here because the fallback runs only when m.height <= 0, so the
// scroll math never engages. The production canvas path splits the stream and
// footer into separate layers (plan 17 §A1) so the footer stays pinned across
// scroll.
func (m Model) renderSession() string {
	s := m.styles

	// Optional right sidebar; the stream + composer take the remaining width.
	sidebar := ""
	leftW := m.leftColumnWidth()
	if m.sidebarVisible() {
		sidebar = m.sidebarView() // width == sidebarWidth (pinned by a test)
	}
	innerW := leftW - 2*streamGutter
	if innerW < 1 {
		innerW = 1
	}
	m.streamWidth = innerW // narrows the stream/composer wrap to the gutter-reduced left column

	footer := m.buildFooter(innerW)
	if ac := m.autocompleteView(); ac != "" {
		footer = ac + "\n" + footer // popup sits just above the composer
	}

	sid := m.cfg.SessionID
	header := s.Section.Render(truncate(m.sessionTitle(sid), innerW))
	blocks := m.sessionStreamBlocks(sid)
	body := header + "\n\n" + strings.Join(blocks, "\n\n")

	left := m.frame(body, footer)
	// Apply the 2-col gutter on each side of the stream column (plan 18 §B1).
	// Width(leftW) keeps the left column exactly leftW wide so the
	// JoinHorizontal with the sidebar aligns; the padding insets the stream
	// content by streamGutter cols on each side.
	left = lipgloss.NewStyle().Width(leftW).Padding(0, streamGutter).Render(left)
	if sidebar == "" {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, sidebar)
}

// buildFooter stacks the pinned bottom chrome: the tasks dock, sub-agent strip,
// embedded PTY pane, composer, and status bar — bottom-up, so the composer +
// status bar are the lowest rows. Shared by the canvas path (sessionLayers,
// plan 17 §A1) and the pre-resize fallback (renderSession) so the two paths
// don't duplicate the stacking order. The autocomplete popup is NOT part of
// this stack — on the canvas it composes as its own overlay layer at zPopup,
// and in the fallback it is prepended by the caller.
func (m Model) buildFooter(leftW int) string {
	footerParts := []string{m.composerView() + "\n" + m.statusBarView(leftW)}
	if dock := m.tasksDockView(leftW); dock != "" {
		footerParts = append([]string{dock}, footerParts...)
	}
	if sf := m.subagentFooterView(leftW); sf != "" {
		footerParts = append([]string{sf}, footerParts...)
	}
	if pty := m.ptyPaneView(leftW); pty != "" {
		footerParts = append([]string{pty}, footerParts...)
	}
	return strings.Join(footerParts, "\n")
}

// sessionStreamBlocks builds the chat-stream block list for a session: the
// per-message blocks (user/assistant parts) followed by the in-stream
// answered-question cards (plan 08e §E4, plan 17 §B6). Plan 17 §B6 drops the
// pending-question in-stream card — opencode has no pending card
// (run/tool.ts:827-829 scrollQuestionStart returns ""); the blocking footer
// panel is the only pending-question affordance. Finalized questions
// (answered or skipped) are appended as collapsed cards that stay in the
// history. Shared by renderSession (the pre-resize fallback / test path) and
// sessionLayers (the v2 canvas path).
func (m Model) sessionStreamBlocks(sid string) []string {
	var blocks []string
	for _, msg := range m.store.messages[sid] {
		if b := m.renderMessage(msg, m.store.parts[msg.ID]); b != "" {
			blocks = append(blocks, b)
		}
	}
	// Answered/skipped question cards (plan 08e §E4): collapsed cards that
	// stay in the history so the question is visible in the conversation
	// record, not just a transient modal.
	for _, aq := range m.store.answeredQuestions[sid] {
		if c := m.answeredQuestionCardView(aq); c != "" {
			blocks = append(blocks, c)
		}
	}
	return blocks
}

func (m Model) sessionTitle(sid string) string {
	for _, ss := range m.store.sessions {
		if ss.ID == sid && ss.Title != "" {
			return ss.Title
		}
	}
	return "session " + sid
}

// cachedBodyLines returns the conversation stream body pre-split into
// individual lines, cached by content version (plan 19 §2). On a cache hit
// (pure scroll — store/view/theme/width unchanged), the expensive
// sessionStreamBlocks iteration + JSON decodes + lipgloss renders + the
// join/split are all skipped; the cached []string is returned directly for
// viewport windowing. On a miss (content changed), the body is rebuilt,
// split into lines, cached, and returned.
func (m Model) cachedBodyLines(sid string, innerW int) []string {
	key := bodyLinesKey{
		storeVersion: m.store.version,
		sessionID:    sid,
		viewVersion:  m.viewVersion,
		themeName:    m.themeName,
		streamWidth:  innerW,
	}
	if m.animating() {
		key.animFrame = m.animFrame
	}
	if lines, ok := m.bodyLinesCache[key]; ok {
		return lines
	}
	header := m.styles.Section.Render(truncate(m.sessionTitle(sid), innerW))
	blocks := m.sessionStreamBlocks(sid)
	body := header + "\n\n" + strings.Join(blocks, "\n\n")
	lines := strings.Split(body, "\n")
	if m.bodyLinesCache == nil {
		m.bodyLinesCache = make(bodyLinesCacheMap)
	}
	m.bodyLinesCache[key] = lines
	return lines
}

// renderMessage renders one message's parts into stacked blocks.
func (m Model) renderMessage(msg Message, parts []Part) string {
	var out []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			txt := strings.TrimRight(p.Text, "\n")
			if txt == "" {
				continue
			}
			if msg.Role == "user" {
				out = append(out, m.userTurn(txt))
			} else {
				// Use the incremental streaming cache (plan 17 §D3) so a
				// growing assistant text part only re-renders its trailing
				// streaming block, not the whole part.
				out = append(out, m.prosePart(p))
			}
		case "reasoning":
			// Plan 17 §D1: reasoning is ALWAYS rendered (opencode full-TUI
			// showThinking = createMemo(() => true), index.tsx:254). In
			// hide mode (default) only the 1-line header shows; in show
			// mode (or with expandedThinking flipped in hide mode) the
			// body renders too. Empty text is still skipped — there's
			// nothing to display.
			if strings.TrimSpace(p.Text) == "" {
				continue
			}
			out = append(out, m.renderReasoning(p))
		case "tool":
			if m.view.hideTools {
				continue
			}
			out = append(out, m.toolRow(p))
		case "file":
			// Plan 08e §E2: image file parts render inline (Sixel/iTerm2)
			// when viewState.images is on and a terminal capability is
			// advertised; otherwise a placeholder glyph. Non-image file parts
			// render as a chip (filename + mime) so they're still visible in
			// the conversation record. renderImagePart handles both paths.
			if strings.HasPrefix(p.Mime, "image/") {
				out = append(out, m.renderImagePart(p))
			} else {
				out = append(out, m.fileChip(p))
			}
		}
	}
	// Surface an assistant turn's error (auth, overflow, rate limit, …) — never
	// swallow it; an errored turn often has no text parts at all.
	if msg.Error != nil {
		out = append(out, m.errorLine(msg.Error))
	}
	return strings.Join(out, "\n")
}

// errorLine renders an assistant error in red.
func (m Model) errorLine(e *MsgError) string {
	return lipgloss.NewStyle().Foreground(m.styles.P.Red).Width(m.contentWidth()).
		Render("⚠ " + e.Name + ": " + e.text())
}

// userTurn renders a user prompt with the design's blue left accent bar.
func (m Model) userTurn(text string) string {
	bar := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(m.styles.P.Blue).
		PaddingLeft(1).
		Width(m.barWidth()) // -1: the left border renders outside Width
	return bar.Render(m.styles.Base.Render(text))
}

// prose renders assistant text as styled markdown via glamour (plan 08c M4).
// The glamour render is theme-driven (colors from m.styles.P.Markdown) and
// cached so repeated frame renders are free. The full-text cache (renderMarkdown)
// is used here for one-off / non-streaming renders; streaming parts should use
// prosePart to get the incremental per-block cache (plan 17 §D3). Background
// fill is handled inside the markdown renderer (markdown.go).
func (m Model) prose(text string) string {
	return m.renderMarkdown(text)
}

// prosePart renders an assistant text part using the incremental streaming
// cache when the part has an ID (plan 17 §D3). The trailing streaming block
// re-renders each frame; stable blocks serve from the per-block cache.
func (m Model) prosePart(p Part) string {
	return m.renderMarkdownStreaming(p.ID, p.Text)
}

// renderReasoning renders one reasoning part following opencode's full-TUI
// ReasoningPart (tui/routes/session/index.tsx:1572-1632) + ReasoningHeader
// (index.tsx:1635-1677). The part's Time.End signals "done" (the streaming
// spinner vs static header flip); reasoningSummary extracts a `**Title**`
// prefix from the body to drive the header label.
//
// Two display modes mirror opencode's ThinkingMode (tui/context/thinking.ts):
//
//	hideThinking == false  → "show" mode: header + body always render.
//	hideThinking == true   → "hide" mode: header only; the body renders only
//	                        when expandedThinking is also true (opencode's
//	                        per-part `expanded` signal, index.tsx:1577).
//
// While streaming (Time.End == 0) the header carries a spinner with the
// "Thinking" / "Thinking: <title>" label (index.tsx:1650-1654); when done
// (Time.End != 0) the header is a static "+ Thought: <title> · <duration>"
// (index.tsx:1655-1674). The duration uses opencode's Locale.duration format
// (util/locale.ts:39-59): "<n>ms" / "<s.s>s" / "<m>m <s>s" / "<h>h <m>m" / …
//
// [REDACTED] (OpenRouter encrypted-reasoning placeholder) is stripped from the
// body before rendering (index.tsx:1582, run/entry.body.ts:62).
//
// Toggle keybinds (plan 17 §D1): ctrl+x r flips hideThinking; ctrl+x f flips
// expandedThinking (only meaningful in hide mode).
func (m Model) renderReasoning(p Part) string {
	s := m.styles
	cw := m.contentWidth()

	// [REDACTED] stripping (plan 17 §D5; opencode index.tsx:1582,
	// run/entry.body.ts:62). OpenRouter encrypts some reasoning blocks; the
	// placeholder would render as literal noise so drop it.
	content := strings.ReplaceAll(p.Text, "[REDACTED]", "")
	content = strings.TrimSpace(content)

	// reasoningSummary (opencode thinking.ts:12-17): a leading `**Title**`
	// block (followed by a blank line or end of text) is disclosure metadata
	// and is rendered as the header label, independent from the body.
	title, body := reasoningSummary(content)

	// Header style: open in show mode OR (hide mode + expanded); closed in
	// hide mode + collapsed. opencode's ReasoningHeader renders "+ Thought"
	// when closed and "- Thought" when open (index.tsx:1657-1659); Opcode42
	// keeps the same "+ Thought" prefix in both states for visual stability
	// (the open/closed affordance is conveyed by the body being present).
	open := !m.view.hideThinking || m.view.expandedThinking

	// Header label: spinner while streaming, static text when done.
	var header string
	if !p.Time.Done() {
		// Streaming: gradient-scanner "Thinking" or "Thinking: <title>"
		// (index.tsx:1650-1654). The scanner runs on the animTick infra
		// (spinner.go); the glyph is a braille frame driven by m.animFrame.
		label := "Thinking"
		if title != "" {
			label = "Thinking: " + title
		}
		frame := spinnerFrames[m.animFrame%len(spinnerFrames)]
		spin := lipgloss.NewStyle().Foreground(s.P.Amber).Render(frame)
		// scannerFrame sweeps the label with the Accent ramp. The label is
		// truncated to the content width minus the spinner glyph + space.
		budget := cw - lipgloss.Width(spin) - 1
		if budget < 1 {
			budget = 1
		}
		scanLabel := scannerFrame(truncate(label, budget), m.animFrame, s.P)
		header = spin + " " + scanLabel
	} else {
		// Done: static "+ Thought: <title> · <duration>" (index.tsx:1655-1674).
		// Duration uses Locale.duration (util/locale.ts:39-59).
		hdr := "+ Thought"
		if title != "" {
			hdr += ": " + title
		}
		if d := formatDuration(p.Time.Duration()); d != "" {
			hdr += " · " + d
		}
		budget := cw
		if budget < 1 {
			budget = 1
		}
		header = lipgloss.NewStyle().Foreground(s.P.Amber).Render(truncate(hdr, budget))
	}

	// Body: only when open AND there's a body to render. In show mode the
	// body always shows; in hide mode it shows only when expanded. opencode
	// renders the body as muted markdown (theme.textMuted, index.tsx:1626);
	// we use the Faint style (FgFaint tier) over the rendered markdown to
	// match run/scrollback.shared.ts:53-58.
	if !open || body == "" {
		return header
	}
	rendered := m.renderReasoningBody(p, body, cw)
	return header + "\n" + rendered
}

// renderReasoningBody renders the markdown body of a reasoning part in the
// muted reasoning style (FgFaint color, matching opencode's theme.textMuted on
// index.tsx:1626 + run/scrollback.shared.ts:53-58). The incremental markdown
// cache (markdown.go) keys on (partID, stableBlockCount, width, theme) so
// streaming deltas only re-render the trailing partial block (plan 17 §D3).
func (m Model) renderReasoningBody(p Part, body string, cw int) string {
	rendered := m.renderMarkdownStreaming(p.ID, body)
	// Apply the muted reasoning style: the Faint tier color over the rendered
	// markdown. glamour's own ANSI spans are left intact (they carry the
	// theme's markdown colors); we wrap the whole block in a Faint-styled
	// lipgloss render so lipgloss re-applies FgFaint across the spans.
	//
	// Note: lipgloss does not push a foreground through ANSI reset spans, so
	// this primarily affects plain-text runs (no explicit colour) — which is
	// the bulk of reasoning prose. Styled runs (headings, code, links) keep
	// their markdown palette colours, matching opencode where the body uses
	// theme.textMuted as the base and syntax styles still colour code spans.
	return m.styles.Faint.Width(cw).Render(rendered)
}

// reasoningSummary mirrors opencode's thinking.ts:12-17 — extracts a leading
// `**Title**` block (followed by a blank line or end of text) as the header
// label and returns the remaining body. Returns ("", content) when no titled
// summary is present.
//
// OpenAI's Responses API surfaces reasoning summaries that start with a
// bolded title block: "**Inspecting PR workflow**\n\n<body>". The title is
// disclosure metadata the TUI styles independently from the markdown body.
func reasoningSummary(text string) (title, body string) {
	const marker = "**"
	if !strings.HasPrefix(text, marker) {
		return "", text
	}
	rest := text[len(marker):]
	end := strings.Index(rest, marker)
	if end < 0 {
		return "", text
	}
	title = strings.TrimSpace(rest[:end])
	after := rest[end+len(marker):]
	// The marker must be followed by a blank line or end-of-text; otherwise
	// it's not a real title block (e.g. "**bold** mid-sentence").
	if after != "" && !strings.HasPrefix(after, "\n\n") && after != "\n" {
		// not a disclosure title — treat the whole text as body.
		return "", text
	}
	body = strings.TrimSpace(after)
	return title, body
}

// formatDuration mirrors opencode's Locale.duration (util/locale.ts:39-59):
//
//	<1000ms          → "<n>ms"
//	<60s             → "<s.s>s"
//	<60m             → "<m>m <s>s"
//	<24h             → "<h>h <m>m"
//	otherwise        → "<d>d <h>h"
//
// Returns "" for non-positive input (the caller gates on Time.Done() so this
// is defensive).
func formatDuration(ms int64) string {
	if ms <= 0 {
		return ""
	}
	switch {
	case ms < 1000:
		return strconv.FormatInt(ms, 10) + "ms"
	case ms < 60_000:
		return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + "s"
	case ms < 3_600_000:
		mins := ms / 60_000
		secs := (ms % 60_000) / 1000
		return strconv.FormatInt(mins, 10) + "m " + strconv.FormatInt(secs, 10) + "s"
	case ms < 86_400_000:
		hrs := ms / 3_600_000
		mins := (ms % 3_600_000) / 60_000
		return strconv.FormatInt(hrs, 10) + "h " + strconv.FormatInt(mins, 10) + "m"
	default:
		d := ms / 86_400_000
		hrs := (ms % 86_400_000) / 3_600_000
		return strconv.FormatInt(d, 10) + "d " + strconv.FormatInt(hrs, 10) + "h"
	}
}

// toolRow is defined in toolrender.go (plan 08c M7): per-tool headers,
// collapsible output panels, and todo-list rendering.

func (m Model) contentWidth() int {
	w := m.streamWidth // set when a sidebar narrows the stream column
	if w == 0 {
		w = m.width
	}
	return w
}

// barWidth is the content width an accent-bar block (left ThickBorder) should
// use so the bar+content fit exactly in contentWidth — lipgloss renders the
// border outside the style's Width, so reserve its one column here.
func (m Model) barWidth() int {
	if w := m.contentWidth() - 1; w > 0 {
		return w
	}
	return 1
}

// composerView renders the prompt input with the design's blue left accent bar.
func (m Model) composerView() string {
	accent := m.styles.P.Blue
	if m.shellMode {
		accent = m.styles.P.Red // shell mode: distinct accent so it's unmistakable
	}
	bar := lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(accent).
		BorderBackground(m.styles.P.BgElev). // paint the border cell too (no terminal bleed)
		Background(m.styles.P.BgElev).       // composer surface (opencode "surface"; BgElev is solid)
		PaddingLeft(1).
		Width(m.barWidth()) // -1: the left border renders outside Width
	view := bar.Render(m.input.View())
	if m.shellMode {
		label := lipgloss.NewStyle().Foreground(m.styles.P.Red).Render("! shell — enter run · esc cancel")
		return lipgloss.JoinVertical(lipgloss.Left, label, view)
	}
	return view
}

// composerBackground is the surface background the composer paints — opencode's
// "surface" token (a semi-opaque fade), here the solid BgElev. Exposed so a
// test can assert the composer owns an elevated surface distinct from the
// status bar (plan 17 §F1/F3) without relying on ANSI color emission (lipgloss
// emits no escapes in the no-TTY test environment).
func (m Model) composerBackground() theme.Color { return m.styles.P.BgElev }

// statusLine is the bottom status: connection state plus the active model.
func (m Model) statusLine() string {
	return m.status + " · " + m.model.label()
}

// frame tail-scrolls body to the lines that fit above footer and pins footer to
// the bottom (padding a short body so the composer/status bar stay anchored).
//
// This is the pre-resize fallback join: it returns the body + footer as a
// single joined string. The canvas path (sessionLayers, plan 17 §A1) does NOT
// use this join — it composes the stream and footer as separate layers so the
// footer stays pinned across scroll (the join is the original root cause of
// bug #1: the scroll math treated the footer as a suffix of the body). frame is
// retained for the fallback path and for direct callers that want the joined
// form (e.g. tests).
//
// The clamp/window math lives in the scrollregion package (plan 08e §A3): the
// Region is tail-anchored (0 == live tail), and Window both clamps the offset
// against [0, MaxOffset] and pads a short body so the footer stays pinned to
// the bottom row.
func (m Model) frame(body, footer string) string {
	if m.height <= 0 {
		return body + "\n" + footer
	}
	avail := m.height - lipgloss.Height(footer)
	if avail < 1 {
		avail = 1
	}
	lines := m.scroll.Window(strings.Split(body, "\n"), avail)
	return strings.Join(lines, "\n") + "\n" + footer
}

// frameStreamLines windows the pre-split body lines to the stream height
// (plan 19 §2). It takes the cached []string from cachedBodyLines and windows
// it via scrollregion.Window, which both clamps the offset and pads a short
// body so the stream region is always exactly bodyH rows.
func (m Model) frameStreamLines(lines []string, bodyH int) string {
	if bodyH <= 0 {
		return strings.Join(lines, "\n")
	}
	windowed := m.scroll.Window(lines, bodyH)
	return strings.Join(windowed, "\n")
}

// frameFooter returns the footer unchanged. The split exists to name the
// layer roles in sessionLayers (plan 17 §A1) — the footer is the pinned layer
// at Y=bodyH and never goes through the scroll window.
func (m Model) frameFooter(footer string) string { return footer }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// centerScreen places body in the middle of a width×height screen, returning it
// unplaced when either dimension is still zero (pre-first-resize). Shared by the
// full-screen overlays (modals, diff reviewer, prompts).
func centerScreen(width, height int, body string) string {
	if width == 0 || height == 0 {
		return body
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}

// centeredCardPos returns the (x, y) layer position that centers a card of the
// given content within a width×height canvas, mirroring lipgloss.Place's Center
// math without producing a full-screen padded string. Used by overlayLayers
// (plan 17 §A4) to place footer-panel cards (permission/question) as card-only
// layers — so the blank padding of a full-screen Place doesn't paint over the
// body layer at z=1. Returns ok=false when either dimension is zero or the card
// is larger than the canvas (caller falls back to 0,0).
func centeredCardPos(width, height int, card string) (x, y int, ok bool) {
	if width == 0 || height == 0 {
		return 0, 0, false
	}
	cw, ch := lipgloss.Width(card), lipgloss.Height(card)
	if cw > width || ch > height {
		return 0, 0, false
	}
	x = (width - cw) / 2
	y = (height - ch) / 2
	return x, y, true
}

// windowAround returns the [start,end) slice of count rows that fits height
// lines with sel kept roughly centered; the whole range when it already fits.
func windowAround(sel, count, height int) (int, int) {
	if count <= height {
		return 0, count
	}
	start := sel - height/2
	if start < 0 {
		start = 0
	}
	if hi := count - height; start > hi {
		start = hi
	}
	return start, start + height
}

// windowFrom returns the [start,end) slice of count rows starting at offset off
// (clamped so the last line can reach the bottom), fitting height lines — the
// top-anchored counterpart to windowAround, for scroll offsets.
func windowFrom(off, count, height int) (int, int) {
	maxOff := count - height
	if maxOff < 0 {
		maxOff = 0
	}
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	end := off + height
	if end > count {
		end = count
	}
	return off, end
}
