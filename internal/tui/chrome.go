package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// U7 — chrome: the bottom status bar and the right sidebar (design components.jsx
// StatusBar/Sidebar). Both read from the mirrored store; nothing is fabricated —
// fields the daemon hasn't reported simply render as zero.

const sidebarWidth = 28

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

// statusBarView renders the full-width bottom bar: mode · model on the left,
// connection + tokens/cost + the commands hint on the right.
func (m Model) statusBarView(width int) string {
	s := m.styles
	left := s.Base.Render(m.modeName()) + s.Faint.Render(" · ") + s.Base.Render(m.model.label())

	right := m.connGlyph() + s.Faint.Render(" "+m.status)
	if ss := m.currentSession(); ss != nil && ss.Tokens.Total() > 0 {
		right += s.Faint.Render(" · ") + s.Dim.Render(humanInt(ss.Tokens.Total())+" tok")
		if ss.Cost > 0 {
			right += s.Faint.Render(" · ") + s.Dim.Render(fmt.Sprintf("$%.4f", ss.Cost))
		}
	}
	right += s.Faint.Render(" · ") + s.Base.Render("ctrl+p") + s.Faint.Render(" commands")

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		// Too narrow for both segments — keep the right (status) only.
		return lipgloss.NewStyle().Width(width).Render(right)
	}
	bar := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().Background(s.P.BgElev).Width(width).Render(bar)
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

// sidebarView renders the right sidebar at full height: session title, the
// context (tokens/cost) block, the working directory, and the build tag.
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

	b.WriteString(s.Section.Render(truncate(title, sidebarWidth-2)) + "\n\n")

	b.WriteString(s.Dim.Render("CONTEXT") + "\n")
	if ss := m.currentSession(); ss != nil && ss.Tokens.Total() > 0 {
		b.WriteString(s.Base.Render(humanInt(ss.Tokens.Total())) + s.Faint.Render(" tokens") + "\n")
		if ss.Cost > 0 {
			b.WriteString(s.Base.Render(fmt.Sprintf("$%.4f", ss.Cost)) + s.Faint.Render(" spent") + "\n")
		}
	} else {
		b.WriteString(s.Faint.Render("—") + "\n")
	}

	body := b.String()

	// Footer pinned to the bottom: directory + build tag.
	var foot strings.Builder
	if dir != "" {
		foot.WriteString(s.Faint.Render(truncate(collapseHome(dir), sidebarWidth-2)) + "\n")
	}
	foot.WriteString(s.Faint.Render("• ") + s.Base.Render("Forge") + s.Faint.Render(" dev"))

	pad := m.height - lipgloss.Height(body) - lipgloss.Height(foot.String())
	if pad < 0 {
		pad = 0
	}
	panel := body + strings.Repeat("\n", pad) + foot.String()

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(s.P.Border).
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
