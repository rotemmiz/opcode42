// Package tui is the Opcode42 terminal client: a Bubble Tea app over the
// opencode/Opcode42 wire protocol (via the Go SDK, plan 06). It is wire-generic —
// point it at a Opcode42 or a real opencode daemon. Design: design/tui/ (plan 08).
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/opcode42/internal/tui/theme"
	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Screen is the active top-level view.
type Screen int

const (
	// ScreenSplash is the first-run entry / connecting state.
	ScreenSplash Screen = iota
	// ScreenSession is the conversation stream.
	ScreenSession
)

// ConnState is the daemon connection state.
type ConnState int

const (
	// Connecting is the initial state before the first successful health check.
	Connecting ConnState = iota
	// Connected means the daemon is reachable and the event stream is live.
	Connected
	// Reconnecting means the stream dropped and is backing off.
	Reconnecting
	// ConnError is a terminal connection/auth failure.
	ConnError
)

// Config configures a Model.
type Config struct {
	URL       string
	Directory string
	SessionID string
	Username  string
	Password  string
	Provider  string // prompt model provider id (else resolved from /config)
	Model     string // prompt model id
	Theme     string // override theme name (empty = auto-pick or KV-pinned; for deterministic capture)
}

// Model is the Bubble Tea application state.
type Model struct {
	cfg    Config
	styles theme.Styles

	width  int
	height int

	screen Screen
	conn   ConnState
	status string // human-readable connection status
	err    error

	// Connection.
	client     *opcode42client.Opcode42Client
	ctx        context.Context
	cancel     context.CancelFunc
	stream     *opcode42client.EventStream
	attempt    int // reconnect backoff attempt
	eventCount int // events seen this connection (status line)

	// store mirrors the daemon state from the SSE stream.
	store store

	// Composer.
	input     textarea.Model
	model     promptModel
	shellMode bool // composer is in `!` shell mode (submit → POST /session/{id}/shell)

	// Command overlay.
	modal        modalKind
	modalSel     int
	renameInput  textinput.Model // text-input overlay (rename current session)
	mcpServers   []mcpItem       // read-only MCP list (GET /mcp)
	skills       []skillItem     // read-only skills list (GET /skill)
	permSel      int             // selected choice in the permission overlay
	permReplying bool            // a permission reply is in flight (overlay stays up until it resolves)

	// Question overlay (steps through a request's questions).
	qIdx      int        // current question index
	qSel      int        // option cursor
	qChecked  []bool     // multi-select toggles for the current question
	qAnswers  [][]string // accumulated answers (one []label per answered question)
	qReplying bool       // a question reply/reject is in flight

	// choices is the connected provider/model catalog (model switcher).
	choices []modelChoice

	// Slash commands.
	commands []slashItem  // daemon commands (GET /command)
	ac       autocomplete // composer "/" popup state

	// Chrome.
	agent          string      // active agent (status bar "mode"); empty → default
	agents         []agentItem // selectable agents (GET /agent)
	themeName      string      // active theme name (theme switcher)
	sidebarHidden  bool        // right sidebar visibility (toggle: ctrl+x b)
	streamWidth    int         // transient: stream column width when the sidebar is shown
	leader         bool        // ctrl+x leader pressed, awaiting the chord key
	tasksOpen      bool        // tasks dock visibility (toggle: ctrl+x t)
	todos          []Todo      // current session's todos (tasks dock)
	scrollOffset   int         // stream scrollback: lines hidden below the viewport (0 = live tail)
	view           viewState   // display toggles (timestamps, tool output, thinking)
	history        []string    // submitted prompts (persisted; recalled with up/down when empty)
	histIdx        int         // browse cursor into history (-1 = not browsing)
	persistEnabled bool        // gate local-KV reads/writes (off in tests; on via Restore)

	// Diff reviewer (plan 08b §1).
	diff           diffState // full-screen diff reviewer (open == active)
	diffTreeHidden bool      // persisted: file-tree pane preference

	// PTY pane (plan 08b §2).
	pty    ptyState // embedded terminal split (open == visible)
	ptyGen int      // monotonic pane-open counter (stamps async PTY msgs)

	// Stashed prompt drafts (plan 08b §6; persisted).
	stash []string

	// termDark records whether the terminal reported a dark background at
	// startup (lipgloss.HasDarkBackground()).  Stored so that applyThemeByName
	// can resolve embedded opencode themes for the correct dark/light variant
	// when the user changes theme mid-session (mirrors opencode's theme_mode_lock
	// + dark/light token resolution — context/theme.tsx resolveTheme).
	termDark bool

	// mdCache is the rendered-markdown cache for renderMarkdown (markdown.go).
	// Key: (SHA-256 of text, content width, theme name).  Invalidated naturally
	// by the theme name component: a theme switch produces cache misses and new
	// entries for the new theme; old entries become unreachable and are GC'd.
	mdCache mdCache

	// animFrame is the monotonic animation frame counter incremented on each
	// animTickMsg.  Passed to scannerFrame() and (later) logo shimmer.
	// Reset to 0 when a new session opens so the sweep always starts from the left.
	// (plan 08c M9 — spinner.go)
	animFrame int

	// toasts is the live toast queue (plan 08c M11).  Entries expire after
	// toastTTL; the animTick drives TTL countdown via toastTick().
	// pushToast enqueues and toastTick purges; overlayToasts composites the
	// stack onto the rendered frame bottom-right.
	toasts []toast
}

