package tui

import (
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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

// modeName is the status bar's "mode" label. opencode has two renderings:
//   - the run mini-TUI shows "BUILD"/"SHELL"/"EXIT" (uppercase) —
//     footer.view.tsx:384-390.
//   - the full TUI shows the agent name Title-cased (or "Shell" in shell mode)
//     — tui/component/prompt/index.tsx:1442-1444 (Locale.titlecase(agent().name)).
//
// Opcode42 follows the full-TUI convention: the active agent Title-cased when
// set (falling back to "Build"), "Shell" when the composer is in `!` shell mode,
// and "Exit" while the two-press ctrl+c guard is armed. The chip's foreground
// color (modeColor below) carries the mode semantic; the chip background is a
// neutral lift (BgSel).
func (m Model) modeName() string {
	if m.exiting {
		return "Exit"
	}
	if m.shellMode {
		return "Shell"
	}
	if m.agent != "" {
		return titleCase(m.agent)
	}
	return "Build"
}

// modeColor is the status-bar chip foreground for the current mode, mirroring
// opencode's modeColor memo (footer.view.tsx:391-401): error/red for exit,
// warning/amber for shell, highlight/blue for build.
func (m Model) modeColor(p theme.Palette) theme.Color {
	if m.exiting {
		return p.Red
	}
	if m.shellMode {
		return p.Amber
	}
	return p.Blue
}

// titleCase returns s with its first rune upper-cased and the rest left alone
// (matching opencode's Locale.titlecase for single-word agent names like
// "build" → "Build", "researcher" → "Researcher"). Agent names are simple
// lowercase identifiers from GET /agent, so no locale-aware special-casing
// is needed.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
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

// gitBranchCache memoizes gitBranch results per directory with a TTL (plan 19
// §4). Without this, sidebarView calls gitBranch on every frame — including
// pure scroll — spawning an exec.Command subprocess 20-200×/s. The cache is a
// sync.Map so it survives across Model value-copies (the Model is a value type
// in Bubble Tea's Update loop). Entries expire after gitBranchTTL so a branch
// switch is picked up without a TUI restart.
var gitBranchCache sync.Map // map[string]gitBranchEntry

type gitBranchEntry struct {
	branch  string
	fetched time.Time
}

const gitBranchTTL = 5 * time.Second

// gitBranch returns the current git branch name for the given directory, or ""
// when the directory is not a git repo or git is unavailable. Best-effort — any
// error (no git, no repo, detached HEAD) produces an empty string so callers
// can skip the branch display rather than show a confusing error.
//
// Results are cached per directory for gitBranchTTL (plan 19 §4) so the
// sidebar's per-frame call doesn't spawn a subprocess on every render.
func gitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	if v, ok := gitBranchCache.Load(dir); ok {
		e := v.(gitBranchEntry)
		if time.Since(e.fetched) < gitBranchTTL {
			return e.branch
		}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		gitBranchCache.Store(dir, gitBranchEntry{branch: "", fetched: time.Now()})
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		// Detached HEAD — not meaningful to display.
		branch = ""
	}
	gitBranchCache.Store(dir, gitBranchEntry{branch: branch, fetched: time.Now()})
	return branch
}

