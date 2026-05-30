// Package tui is the Forge terminal client: a Bubble Tea app over the
// opencode/Forge wire protocol (via the Go SDK, plan 06). It is wire-generic —
// point it at a Forge or a real opencode daemon. Design: design/tui/ (plan 08).
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rotemmiz/forge/internal/tui/theme"
	forgeclient "github.com/rotemmiz/forge/sdk/go"
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
	client     *forgeclient.ForgeClient
	ctx        context.Context
	cancel     context.CancelFunc
	stream     *forgeclient.EventStream
	attempt    int // reconnect backoff attempt
	eventCount int // events seen this connection (status line)

	// store mirrors the daemon state from the SSE stream.
	store store

	// Composer.
	input textarea.Model
	model promptModel

	// Command overlay.
	modal    modalKind
	modalSel int

	// choices is the connected provider/model catalog (model switcher).
	choices []modelChoice
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
	m := Model{
		cfg:    cfg,
		styles: theme.DefaultStyles(),
		screen: ScreenSplash,
		conn:   Connecting,
		status: "connecting to " + cfg.URL,
		ctx:    ctx,
		cancel: cancel,
		store:  newStore(),
		input:  ta,
		model:  promptModel{Provider: cfg.Provider, Model: cfg.Model},
	}
	c, err := forgeclient.New(cfg.URL, forgeclient.Options{
		Directory: cfg.Directory, Username: cfg.Username, Password: cfg.Password,
	})
	if err != nil {
		m.conn, m.err = ConnError, err
		return m
	}
	m.client = c
	return m
}

// Init kicks off the daemon health check.
func (m Model) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return healthCmd(m.ctx, m.client)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The composer placeholder follows the screen (design app.jsx:355).
	if m.screen == ScreenSession {
		m.input.Placeholder = "Reply, or / for commands"
	} else {
		m.input.Placeholder = "Ask anything…"
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m = m.resizeComposer()
		return m, nil

	case tea.KeyMsg:
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
		// A modal captures navigation/selection keys.
		if m.modal != modalNone {
			return m.handleModalKey(msg)
		}
		switch msg.String() {
		case "ctrl+p":
			m.modal, m.modalSel = modalPalette, 0
			return m, nil
		case "enter":
			return m.submit()
		}
		// Everything else goes to the composer (shift+enter / ctrl+j add a newline).
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m = m.resizeComposer() // auto-grow to fit the new content
		return m, cmd

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
		}
		return m, nil

	case connectedMsg:
		m.conn, m.status, m.attempt = Connected, "connected", 0
		// Subscribe to events, bootstrap the session list, resolve the model, and
		// preload the provider catalog so the model switcher opens populated.
		return m, tea.Batch(openSSECmd(m.ctx, m.client), loadSessionsCmd(m.ctx, m.client), loadConfigCmd(m.ctx, m.client), loadProvidersCmd(m.ctx, m.client))

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

	case sessionCreatedMsg:
		if msg.err != nil {
			m.status = "create session failed: " + msg.err.Error()
			return m, nil
		}
		m.store.sessions = upsertSession(m.store.sessions, msg.session)
		m.cfg.SessionID = msg.session.ID
		m.screen = ScreenSession
		return m, promptCmd(m.ctx, m.client, msg.session.ID, msg.text, m.model)

	case promptSentMsg:
		if msg.err != nil {
			m.status = "prompt failed: " + msg.err.Error()
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
		return m, nil

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
		m.store = m.store.Reduce(msg.ev)
		m.status = fmt.Sprintf("connected · %d events · %d sessions", m.eventCount, len(m.store.sessions))
		return m, listenCmd(m.stream)

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
	}
	return m, nil
}

// handleModalKey routes keys while a command overlay is open.
func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	cols := m.barWidth() - 1 // inside the accent bar: barWidth less its left padding
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

// submit sends the composer's text: it creates a session first if none is open,
// then prompts. Requires a resolved model.
func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	if !m.model.ok() {
		m.status = "no model configured (pass --provider/--model)"
		return m, nil
	}
	m.input.SetValue("")
	m = m.resizeComposer() // collapse back to one row
	if m.cfg.SessionID == "" {
		return m, createSessionCmd(m.ctx, m.client, text)
	}
	return m, promptCmd(m.ctx, m.client, m.cfg.SessionID, text, m.model)
}

// View renders the active screen (or the command overlay when one is open).
func (m Model) View() string {
	if m.modal != modalNone {
		return m.modalView()
	}
	switch m.screen {
	case ScreenSession:
		return m.viewSession()
	default:
		return m.viewSplash()
	}
}

// viewSplash renders the wordmark, the composer, and the connection status.
func (m Model) viewSplash() string {
	s := m.styles
	wordmark := s.Base.Bold(true).Render("forge")
	composer := m.composerView()
	status := s.Faint.Render(m.statusLine())
	if m.err != nil {
		status = lipgloss.NewStyle().Foreground(s.P.Red).Render(m.err.Error())
	}
	hint := s.Faint.Render("enter send · ctrl+j newline · ctrl+p commands · ctrl+c quit")

	body := lipgloss.JoinVertical(lipgloss.Center, wordmark, "", composer, "", hint, "", status)
	if m.width == 0 || m.height == 0 {
		return body
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m Model) viewSession() string { return m.renderSession() }