// pickDefaultTheme returns the appropriate default theme name based on whether
// the terminal has a dark background. This mirrors opencode's theme_mode_lock
// (kv.json) + dark/light token resolution (context/theme.tsx): on a dark
// terminal opcode42-dark fits; on a light/white terminal opcode42-light is chosen so
// foreground colors remain legible without imposing a dark fill.
// The darkBg param lets tests inject a value instead of reading the real terminal.
func pickDefaultTheme(darkBg bool) string {
	if darkBg {
		return "opcode42-dark"
	}
	return "opcode42-light"
}

// New builds the initial Model, constructing the SDK client.
func New(cfg Config) Model {
	ctx, cancel := context.WithCancel(context.Background())
	ta := textarea.New()
	ta.Placeholder = "Ask anything…"
	ta.Prompt = ""                                           // we draw our own blue accent bar (composerView)
	ta.ShowLineNumbers = false                               //
	ta.CharLimit = 0                                         // no limit — prompts can be long
	ta.SetHeight(1)                                          // grows with content up to maxComposerRows
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "ctrl+j") // Enter submits; these add a newline
	// Drop the focused cursor-line highlight + base frame to keep the minimal look.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.Focus()
	ri := textinput.New()
	ri.Placeholder = "Session title"
	ri.CharLimit = 200
	m := Model{
		cfg:         cfg,
		screen:      ScreenSplash,
		conn:        Connecting,
		status:      "connecting to " + cfg.URL,
		ctx:         ctx,
		cancel:      cancel,
		store:       newStore(),
		input:       ta,
		renameInput: ri,
		model:       promptModel{Provider: cfg.Provider, Model: cfg.Model},
	}
	// Auto-pick light vs dark by terminal background — mirrors opencode's
	// theme_mode_lock behaviour. Restore() will override with any pinned KV theme.
	// cfg.Theme (--theme flag) wins over both auto-pick and KV.
	m.termDark = lipgloss.HasDarkBackground()
	defName := pickDefaultTheme(m.termDark)
	if cfg.Theme != "" {
		defName = cfg.Theme
	}
	def, _ := theme.ByNameForMode(defName, m.termDark)
	m = m.applyTheme(defName, def)
	m.histIdx = -1
	c, err := opcode42client.New(cfg.URL, opcode42client.Options{
		Directory: cfg.Directory, Username: cfg.Username, Password: cfg.Password,
	})
	if err != nil {
		m.conn, m.err = ConnError, err
		return m
	}
	m.client = c
	// Ensure the markdown render cache is allocated so all Model copies
	// derived from this root share a non-nil map (maps are reference types).
	m.ensureMDCache()
	return m
}