// statusBarView renders the full-width bottom bar using the opencode grammar
// (footer.view.tsx:822-910):
//
//   - Left:  [mode-chip] · model [variant]  — the chip has a neutral lift bg
//     (statusAccent ≈ BgSel) with a mode-colored fg (blue/amber/red) and bold
//     label (opencode footer.view.tsx:824-827, 384-401). The model name follows
//     in base; the variant, when set, is a bold amber suffix
//     (footer.view.tsx:872-878). The provider is dropped (footer.view.tsx:430-
//     435: "Prefer without provider").
//   - Right: status/spinner · tokens/cost · context hints · ctrl+p commands.
//
// The status bar paints its own surface (BgPanel, a tinted lift of the footer
// bg — opencode theme.ts:514-519 `status`) so it reads as a distinct chrome
// row from the composer (which uses BgElev). Each row is rendered through a
// single Surface(BgPanel) style padded to `width` so every cell carries the
// background — no transparent trailing cells (plan 08c M8 / Tier 0 fill rule).
func (m Model) statusBarView(width int) string {
	s := m.styles

	// Mode chip: neutral BgSel background, mode-colored foreground, bold label.
	// opencode's statusAccent is a near-white tint of the footer bg; BgSel is
	// Opcode42's closest neutral lift (row-hover surface). The mode color is
	// the FOREGROUND (opencode footer.view.tsx:826) — blue for build, amber
	// for shell, red for the armed exit guard.
	chip := lipgloss.NewStyle().
		Background(s.P.BgSel).
		Foreground(m.modeColor(s.P)).
		Bold(true).
		Padding(0, 1).
		Render(m.modeName())
	left := chip

	// Model + variant (provider dropped). opencode footer.view.tsx:864-882:
	// the model name in base text, the variant as a bold warning-colored
	// suffix; the provider span is intentionally omitted.
	if m.model.ok() {
		left += s.Faint.Render(" · ") + s.Base.Render(m.model.Model)
		if v := m.model.effectiveVariant(); v != "" {
			left += lipgloss.NewStyle().Foreground(s.P.Amber).Bold(true).Render(" " + v)
		}
	}

	// Status text: opencode footer.view.tsx:402-416. While the exit guard is
	// armed, prompt the second press; in shell mode at idle, label it "Shell
	// mode"; otherwise the connection glyph + the store status string. While
	// a turn is running, a gradient-scanner "thinking…" label replaces the
	// static status (mirrors opencode's <Spinner>Working...</Spinner>).
	var right string
	if m.exiting {
		right = s.Faint.Render("press ctrl+c again to exit")
	} else if m.animating() {
		frame := spinnerFrames[m.animFrame%len(spinnerFrames)]
		spinGlyph := lipgloss.NewStyle().Foreground(s.P.Accent()).Render(frame)
		thinkingLabel := scannerFrame("thinking…", m.animFrame, s.P)
		right = spinGlyph + " " + thinkingLabel
	} else if m.shellMode {
		right = s.Faint.Render("Shell mode")
	} else {
		right = m.connGlyph() + s.Faint.Render(" "+m.status)
	}
	// Token/cost counts are independent of the status text (opencode
	// footer.view.tsx:856-862 activityMeta is its own box, shown even while
	// the exit guard is armed), so they append regardless of mode.
	// Plan 08f H2 / G.4: prefer the opencode usage chip from the last
	// assistant message (tokens + context % + session cost).
	if chip := m.usageChip(); chip != "" {
		right += s.Faint.Render(" · ") + s.Dim.Render(chip)
	}
	// F6: context hints on the right. opencode footer.view.tsx:884-896 shows
	// background/queued/subagents chips (key + label) gated by a responsive
	// width policy. Opcode42 surfaces a subagent count when the open session
	// has children (its only source of truth for this state).
	if kids := m.childrenOf(m.cfg.SessionID); len(kids) > 0 {
		right += s.Faint.Render(" · ") + s.Base.Render(strconv.Itoa(len(kids))) + s.Faint.Render(" subagents")
	}
	right += s.Faint.Render(" · ") + s.Base.Render("ctrl+p") + s.Faint.Render(" commands")

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// Too narrow for both — keep the right (status) only, still surface-filled.
		return s.Surface(s.P.BgPanel).Width(width).Render(right)
	}
	bar := left + strings.Repeat(" ", gap) + right
	return s.Surface(s.P.BgPanel).Width(width).Render(bar)
}

// statusBarBackground is the surface background the status bar paints —
// opencode's "status" token (a tinted lift of the footer bg, theme.ts:514-519),
// here BgPanel. Exposed so a test can assert the status bar owns a distinct
// chrome row from the composer (BgElev) without relying on ANSI emission
// (plan 17 §F3).
func (m Model) statusBarBackground() theme.Color { return m.styles.P.BgPanel }

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

// usageChip formats the opencode prompt usage line (prompt/index.tsx:259-277):
// "<tokens> (<pct%>) · $<cost>" from the last assistant message with
// tokens.output > 0 plus the session cost. Empty when nothing to show.
func (m Model) usageChip() string {
	msgs := m.store.messages[m.cfg.SessionID]
	var last *Message
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Tokens.Output > 0 {
			last = &msgs[i]
			break
		}
	}
	if last == nil {
		return ""
	}
	tokens := int(last.Tokens.Total())
	if tokens <= 0 {
		return ""
	}
	context := humanInt(tokens)
	limit := m.contextLimitForActiveModel()
	if limit <= 0 {
		limit = defaultContextLimit
	}
	if limit > 0 {
		pct := int(math.Round(float64(tokens) / float64(limit) * 100))
		context = fmt.Sprintf("%s (%d%%)", humanInt(tokens), pct)
	}
	parts := []string{context}
	if ss := m.currentSession(); ss != nil && ss.Cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", ss.Cost))
	}
	return strings.Join(parts, " · ")
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
