package tui

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
)

// U7 — chrome: the bottom status bar and the right sidebar (design components.jsx
// StatusBar/Sidebar). Both read from the mirrored store; nothing is fabricated —
// fields the daemon hasn't reported simply render as zero.

// sidebarWidth is the right sidebar's column count, matching opencode's full
// TUI (tui/routes/session/sidebar.tsx:31 `width={42}`). Plan 17 §A2 corrected
// the prior 28-col constant to the source-grounded 42.
const sidebarWidth = 42

// currentSession returns the open session (or nil).
func (m Model) currentSession() *Session {
	for i := range m.store.sessions {
		if m.store.sessions[i].ID == m.cfg.SessionID {
			return &m.store.sessions[i]
		}
	}
	return nil
}

// modeName is the status bar's "mode" — the active agent, or "build" by default.
func (m Model) modeName() string {
	if m.agent != "" {
		return m.agent
	}
	return "build"
}

// sidebarVisible reports whether the right sidebar is shown: only on the session
// screen, when enabled, and when the terminal is wide/tall enough. Plan 17 §A2:
// the width threshold is >= 121, matching opencode's `width > 120`
// (tui/routes/session/index.tsx:263) — the sidebar is a "wide" affordance, not
// a "narrow" one, so it appears only when there's room for a 42-col sidebar
// plus a usable stream column.
func (m Model) sidebarVisible() bool {
	return m.screen == ScreenSession && !m.sidebarHidden && m.width >= 121 && m.height >= 10
}

// leftColumnWidth is the width available to the stream + composer + status bar —
// the full width less the sidebar when it's shown. Computed without rendering so
// the Update path (resizeComposer) and the render path agree; a unit test pins
// lipgloss.Width(sidebarView()) == sidebarWidth so the constant stays honest.
func (m Model) leftColumnWidth() int {
	if m.sidebarVisible() {
		return m.width - sidebarWidth
	}
	if m.width == 0 {
		return 0
	}
	return m.width
}

// gitBranch returns the current git branch name for the given directory, or ""
// when the directory is not a git repo or git is unavailable. Best-effort — any
// error (no git, no repo, detached HEAD) produces an empty string so callers
// can skip the branch display rather than show a confusing error.
func gitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		// Detached HEAD — not meaningful to display.
		return ""
	}
	return branch
}

// statusBarView renders the full-width bottom bar using the opencode grammar:
//
//   - Mode-line (left): [mode-chip] · model · provider  — matches opencode's
//     "Build · Big Pickle OpenCode Zen" pattern (scene 03-home-empty).
//     ModeChip is the accent-bg style; model is dim/primary; provider is faint.
//   - Footer (right): connection glyph + status · tokens/cost · ctrl+p hint.
//
// Each row is rendered through a single Surface(Bg) style padded to `width` so
// every cell carries the theme background — no transparent trailing cells.
// opencode always fills every cell via its opentui compositor; we replicate that
// here via lipgloss Width(width).Background(Bg). (plan 08c M8 / Tier 0 fill rule)
func (m Model) statusBarView(width int) string {
	s := m.styles

	// Left: mode chip (accent bg) + dim "·" + model name + faint provider.
	// This matches opencode prompt/index.tsx lines 1571–1583:
	//   agent-name · model · provider
	modeChip := s.ModeChip.Render(" " + m.modeName() + " ")
	left := modeChip
	if m.model.ok() {
		// model name in base, provider in faint — mirrors opencode's styling.
		left += s.Faint.Render(" · ") + s.Base.Render(m.model.Model) +
			s.Faint.Render(" · ") + s.Faint.Render(m.model.Provider)
	}

	// Right: connection dot + status/spinner, token/cost counts, command hint.
	// When a turn is in progress, show a gradient-scanner "thinking…" label
	// with a braille spinner in place of the static status text.  This mirrors
	// opencode's prompt/index.tsx which renders a <Spinner>Working...</Spinner>
	// while the assistant is active.  The scanner uses the left side of the bar
	// text — short enough that the sweep is clearly visible.
	var right string
	if m.animating() {
		frame := spinnerFrames[m.animFrame%len(spinnerFrames)]
		spinGlyph := lipgloss.NewStyle().Foreground(s.P.Accent()).Render(frame)
		thinkingLabel := scannerFrame("thinking…", m.animFrame, s.P)
		right = spinGlyph + " " + thinkingLabel
	} else {
		right = m.connGlyph() + s.Faint.Render(" "+m.status)
	}
	if ss := m.currentSession(); ss != nil && ss.Tokens.Total() > 0 {
		right += s.Faint.Render(" · ") + s.Dim.Render(humanInt(ss.Tokens.Total())+" tok")
		if ss.Cost > 0 {
			right += s.Faint.Render(" · ") + s.Dim.Render(fmt.Sprintf("$%.4f", ss.Cost))
		}
	}
	right += s.Faint.Render(" · ") + s.Base.Render("ctrl+p") + s.Faint.Render(" commands")

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// Too narrow for both — keep the right (status) only, still surface-filled.
		return s.Surface(s.P.Bg).Width(width).Render(right)
	}
	bar := left + strings.Repeat(" ", gap) + right
	// Surface fill: every cell in the bar row carries the Bg color so the
	// status bar reads as an owned surface on any terminal (plan 08c M8).
	return s.Surface(s.P.Bg).Width(width).Render(bar)
}