// applyTheme switches the active palette: the shared styles AND the textarea's
// own text/placeholder colors (lipgloss leaves those terminal-default, which is
// unreadable after a light/mono switch). View() paints the palette background
// behind everything so foreground-only renderers stay legible on any terminal.
func (m Model) applyTheme(name string, p theme.Palette) Model {
	m.themeName = name
	m.styles = theme.New(p)
	txt := lipgloss.NewStyle().Foreground(p.Fg).Background(p.Bg)
	ph := lipgloss.NewStyle().Foreground(p.FgGhost).Background(p.Bg)
	m.input.FocusedStyle.Text, m.input.FocusedStyle.Placeholder = txt, ph
	m.input.BlurredStyle.Text, m.input.BlurredStyle.Placeholder = txt, ph
	// The textarea pads its current/empty line with CursorLine + Base styles; pin
	// their Bg too so the composer row fills with the theme background rather than
	// the terminal default (visible as a dark bar on a light terminal). plan 08c Tier 0.
	bg := lipgloss.NewStyle().Background(p.Bg)
	m.input.FocusedStyle.CursorLine, m.input.FocusedStyle.Base = bg, bg
	m.input.BlurredStyle.CursorLine, m.input.BlurredStyle.Base = bg, bg
	m.input.FocusedStyle.EndOfBuffer, m.input.BlurredStyle.EndOfBuffer = bg, bg
	// bubbles' textarea caches an internal *Style pointer (set only by Focus/Blur)
	// to the active style; after this value-copy of Model that pointer still aims at
	// the pre-copy FocusedStyle, so our edits above wouldn't take effect on render.
	// Re-point it to the copy's style by re-applying the current focus state.
	if m.input.Focused() {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
	return m
}

// Restore loads the persisted theme/model/history from the local KV and turns on
// persistence. Call once from the real entrypoint (not in tests, which want a
// hermetic New). CLI --provider/--model/--theme still win.
// Theme resolution order: cfg.Theme (CLI --theme) > pinned KV theme > auto-pick by terminal background.
func (m Model) Restore() Model {
	m.persistEnabled = true
	kv := loadKV()
	m.history, m.histIdx = kv.History, -1
	m.stash = kv.Stash
	m.diffTreeHidden = kv.HideDiffTree
	if m.cfg.Theme != "" {
		// CLI --theme flag takes highest priority — deterministic capture / testing.
		m = m.applyThemeByName(m.cfg.Theme)
	} else if kv.Theme != "" {
		// User explicitly pinned a theme — honour it (mirrors opencode's theme_mode_lock).
		m = m.applyThemeByName(kv.Theme)
	}
	// If no pinned theme the auto-pick applied in New() already selected the right
	// default; nothing further needed here.
	if !m.model.ok() && kv.Provider != "" && kv.Model != "" {
		m.model = promptModel{Provider: kv.Provider, Model: kv.Model, Variant: kv.Variant}
	}
	return m
}

// applyThemeByName switches to a palette by name (no-op if unknown).
// Resolves the palette for the terminal's dark/light mode (m.termDark) so that
// embedded opencode themes use the correct dark or light token variant.
func (m Model) applyThemeByName(name string) Model {
	if p, ok := theme.ByNameForMode(name, m.termDark); ok {
		return m.applyTheme(name, p)
	}
	return m
}

// Init kicks off the daemon health check.
func (m Model) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	// Kick the animation tick immediately: the splash screen is the initial state,
	// and the logo shimmer needs to start running right away (plan 08c M10).
	// maybeKickAnim() returns animTickCmd() because animating() is true on
	// ScreenSplash.  The health check and tick run concurrently.
	return tea.Batch(healthCmd(m.ctx, m.client), m.maybeKickAnim())
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The composer placeholder follows the screen (design app.jsx:355).
	if m.screen == ScreenSession {
		m.input.Placeholder = "Reply, or / for commands"
	} else {
		m.input.Placeholder = "Ask anything…"
	}
	// Keep the composer sized to the current left column: a screen change
	// (splash→session) or sidebar toggle alters the available width even when no
	// key was pressed. WindowSizeMsg re-runs this after updating m.width.
	m = m.resizeComposer()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m = m.resizeComposer()
		if cmd := m.resizePTY(); cmd != nil {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// A focused terminal captures every key (ctrl+c included, so the shell can
		// interrupt) — only ctrl+] escapes, handled inside handlePTYKey. A pending
		// permission/question prompt still takes precedence (so it can be answered);
		// focus returns to the shell once the prompt resolves.
		if m.pty.open && m.pty.focused && m.pendingPermission() == nil && m.pendingQuestion() == nil {
			return m.handlePTYKey(msg)
		}
		// An open-but-unfocused terminal: ctrl+] closes it.
		if m.pty.open && msg.String() == "ctrl+]" {
			m.closePTY()
			return m, nil
		}
		// ctrl+c always quits.
		if msg.String() == "ctrl+c" {
			if m.stream != nil {
				m.stream.Close()
			}
			if m.cancel != nil {
				m.cancel() // cancel any in-flight health/open cmd + SDK work
			}
			return m, tea.Quit
		}
		// A pending permission blocks everything until answered.
		if m.pendingPermission() != nil {
			return m.handlePermissionKey(msg)
		}
		// A pending question likewise blocks until answered/rejected.
		if m.pendingQuestion() != nil {
			return m.handleQuestionKey(msg)
		}
		// The full-screen diff reviewer captures all navigation keys.
		if m.diff.open {
			return m.handleDiffKey(msg)
		}
		// A modal captures navigation/selection keys.
		if m.modal != modalNone {
			return m.handleModalKey(msg)
		}
		// ctrl+x leader: the next key is a chord (design app.jsx:227-237).
		if m.leader {
			m.leader = false
			return m.handleLeaderKey(msg)
		}
		if msg.String() == "ctrl+x" {
			m.leader = true
			m.status = "ctrl+x — l sessions · n new · m model · a agent · g timeline · s status · b sidebar · t tasks · y copy · r thinking · f fold thought · o tools · v fold tool · e editor · d diff · ` terminal · w stash · ↓ child · ↑ parent · [ ] siblings"
			return m, nil
		}
		// The slash popup captures nav/accept/dismiss keys; other keys fall
		// through so typing keeps filtering it.
		if m.ac.open {
			if handled, nm, cmd := m.handleAutocompleteKey(msg); handled {
				return nm, cmd
			}
		}
		switch msg.String() {
		case "ctrl+p":
			m.modal, m.modalSel = modalPalette, 0
			return m, nil
		case "ctrl+t":
			return m.cycleVariant(), nil // cycle model variants (opencode variant_cycle)
		case "ctrl+up", "pgup":
			m.scrollOffset += scrollStep
			return m, nil
		case "ctrl+down", "pgdown", "pgdn":
			m.scrollOffset -= scrollStep
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			return m, nil
		case "up":
			if nm, ok := m.historyRecall(-1); ok {
				return nm, nil
			}
		case "down":
			if nm, ok := m.historyRecall(+1); ok {
				return nm, nil
			}
		case "!":
			// `!` at the start of an empty composer enters shell mode (opencode
			// prompt-input.tsx:1160); a real "!" mid-text falls through to typing.
			if !m.shellMode && strings.TrimSpace(m.input.Value()) == "" {
				m.shellMode = true
				return m, nil
			}
		case "esc":
			if m.shellMode {
				m.shellMode = false
				return m, nil
			}
		case "enter":
			return m.submit()
		}
		// Everything else goes to the composer (shift+enter / ctrl+j add a newline).
		m.histIdx = -1 // editing exits history browse
		var cmd, acCmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m = m.resizeComposer()             // auto-grow to fit the new content
		m, acCmd = m.refreshAutocomplete() // open/refresh the "/" or "@" popup
		return m, tea.Batch(cmd, acCmd)

	case sessionOpenedMsg:
		if msg.err != nil {
			m.status = "create session failed: " + msg.err.Error()
			return m, nil
		}
		if msg.session.ID == "" { // daemon returned 200 + {} or similar
			m.status, m.modal = "create session: empty response", modalNone
			return m, nil
		}
		m.store.sessions = upsertSession(m.store.sessions, msg.session)
		m.cfg.SessionID, m.screen, m.modal = msg.session.ID, ScreenSession, modalNone
		// Reset animation frame so the sweep starts from the left in the new session.
		m.animFrame = 0
		return m, nil

	case sessionDeletedMsg:
		if msg.err != nil {
			m.status = "delete failed: " + msg.err.Error()
			return m, nil
		}
		for _, dm := range m.store.messages[msg.id] { // drop the session's parts too
			delete(m.store.parts, dm.ID)
		}
		m.store.sessions = removeSession(m.store.sessions, msg.id)
		delete(m.store.messages, msg.id)
		if m.modalSel > 0 && m.modalSel >= m.modalCount() {
			m.modalSel = m.modalCount() - 1
		}
		if m.cfg.SessionID == msg.id { // the open session was deleted
			if ss := m.orderedSessions(); len(ss) > 0 {
				m.cfg.SessionID = ss[0].ID
				return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)
			}
			m.cfg.SessionID, m.screen = "", ScreenSplash
			// Re-entering the splash — kick the logo shimmer tick (plan 08c M10).
			return m, m.maybeKickAnim()
		}
		return m, nil

	case connectedMsg:
		m.conn, m.status, m.attempt = Connected, "connected", 0
		// Subscribe to events, bootstrap the session list, resolve the model, and
		// preload the provider + command catalogs so the switcher/slash popup open
		// populated.
		return m, tea.Batch(openSSECmd(m.ctx, m.client), loadSessionsCmd(m.ctx, m.client), loadConfigCmd(m.ctx, m.client), loadProvidersCmd(m.ctx, m.client), loadCommandsCmd(m.ctx, m.client), loadAgentsCmd(m.ctx, m.client))

	case configLoadedMsg:
		if !m.model.ok() {
			m.model = promptModel{Provider: msg.provider, Model: msg.model}
		}
		return m, nil

	case providersLoadedMsg:
		if msg.err != nil {
			if m.modal == modalModels {
				m.status = "providers: " + msg.err.Error()
			}
			return m, nil
		}
		m.choices = msg.choices
		if m.modal == modalModels { // re-highlight the active model now the list is in
			m.modalSel = m.modelSelIndex()
		}
		return m, nil

	case commandsLoadedMsg:
		if msg.err == nil {
			m.commands = msg.items
		}
		return m, nil

	case agentsLoadedMsg:
		if msg.err != nil {
			if m.modal == modalAgents {
				m.status = "agents: " + msg.err.Error()
			}
			return m, nil
		}
		m.agents = msg.items
		if m.agent != "" { // drop a selection the (re)connected daemon no longer offers
			found := false
			for _, a := range m.agents {
				if a.name == m.agent {
					found = true
					break
				}
			}
			if !found {
				m.agent = ""
			}
		}
		if m.modal == modalAgents { // re-highlight the active agent now the list is in
			m.modalSel = m.agentSelIndex()
		}
		return m, nil

	case sessionCreatedMsg:
		if msg.err != nil {
			m.status = "create session failed: " + msg.err.Error()
			return m, nil
		}
		m.store.sessions = upsertSession(m.store.sessions, msg.session)
		m.cfg.SessionID = msg.session.ID
		m.screen = ScreenSession
		if msg.command != "" { // a "/command" created this session — run it
			return m, runCommandCmd(m.ctx, m.client, msg.session.ID, msg.command, msg.arguments)
		}
		return m, promptCmd(m.ctx, m.client, msg.session.ID, msg.text, m.model, m.agent)

	case promptSentMsg:
		if msg.err != nil {
			m.status = "prompt failed: " + msg.err.Error()
		}
		return m, nil

	case revertedMsg:
		if msg.err != nil {
			m.status = "revert failed: " + msg.err.Error()
		} else {
			m.status = "reverted"
		}
		return m, nil

	case renamedMsg:
		if msg.err != nil {
			m.status = "rename failed: " + msg.err.Error()
			return m, nil
		}
		if msg.session.ID != "" {
			m.store.sessions = upsertSession(m.store.sessions, msg.session)
		}
		m.status = "renamed"
		return m, nil

	case sharedMsg:
		if msg.err != nil {
			m.status = "share failed: " + msg.err.Error()
			return m, nil
		}
		if msg.session.ID != "" {
			m.store.sessions = upsertSession(m.store.sessions, msg.session)
		}
		if msg.shared {
			if sh := msg.session.Share; sh != nil && sh.URL != "" {
				m.status = "shared · " + sh.URL + " (copied)"
				return m, copyClipboardCmd(sh.URL)
			}
			m.status = "shared"
		} else {
			m.status = "unshared"
		}
		return m, nil

	case summarizedMsg:
		if msg.err != nil {
			m.status = "summarize failed: " + msg.err.Error()
		} else {
			m.status = "summarizing context…"
		}
		return m, nil

	case abortedMsg:
		// Show a toast for interrupt outcomes (plan 08c M11 source #2).
		// The status line continues to carry the text for accessibility; the toast
		// is an additional transient notice in the bottom-right corner.
		if msg.err != nil {
			m.status = "interrupt failed: " + msg.err.Error()
			cmd := m.pushToast(toastError, "interrupt failed")
			return m, cmd
		}
		m.status = "interrupted"
		cmd := m.pushToast(toastInfo, "interrupted")
		return m, cmd

	case forkedMsg:
		if msg.err != nil {
			m.status = "fork failed: " + msg.err.Error()
			return m, nil
		}
		if msg.session.ID == "" {
			m.status = "fork: empty response"
			return m, nil
		}
		m.store.sessions = upsertSession(m.store.sessions, msg.session)
		m.cfg.SessionID, m.screen = msg.session.ID, ScreenSession
		m.status = "forked"
		return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)

	case mcpLoadedMsg:
		if msg.err != nil {
			if m.modal == modalMCP {
				m.status = "mcp: " + msg.err.Error()
			}
			return m, nil
		}
		m.mcpServers = msg.items
		return m, nil

	case skillsLoadedMsg:
		if msg.err != nil {
			if m.modal == modalSkills {
				m.status = "skills: " + msg.err.Error()
			}
			return m, nil
		}
		m.skills = msg.items
		return m, nil

	case clipboardCopiedMsg:
		// Show a success toast for every clipboard copy (plan 08c M11 source #1).
		// The inline m.status text is already set by the caller (e.g. "copied turn"
		// or "copied last response") — the toast augments rather than replaces it.
		cmd := m.pushToast(toastSuccess, "copied to clipboard")
		return m, cmd

	case editorDoneMsg:
		if msg.path != "" {
			if b, err := os.ReadFile(msg.path); err == nil {
				m.input.SetValue(strings.TrimRight(string(b), "\n"))
				m.input.CursorEnd()
				m = m.resizeComposer()
			}
			_ = os.Remove(msg.path)
		}
		if msg.err != nil {
			m.status = "editor: " + msg.err.Error()
		}
		return m, nil

	case shellSentMsg:
		if msg.err != nil {
			m.status = "shell failed: " + msg.err.Error()
		}
		return m, nil

	case permissionRepliedMsg:
		m.permReplying = false
		if msg.err != nil {
			// Keep the request so the user can retry — the daemon is still blocked.
			m.status = "permission reply failed (try again): " + msg.err.Error()
			return m, nil
		}
		m.permSel = 0
		m.store.permissions = removeByID(m.store.permissions, msg.id, func(q Permission) string { return q.ID })
		return m, nil

	case questionRepliedMsg:
		m.qReplying = false
		if msg.err != nil {
			m.status = "question reply failed (try again): " + msg.err.Error()
			return m, nil
		}
		m.store.questions = removeByID(m.store.questions, msg.id, func(x Question) string { return x.ID })
		return m.resetQuestion(), nil

	case filesFoundMsg:
		// Apply only if it still matches the active mention query (drop stale
		// results); open only when there's something to show.
		if q, ok := mentionQuery(m.input.Value()); ok && q == msg.query {
			m.ac = autocomplete{open: len(msg.files) > 0, mode: acMention, files: msg.files, sel: clampSel(m.ac.sel, len(msg.files))}
		}
		return m, nil

	case sessionsLoadedMsg:
		if msg.err != nil {
			return m, nil
		}
		for _, ss := range msg.sessions {
			m.store.sessions = upsertSession(m.store.sessions, ss)
		}
		// Open the requested session, else the newest.
		if m.cfg.SessionID == "" && len(msg.sessions) > 0 {
			m.cfg.SessionID = msg.sessions[0].ID
		}
		if m.cfg.SessionID != "" {
			m.screen = ScreenSession
			return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)
		}
		return m, nil

	case messagesLoadedMsg:
		if msg.err == nil {
			m.store = m.store.ingestHistory(msg.sessionID, msg.items)
		}
		m.todos = nil // todos are per-session; refetch for the opened one if the dock is up
		cmds := []tea.Cmd{}
		if msg.sessionID != "" { // keep the sub-agent footer fresh (GET /session/{id}/children)
			cmds = append(cmds, loadChildrenCmd(m.ctx, m.client, msg.sessionID))
		}
		if m.tasksOpen && m.cfg.SessionID != "" {
			cmds = append(cmds, loadTodosCmd(m.ctx, m.client, m.cfg.SessionID))
		}
		return m, tea.Batch(cmds...)

	case connErrMsg:
		m.conn, m.err = ConnError, msg.err
		return m, nil

	case streamOpenedMsg:
		if msg.err != nil {
			m.conn = Reconnecting
			m.status = "reconnecting…"
			cmd := backoffCmd(m.attempt)
			m.attempt++
			return m, cmd
		}
		if m.stream != nil {
			m.stream.Close() // close any prior stream before replacing it
		}
		m.stream = msg.stream
		m.conn = Connected
		m.attempt = 0 // a successful reopen resets the backoff
		return m, listenCmd(m.stream)

	case sseEventMsg:
		m.eventCount++
		prevQ := questionID(m.pendingQuestion())
		m.store = m.store.Reduce(msg.ev)
		if questionID(m.pendingQuestion()) != prevQ { // active question cleared/replaced
			m = m.resetQuestion()
		}
		m.status = fmt.Sprintf("connected · %d events · %d sessions", m.eventCount, len(m.store.sessions))
		cmds := []tea.Cmd{listenCmd(m.stream)}
		// Ring the bell when the agent blocks on input — the terminal may be unfocused.
		if msg.ev.Type == "permission.asked" || msg.ev.Type == "question.asked" {
			cmds = append(cmds, bellCmd())
		}
		// A todowrite tool part changed the todos — refetch (no todo SSE event).
		if m.tasksOpen && m.cfg.SessionID != "" && isTodoWriteEvent(msg.ev) {
			cmds = append(cmds, loadTodosCmd(m.ctx, m.client, m.cfg.SessionID))
		}
		// Kick the animation tick when a tool part arrives — the tick will self-sustain
		// while animating() is true and stop automatically when the turn completes.
		// maybeKickAnim is a no-op when no animation is needed, so this is safe to call
		// on every SSE event.  (plan 08c M9 — spinner.go)
		if kick := m.maybeKickAnim(); kick != nil {
			cmds = append(cmds, kick)
		}
		return m, tea.Batch(cmds...)

	case todosLoadedMsg:
		if msg.err == nil && msg.sessionID == m.cfg.SessionID {
			m.todos = msg.todos
		}
		return m, nil

	case childrenLoadedMsg:
		if msg.err == nil {
			for _, ss := range msg.children {
				m.store.sessions = upsertSession(m.store.sessions, ss)
			}
		}
		return m, nil

	case diffLoadedMsg:
		if !m.diff.open { // reviewer was closed while the fetch was in flight
			return m, nil
		}
		m.diff.loading = false
		if msg.err != nil {
			m.diff.err = msg.err
			return m, nil
		}
		sortFileDiffs(msg.files)
		m.diff.files = msg.files
		m.diff.treeRows = buildDiffTreeRows(msg.files) // cache; rows depend only on files
		if m.diff.sel >= len(msg.files) {
			m.diff.sel = 0
		}
		return m, nil

	case ptyConnectedMsg:
		if msg.gen != m.pty.gen { // a dial from a prior (closed) pane — discard
			msg.conn.Close()
			return m, nil
		}
		m.pty.connecting = false
		if msg.err != nil {
			m.pty.err = msg.err
			return m, nil
		}
		m.pty.id, m.pty.conn = msg.id, msg.conn
		// Reconcile the size: the layout may have changed while dialing (when
		// id was empty, resizePTY couldn't push it to the daemon yet).
		return m, tea.Batch(
			ptyReadCmd(msg.conn, m.pty.gen),
			resizePTYCmd(m.ctx, m.client, msg.id, m.pty.cols, m.pty.rows),
		)

	case ptyOutputMsg:
		if msg.gen != m.pty.gen { // bytes from a stale connection — drop
			return m, nil
		}
		if m.pty.term != nil {
			_, _ = m.pty.term.Write(msg.data)
		}
		if m.pty.conn != nil { // keep pumping while connected
			return m, ptyReadCmd(m.pty.conn, m.pty.gen)
		}
		return m, nil

	case ptyClosedMsg:
		if msg.gen != m.pty.gen { // close of a prior connection — ignore
			return m, nil
		}
		m.pty.connecting = false
		m.pty.conn = nil
		if msg.err != nil {
			m.pty.err = msg.err
		}
		return m, nil

	case sseClosedMsg:
		if m.stream != nil {
			m.stream.Close() // release the closed stream's conn + context
		}
		m.stream = nil
		m.conn = Reconnecting
		m.status = "reconnecting…"
		cmd := backoffCmd(m.attempt)
		m.attempt++
		return m, cmd

	case reconnectMsg:
		return m, openSSECmd(m.ctx, m.client)

	// animTickMsg drives the gradient-scanner spinner and any other per-frame
	// animation (plan 08c M9).  The tick is gated: it only reschedules while
	// animating() is true, so the render loop is never woken at idle.
	//
	// Pattern:
	//   animating() == true  → increment frame, return a new tick cmd
	//   animating() == false → do not reschedule; animation is over
	//
	// The tick is started (kicked) by sseEventMsg and sessionOpenedMsg below,
	// which are the natural entry points for a new assistant turn.
	case animTickMsg:
		// Purge expired toasts on every tick (plan 08c M11).  toastTick() drains
		// the queue in-place; once empty toastsLive() returns false, animating()
		// may return false (if no tools are running), and the tick self-stops.
		m.toastTick()
		if m.animating() {
			m.animFrame++
			return m, animTickCmd()
		}
		// Not animating — stop; the next animating state will re-kick via maybeKickAnim.
		return m, nil
	}
	return m, nil
}

