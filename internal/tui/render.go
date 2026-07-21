package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// contentWidth caps prose width for readability (the design's stream column).
const maxContentWidth = 100

// renderSession draws the conversation stream for the selected session: a title,
// the message blocks (user/assistant parts → user-turn/prose/thinking/tool-row),
// and the status line, scrolled to the newest content.
func (m Model) renderSession() string {
	s := m.styles

	// Optional right sidebar; the stream + composer take the remaining width.
	sidebar := ""
	leftW := m.leftColumnWidth()
	if m.sidebarVisible() {
		sidebar = m.sidebarView() // width == sidebarWidth (pinned by a test)
	}
	m.streamWidth = leftW // narrows the stream/composer wrap to the left column

	footer := m.composerView() + "\n" + m.statusBarView(leftW)
	if ac := m.autocompleteView(); ac != "" {
		footer = ac + "\n" + footer // popup sits just above the composer
	}
	if dock := m.tasksDockView(leftW); dock != "" {
		footer = dock + "\n" + footer // tasks dock above the composer area
	}
	if sf := m.subagentFooterView(leftW); sf != "" {
		footer = sf + "\n" + footer // sub-agent context strip (plan 08b §9)
	}
	if pty := m.ptyPaneView(leftW); pty != "" {
		footer = pty + "\n" + footer // embedded terminal split (plan 08b §2)
	}

	sid := m.cfg.SessionID
	header := s.Section.Render(truncate(m.sessionTitle(sid), leftW))
	blocks := m.sessionStreamBlocks(sid)
	body := header + "\n\n" + strings.Join(blocks, "\n\n")

	left := m.frame(body, footer)
	if sidebar == "" {
		return left
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, sidebar)
}

// sessionStreamBlocks builds the chat-stream block list for a session: the
// per-message blocks (user/assistant parts) followed by the in-stream question
// cards (plan 08e §E4). The pending-question card is appended after the last
// assistant message when the active pending question belongs to this session
// (the blocking overlay covers it while up; it shows in the scrollback once
// the overlay closes). Finalized questions (answered or skipped) are appended
// as collapsed cards that stay in the history. Shared by renderSession (the
// pre-resize fallback / test path) and sessionLayers (the v2 canvas path).
func (m Model) sessionStreamBlocks(sid string) []string {
	var blocks []string
	for _, msg := range m.store.messages[sid] {
		if b := m.renderMessage(msg, m.store.parts[msg.ID]); b != "" {
			blocks = append(blocks, b)
		}
	}
	// Pending question card (plan 08e §E4): only when the active pending
	// question belongs to this session. The blocking overlay covers the body
	// while up, so this card renders behind the overlay and becomes visible in
	// the scrollback once the overlay closes.
	if q := m.pendingQuestion(); q != nil && q.SessionID == sid {
		if c := m.questionCardView(); c != "" {
			blocks = append(blocks, c)
		}
	}
	// Answered/skipped question cards (plan 08e §E4): collapsed cards that stay
	// in the history so the question is visible in the conversation record, not
	// just a transient modal.
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
	if w == 0 || w > maxContentWidth {
		w = maxContentWidth
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
		BorderBackground(m.styles.P.Bg). // paint the border cell too (no terminal bleed)
		Background(m.styles.P.Bg).       // fill the composer row so it owns its bg
		PaddingLeft(1).
		Width(m.barWidth()) // -1: the left border renders outside Width
	view := bar.Render(m.input.View())
	if m.shellMode {
		label := lipgloss.NewStyle().Foreground(m.styles.P.Red).Render("! shell — enter run · esc cancel")
		return lipgloss.JoinVertical(lipgloss.Left, label, view)
	}
	return view
}

// statusLine is the bottom status: connection state plus the active model.
func (m Model) statusLine() string {
	return m.status + " · " + m.model.label()
}

// frame tail-scrolls body to the lines that fit above footer and pins footer to
// the bottom (padding a short body so the composer/status bar stay anchored).
//
// The clamp/window math lives in the scrollregion package (plan 08e §A3): the
// Region is tail-anchored (0 == live tail), and Window both clamps the offset
// against [0, MaxOffset] and pads a short body so the footer stays pinned to
// the bottom row. The canvas renders the resulting windowed string as the body
// layer at zPane — the scroll viewport is the canvas region the body layer
// occupies, and scrollregion.Window is the math that decides which rows of the
// full stream land in that region.
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