// connGlyph is a colored dot for the connection state.
func (m Model) connGlyph() string {
	c := m.styles.P.Green
	switch m.conn {
	case Connecting, Reconnecting:
		c = m.styles.P.Amber
	case ConnError:
		c = m.styles.P.Red
	}
	return lipgloss.NewStyle().Foreground(c).Render("●")
}

// sidebarView renders the right sidebar at full height matching opencode's scene
// 06-markdown-reasoning layout:
//
//   - Session title (bold)
//   - CONTEXT section: token counts (input/output) + cost
//   - LSP section: server count with a green/muted status dot
//   - Footer: cwd path + Opcode42 version tag
//
// Each section header uses s.Dim (all-caps muted label) and each row is a plain
// Base+Faint pair — mirrors opencode sidebar.tsx label/value pattern.
//
// Surface fill: the whole panel is rendered through Surface(BgPanel) so every
// padding cell inside the bordered block carries the panel background color
// rather than the terminal default. (plan 08c M8 / Tier 0 fill rule)
func (m Model) sidebarView() string {
	s := m.styles
	var b strings.Builder

	title := "session"
	dir := m.cfg.Directory
	if ss := m.currentSession(); ss != nil {
		if ss.Title != "" {
			title = ss.Title
		}
		if ss.Directory != "" {
			dir = ss.Directory
		}
	}

	// Session title row (bold).
	b.WriteString(s.Section.Render(truncate(title, sidebarWidth-2)) + "\n\n")

	// CONTEXT section — token counts + cost (opencode sidebar scene 06).
	b.WriteString(s.Dim.Render("CONTEXT") + "\n")
	if ss := m.currentSession(); ss != nil && ss.Tokens.Total() > 0 {
		// Input tokens line.
		b.WriteString(s.Faint.Render("in  ") + s.Base.Render(humanInt(ss.Tokens.Input)) + "\n")
		// Output tokens line.
		b.WriteString(s.Faint.Render("out ") + s.Base.Render(humanInt(ss.Tokens.Output)) + "\n")
		// Total + cost on one line when cost is non-zero.
		total := s.Base.Render(humanInt(ss.Tokens.Total())) + s.Faint.Render(" total")
		if ss.Cost > 0 {
			total += s.Faint.Render("  ") + s.Dim.Render(fmt.Sprintf("$%.4f", ss.Cost))
		}
		b.WriteString(total + "\n")
	} else {
		// E5: draft (no messages) — show "0 / <limit>" instead of the bare
		// em-dash placeholder so the gauge reads as a context bar, not a
		// blank. The limit resolves synchronously from the cached providers
		// catalog (m.choices); when the cache isn't populated yet or the
		// active model isn't in it, fall back to a constant default.
		limit := m.contextLimitForActiveModel()
		if limit <= 0 {
			limit = defaultContextLimit
		}
		b.WriteString(s.Base.Render("0") + s.Faint.Render(" / "+humanInt(limit)) + "\n")
	}

	b.WriteString("\n")

	// LSP section — server count with status dot (opencode footer.tsx lines 70-72).
	// Opcode42's TUI loads LSP info via the MCP server list; the count is the number
	// of connected MCP items (best-effort — Opcode42 doesn't have a dedicated LSP endpoint
	// yet so we show MCP-connected count the same way opencode shows LSP count).
	b.WriteString(s.Dim.Render("LSP") + "\n")
	lspCount := 0
	for _, srv := range m.mcpServers {
		if srv.Status == "connected" || srv.Status == "" {
			lspCount++
		}
	}
	dotColor := s.P.FgFaint
	if lspCount > 0 {
		dotColor = s.P.Green
	}
	dot := lipgloss.NewStyle().Foreground(dotColor).Render("•")
	b.WriteString(dot + " " + s.Base.Render(strconv.Itoa(lspCount)) + s.Faint.Render(" servers") + "\n")

	// TASKS section (plan 08e §C3) — live sub-agent status, only while the
	// open session has children. Mirrors Android's D1 tasks flake and the
	// design's right-sidebar Tasks block: a status dot (spinner while
	// running, ✓ when completed) + the child's subagentLabel. When no
	// children exist the section is omitted entirely (the design: "only
	// while a sub-agent runs"). The child status is read from the tool
	// parts of the child session's mirrored stream (a running tool part ⇒
	// the child is still working; all completed ⇒ ✓).
	if kids := m.childrenOf(m.cfg.SessionID); len(kids) > 0 {
		b.WriteString("\n")
		b.WriteString(s.Dim.Render("TASKS") + "\n")
		for _, kid := range kids {
			status := m.childStatus(kid.ID)
			var glyph string
			var col theme.Color
			switch status {
			case "running":
				frame := spinnerFrames[m.animFrame%len(spinnerFrames)]
				glyph, col = frame, s.P.Accent()
			case "error":
				glyph, col = "✗", s.P.Red
			case "cancelled":
				glyph, col = "○", s.P.FgFaint
			case "completed":
				glyph, col = "✓", s.P.Green
			default:
				glyph, col = "•", s.P.FgFaint
			}
			dot := lipgloss.NewStyle().Foreground(col).Render(glyph)
			label := truncate(subagentLabel(kid), sidebarWidth-4)
			b.WriteString(dot + " " + s.Base.Render(label) + "\n")
		}
	}

	body := b.String()

	// Footer pinned to the bottom: cwd:branch + Opcode42 version tag.
	// Matches opencode footer.tsx (directory left, version right) and sidebar.tsx
	// (OpenCode + InstallationVersion on the bottom row).
	var foot strings.Builder
	if dir != "" {
		cwd := truncate(collapseHome(dir), sidebarWidth-2)
		if br := gitBranch(dir); br != "" {
			cwd += s.Faint.Render(":") + s.Dim.Render(br)
			cwd = truncate(cwd, sidebarWidth-2) // re-truncate in case branch adds length
		}
		foot.WriteString(s.Faint.Render(cwd) + "\n")
	}
	foot.WriteString(
		footerMicroLogo(s.P) + " " + s.Base.Render("Opcode42") + s.Faint.Render(" dev"),
	)

	pad := m.height - lipgloss.Height(body) - lipgloss.Height(foot.String())
	if pad < 0 {
		pad = 0
	}
	panel := body + strings.Repeat("\n", pad) + foot.String()

	// Pin the sidebar surface background so every padding/fill cell inside the
	// panel is owned by BgPanel rather than left transparent — Surface() is the
	// shared helper for this pattern (plan 08c M0).
	return s.Surface(s.P.BgPanel).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(s.P.Border).
		BorderBackground(s.P.BgPanel).
		Width(sidebarWidth). // lipgloss v2: Width includes the border column (was -1 in v1)
		Height(m.height).
		Padding(0, 1).
		Render(panel)
}