// handleModalKey routes keys while a command overlay is open.
func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The rename overlay is a text input: enter submits, esc cancels, everything
	// else edits the field.
	if m.modal == modalRename {
		switch msg.String() {
		case "esc":
			m.modal, m.renameInput = modalNone, blurInput(m.renameInput)
			return m, nil
		case "enter":
			return m.modalSelect()
		}
		var cmd tea.Cmd
		m.renameInput, cmd = m.renameInput.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "esc":
		m.modal, m.modalSel = modalNone, 0
		return m, nil
	case "up", "k", "ctrl+p":
		if m.modalSel > 0 {
			m.modalSel--
		}
		return m, nil
	case "down", "j", "ctrl+n":
		if m.modal == modalSessions && msg.String() == "ctrl+n" {
			m.modal = modalNone
			return m, newSessionCmd(m.ctx, m.client)
		}
		if m.modalSel < m.modalCount()-1 {
			m.modalSel++
		}
		return m, nil
	case "enter":
		return m.modalSelect()
	case "ctrl+d":
		if m.modal == modalSessions {
			if ss := m.orderedSessions(); m.modalSel < len(ss) {
				return m, deleteSessionCmd(m.ctx, m.client, ss[m.modalSel].ID)
			}
		}
		if m.modal == modalStash && m.modalSel < len(m.stash) {
			m = m.deleteStash(m.modalSel)
			if m.modalSel > 0 && m.modalSel >= m.modalCount() {
				m.modalSel = m.modalCount() - 1
			}
		}
		return m, nil
	case "y":
		if m.modal == modalTimeline {
			if items := m.timelineItems(); m.modalSel < len(items) {
				if txt := m.messageText(items[m.modalSel].messageID); txt != "" {
					m.modal, m.modalSel = modalNone, 0
					m.status = "copied turn"
					return m, copyClipboardCmd(txt)
				}
			}
		}
		return m, nil
	}
	return m, nil
}

