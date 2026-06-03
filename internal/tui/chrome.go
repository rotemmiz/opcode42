package tui

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// U7 — chrome: the bottom status bar and the right sidebar (design components.jsx
// StatusBar/Sidebar). Both read from the mirrored store; nothing is fabricated —
// fields the daemon hasn't reported simply render as zero.

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
// screen, when enabled, and when the terminal is wide/tall enough.
func (m Model) sidebarVisible() bool {
	return m.screen == ScreenSession && !m.sidebarHidden && m.width >= 80 && m.height >= 10
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
	// Token count + cost live solely in the sidebar CONTEXT section — the status
	// bar intentionally does not repeat them (opencode keeps context in the
	// sidebar and realtime status in the footer; no overlap).
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
//   - Footer: cwd path + Forge version tag
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
		b.WriteString(s.Faint.Render("—") + "\n")
	}

	b.WriteString("\n")

	// LSP section — server count with status dot (opencode footer.tsx lines 70-72).
	// Forge's TUI loads LSP info via the MCP server list; the count is the number
	// of connected MCP items (best-effort — Forge doesn't have a dedicated LSP endpoint
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

	body := b.String()

	// Footer pinned to the bottom: cwd:branch + Forge version tag.
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
		lipgloss.NewStyle().Foreground(s.P.Green).Render("•") +
			" " + s.Base.Render("Forge") + s.Faint.Render(" dev"),
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
		Width(sidebarWidth-1).
		Height(m.height).
		Padding(0, 1).
		Render(panel)
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