// defaultContextLimit is the fallback context-window size shown in the sidebar
// gauge when the providers cache is empty (before the first /provider response
// arrives) or the active model isn't found in it. opencode falls back to the
// resolved model's limit.context; we fall back to this constant so the gauge
// is never blank on a draft session (plan 08e §E5).
const defaultContextLimit = 128000

// contextLimitForActiveModel resolves the active model's context-window size
// from the cached providers catalog (m.choices). Returns 0 when the cache is
// empty or the active model isn't in it — callers fall back to
// defaultContextLimit in that case. Synchronous so the gauge renders on
// session switch without waiting for a re-fetch (plan 08e §E5).
func (m Model) contextLimitForActiveModel() int {
	if !m.model.ok() {
		return 0
	}
	for _, ch := range m.choices {
		if ch.Provider == m.model.Provider && ch.Model == m.model.Model {
			return ch.ContextLimit
		}
	}
	return 0
}

// humanInt formats n with thousands separators (1234 → "1,234").
func humanInt(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var out []byte
	for i, d := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, d)
	}
	return string(out)
}

func truncate(s string, n int) string {
	if n <= 0 { // no room — drop it rather than overflow the column
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	r := []rune(s)
	if len(r) > n-1 {
		r = r[:n-1]
	}
	return string(r) + "…"
}

// collapseHome shortens a $HOME-prefixed path to ~/… for the sidebar footer.
func collapseHome(p string) string {
	if i := strings.Index(p, "/git/"); i >= 0 {
		return "~" + p[i:]
	}
	return p
}

// footerMicroLogo returns a compact 1-row block-pixel mark for the sidebar
// footer version chip (plan 08e §B4), replacing the plain "•" bullet with a
// small inline mark that echoes the splash wordmark's block-pixel idiom.
//
// The mark is derived from opcode42Glyph: the "o" letter's filled columns
// (cols 4-6) of the mid row, which render as a solid 3-block "███" — compact
// enough for the 1-line footer (a 3-row micro-logo would break the layout)
// and "subtle, not gimmicky" per the plan's guidance. Colored in the theme
// Green (the same color the bullet used) so the version chip reads as before.
func footerMicroLogo(p theme.Palette) string {
	row := []rune(opcode42Glyph[2]) // mid row of the wordmark
	var b strings.Builder
	for _, x := range []int{4, 5, 6} { // the "o" letter's columns
		if x < len(row) && row[x] == '█' {
			b.WriteRune('█')
		} else {
			b.WriteRune(' ')
		}
	}
	return lipgloss.NewStyle().Foreground(p.Green).Render(b.String())
}