// maxComposerRows caps the auto-growing composer; beyond it the textarea scrolls.
const maxComposerRows = 8

// resizeComposer sets the composer's width to the content column and grows its
// height to fit the current text (clamped to [1, maxComposerRows]). Height is
// the number of WRAPPED visual rows, so a long line with no newline grows the
// box just like explicit newlines do.
func (m Model) resizeComposer() Model {
	m.streamWidth = m.leftColumnWidth() // size to the left column (sidebar-aware)
	cols := m.barWidth() - 1            // inside the accent bar: barWidth less its left padding
	if cols < 1 {
		cols = 1
	}
	m.input.SetWidth(cols)
	h := visualRows(m.input.Value(), cols)
	if h > maxComposerRows {
		h = maxComposerRows
	}
	m.input.SetHeight(h)
	return m
}

// visualRows estimates how many rows text occupies once soft-wrapped at cols
// columns: the sum over logical (newline-separated) lines of ceil(width/cols),
// each line at least one row. Display width (not byte length) so wide runes
// count correctly. Char-wrap vs the textarea's word-wrap means this only ever
// UNDERestimates (by a row for word boundaries, exact-fit lines, or tabs) —
// never over — so the box is at worst one row short, never padded with blanks.
func visualRows(text string, cols int) int {
	if cols < 1 {
		cols = 1
	}
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		n := 1
		if w := lipgloss.Width(line); w > cols {
			n = (w + cols - 1) / cols
		}
		rows += n
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// handleLeaderKey dispatches a ctrl+x chord to the matching modal/action (design
// app.jsx:231-232). An unmapped key is a no-op (the leader is already cleared).
func (m Model) handleLeaderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "l":
		m.modal, m.modalSel = modalSessions, 0
		return m, loadSessionsCmd(m.ctx, m.client)
	case "n":
		return m, newSessionCmd(m.ctx, m.client)
	case "m":
		m.modal, m.modalSel = modalModels, m.modelSelIndex()
		return m, loadProvidersCmd(m.ctx, m.client)
	case "a":
		m.modal, m.modalSel = modalAgents, m.agentSelIndex()
		return m, loadAgentsCmd(m.ctx, m.client)
	case "g":
		m.modal, m.modalSel = modalTimeline, 0
		return m, nil
	case "s":
		m.modal, m.modalSel = modalStatus, 0
		return m, nil
	case "p":
		m.modal, m.modalSel = modalPalette, 0
		return m, nil
	case "b":
		m.sidebarHidden = !m.sidebarHidden
		return m.resizeComposer(), nil // width changed → re-fit the composer
	case "t":
		m.tasksOpen = !m.tasksOpen
		if m.tasksOpen {
			return m, loadTodosCmd(m.ctx, m.client, m.cfg.SessionID)
		}
		return m, nil
	case "y":
		if txt := m.lastAssistantText(); txt != "" {
			m.status = "copied last response"
			return m, copyClipboardCmd(txt)
		}
		m.status = "nothing to copy"
		return m, nil
	case "r":
		m.view.hideThinking = !m.view.hideThinking
		m.status = toggleHint("thinking", !m.view.hideThinking)
		return m, nil
	case "f":
		// Toggle the expanded/collapsed state of the reasoning block (plan 08c M7).
		// ctrl+x r hides thinking entirely; ctrl+x f expands/collapses the full text.
		m.view.expandedThinking = !m.view.expandedThinking
		if m.view.expandedThinking {
			m.status = "thought: expanded"
		} else {
			m.status = "thought: collapsed"
		}
		return m, nil
	case "o":
		m.view.hideTools = !m.view.hideTools
		m.status = toggleHint("tool output", !m.view.hideTools)
		return m, nil
	case "v":
		// Toggle collapse on the last tool part (plan 08c M7 per-tool collapse).
		// ctrl+x v collapses/expands the most recent tool's output panel.
		if id := m.lastToolPartID(); id != "" {
			m.view = m.view.toggleToolCollapse(id)
			if m.view.isToolCollapsed(id) {
				m.status = "tool output: collapsed"
			} else {
				m.status = "tool output: expanded"
			}
		} else {
			m.status = "no tool to fold"
		}
		return m, nil
	case "e":
		return m, openEditorCmd(m.input.Value())
	case "d":
		return m.openDiff() // full-screen diff reviewer
	case "`":
		return m.focusOrOpenPTY() // embedded terminal (ctrl+] to exit)
	case "w":
		return m.stashDraft(), nil // park the current composer draft
	case "down":
		return m.enterFirstChild() // descend into the first sub-agent child
	case "up":
		return m.gotoParent() // return to the parent session
	case "]":
		return m.cycleSibling(+1) // next sibling sub-agent
	case "[":
		return m.cycleSibling(-1) // previous sibling sub-agent
	}
	return m, nil
}

// submit sends the composer's text: it creates a session first if none is open,
// then prompts. In shell mode it runs the text as a shell command instead.
// Requires a resolved model.
func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	if !m.model.ok() {
		m.status = "no model configured (pass --provider/--model)"
		return m, nil
	}
	m = m.pushHistory(text) // remember the submission for up/down recall
	m.persist()
	// Shell mode: run the text as a command in the open session; output streams
	// back as tool parts. Requires an existing session (no implicit create).
	if m.shellMode {
		m.shellMode = false
		if m.cfg.SessionID == "" {
			m.status = "open a session before running a shell command"
			return m, nil
		}
		m.input.SetValue("")
		m = m.resizeComposer()
		return m, shellCmd(m.ctx, m.client, m.cfg.SessionID, text, m.effectiveAgent(), m.model)
	}
	m.input.SetValue("")
	m = m.resizeComposer() // collapse back to one row
	m.scrollOffset = 0     // a new prompt snaps the stream back to the live tail
	if m.cfg.SessionID == "" {
		return m, createSessionCmd(m.ctx, m.client, text)
	}
	return m, promptCmd(m.ctx, m.client, m.cfg.SessionID, text, m.model, m.agent)
}

// scrollStep is the lines moved per scrollback keypress.
const scrollStep = 3

// historyRecall walks the prompt history with up (dir -1, older) / down (dir +1,
// newer). It only starts browsing from an empty composer (so a draft isn't
// clobbered); walking past the newest entry exits and clears. Returns false when
// the key should fall through to normal composer editing.
func (m Model) historyRecall(dir int) (Model, bool) {
	if len(m.history) == 0 {
		return m, false
	}
	if m.histIdx == -1 {
		if dir > 0 || strings.TrimSpace(m.input.Value()) != "" {
			return m, false // down with no browse, or a non-empty draft → fall through
		}
		m.histIdx = len(m.history) - 1
	} else {
		m.histIdx += dir
		if m.histIdx < 0 {
			m.histIdx = 0
		}
		if m.histIdx >= len(m.history) { // walked past newest → live composer
			m.histIdx = -1
			m.input.SetValue("")
			return m.resizeComposer(), true
		}
	}
	m.input.SetValue(m.history[m.histIdx])
	m.input.CursorEnd()
	return m.resizeComposer(), true
}

// effectiveAgent resolves an agent name for endpoints that require one (shell):
// the selected agent, else a "build"-named agent, else the first available, else
// "build" as a last resort.
func (m Model) effectiveAgent() string {
	if m.agent != "" {
		return m.agent
	}
	for _, a := range m.agents {
		if a.name == "build" {
			return a.name
		}
	}
	if len(m.agents) > 0 {
		return m.agents[0].name
	}
	return "build"
}

// View renders the active screen (or the command overlay when one is open). The
// default theme renders on the terminal's native background (no full-screen fill,
// so it doesn't paint a mismatched box); an explicitly chosen light/mono theme
// paints its background so it stays legible on any terminal.
func (m Model) View() string {
	var body string
	switch {
	case m.pendingPermission() != nil:
		body = m.permissionView()
	case m.pendingQuestion() != nil:
		body = m.questionView()
	case m.diff.open:
		body = m.diffView()
	case m.modal != modalNone:
		body = m.modalView()
	case m.screen == ScreenSession:
		body = m.viewSession()
	default:
		body = m.viewSplash()
	}
	if m.width == 0 || m.height == 0 {
		return body
	}
	// Composite onto a full-screen canvas painted with the theme background. This
	// truncates each row to width (no wrapping → the frame never exceeds m.height,
	// so the terminal doesn't scroll the footer/sidebar) and paints every gap/empty
	// cell with the base bg (mirrors opencode's opentui per-cell compositor). A plain
	// lipgloss Background wrap can't do this — its inner resets leave a black void.
	filled := paintBackground(body, m.width, m.height, m.styles.P.Bg)
	// Composite the toast overlay onto the bottom-right of the Bg-filled frame.
	// overlayToasts replaces the rightmost N columns of the last K rows with the
	// toast box; each toast cell carries its own BgElev background so it renders
	// correctly over the Bg-filled surface (plan 08c M11).
	return m.overlayToasts(filled)
}

// viewSplash renders the wordmark, the composer, and the connection status.
func (m Model) viewSplash() string {
	s := m.styles
	// Each splash line is rendered as a single full-width, center-aligned, Bg-painted
	// style → one SGR run per line, so the whole row (text + padding) carries the
	// theme background with no mid-line reset. Lipgloss emits a reset after every
	// styled run, so per-segment backgrounds leave the rest of the row transparent
	// (terminal-dark bleed on a light terminal); one style per line avoids that
	// entirely. plan 08c Tier 0.
	w := m.width
	if w <= 0 {
		// No layout yet — fall back to plain stacking (used only before the first
		// WindowSizeMsg; View() returns body unpainted in that case anyway).
		return lipgloss.JoinVertical(lipgloss.Center,
			s.Base.Bold(true).Render("opcode42"), "", m.composerView(), "",
			s.Faint.Render("enter send · ctrl+j newline · ctrl+p commands · ctrl+c quit"))
	}
	fill := func(st lipgloss.Style, content string) string {
		return st.Background(s.P.Bg).Width(w).Align(lipgloss.Center).Render(content)
	}
	blank := lipgloss.NewStyle().Background(s.P.Bg).Width(w).Render("")

	// Block-pixel "opcode42" logo with left→right shimmer sweep (plan 08c M10).
	// logoFrame returns one string per row; each row is then full-width Bg-filled via
	// fill() so no transparent cell escapes to the terminal background.
	logoRows := logoFrame(m.animFrame, s.P)
	logoLines := make([]string, len(logoRows))
	for i, row := range logoRows {
		logoLines[i] = fill(lipgloss.NewStyle(), row)
	}
	wordmark := lipgloss.JoinVertical(lipgloss.Left, logoLines...)

	composer := m.composerView()
	if ac := m.autocompleteView(); ac != "" {
		composer = lipgloss.JoinVertical(lipgloss.Left, ac, composer)
	}
	// The composer is a fixed-width bordered block; center it on a Bg-filled row.
	composer = lipgloss.PlaceHorizontal(w, lipgloss.Center, composer,
		lipgloss.WithWhitespaceBackground(s.P.Bg))

	status := fill(s.Faint, m.statusLine())
	if m.err != nil {
		status = fill(lipgloss.NewStyle().Foreground(s.P.Red), m.err.Error())
	}
	hint := fill(s.Faint, "enter send · ctrl+j newline · ctrl+p commands · ctrl+c quit")

	body := lipgloss.JoinVertical(lipgloss.Left, wordmark, blank, composer, blank, hint, blank, status)
	if m.height == 0 {
		return body
	}
	// Body rows are already full-width Bg-painted; Place only adds vertical padding,
	// which WithWhitespaceBackground fills with the theme Bg too.
	return lipgloss.Place(w, m.height, lipgloss.Center, lipgloss.Center, body,
		lipgloss.WithWhitespaceBackground(s.P.Bg))
}

func (m Model) viewSession() string { return m.renderSession() }
