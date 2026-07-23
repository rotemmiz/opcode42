// Package tui is the Opcode42 terminal client: a Bubble Tea app over the
// opencode/Opcode42 wire protocol (via the Go SDK, plan 06). It is wire-generic —
// point it at a Opcode42 or a real opencode daemon. Design: design/tui/ (plan 08).
package tui

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rotemmiz/opcode42/internal/mdns"
	"github.com/rotemmiz/opcode42/internal/tui/theme"
	"github.com/rotemmiz/opcode42/scrollregion"
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

	// NoDiscover disables mDNS browsing in the connect overlay (plan 08e §D3).
	// Set by --no-discover for airgapped / CI / scripted runs. The manual URL
	// field still works; only the nearby-servers list is suppressed.
	NoDiscover bool

	// NoAnim disables all per-frame animation (static logo, bg-pulse off) for
	// capture / accessibility (plan 08e §B3). Set by --no-anim.
	NoAnim bool

	// Sixel forces Sixel capability detection on (plan 08e §E2). Set by
	// --sixel for terminals that support Sixel graphics (VT340+:
	// "xterm -ti vt340", mlterm, wezterm, kitty) but don't advertise it via
	// $TERM. Image rendering is still gated behind viewState.images
	// (ctrl+x i); this flag only flips the capability probe so sixel escapes
	// are emitted instead of the placeholder when images are enabled.
	Sixel bool

	// NoOSC52 forces the OSC 52 clipboard-write escape off regardless of the
	// environment-based default or any persisted preference (plan 08f H11 /
	// G.13). Set by --no-osc52.
	NoOSC52 bool

	// TUIConfigPath is the TUI config file path override (plan 08f H12 /
	// G.14 — OPENCODE_TUI_CONFIG). Consumed by New() via loadMergedTUIConfig
	// (plan 08f H13 / G.15).
	TUIConfigPath string
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
	// exiting is the two-press ctrl+c exit guard (opencode footer.ts:987-1006):
	// the first ctrl+c on an empty composer arms it (status bar shows the EXIT
	// chip + "press ctrl+c again to exit"); a second ctrl+c quits; any other
	// key, a prompt submit, or a 5s timeout cancels it. Mirrors opencode's
	// exit counter + armExitTimer.
	exiting bool
	// deleting is the two-press ctrl+d delete-session guard (plan 08f H1a):
	// first ctrl+d arms it; second deletes the open session; any other key
	// cancels. Mirrors the exiting guard pattern.
	deleting bool

	// Command overlay.
	modal        modalKind
	modalSel     int
	renameInput  textinput.Model // text-input overlay (rename current session)
	mcpServers   []mcpItem       // read-only MCP list (GET /mcp)
	lspServers   []lspItem       // read-only LSP status list (GET /lsp; refreshed on lsp.updated)
	mcpResources []mcpResource   // MCP resources for @-mention (GET /experimental/resource; 08f H10)
	skills       []skillItem     // read-only skills list (GET /skill)
	// permState is the 3-stage permission UI state machine (plan 17 §B3):
	// permission → always confirm → reject message. The render path
	// (permission.go permissionView) reads it; the key path
	// (handlePermissionKey) drives it through the pure transition functions
	// in permission_state.go. The per-request id tracks which pending
	// permission the state is for; when the active pending permission
	// changes the state is reset by handlePermissionKey.
	permState     permissionState
	permRequestID string // the id of the pending permission m.permState is for

	// Connect overlay (plan 08e §D2): mDNS-discovered daemons + a manual URL
	// field. discoverCtx/cancel own the D1 browser lifecycle (started on open,
	// cancelled on close). connectURLInput is the manual-entry field at the
	// top of the overlay; connectFieldFocus is true when the URL field (not
	// the server list) owns the cursor. serverProbe is the best-effort
	// /global/health reachability cache keyed by host:port. discoverOut is
	// the live browser channel (stashed so discoverNextCmd can pump it).
	discoveredServers []mdns.DiscoveredService
	discoverCtx       context.Context
	discoverCancel    context.CancelFunc
	discoverOut       <-chan mdns.DiscoveredService
	connectURLInput   textinput.Model
	connectFieldFocus bool
	serverProbe       map[string]serverProbeState

	// Question footer panel (plan 17 §B5): a pure state machine
	// (question_state.go questionBodyState) drives the multi-question tab
	// flow, the Confirm review tab, and the custom-text answer field. The
	// render path (question.go questionView) reads it; the key path
	// (handleQuestionKey) drives it through the pure transition functions
	// in question_state.go. The per-request id tracks which pending question
	// the state is for; questionSync resets the state when the active
	// pending question changes.
	qBody questionBodyState
	// qDeferredSSE holds an SSE question.replied/rejected event for OUR own
	// pending question that arrived while qBody.replying was true (plan 08e
	// §E4). The event is deferred so the local reply path
	// (questionRepliedMsg) can record the locally-selected labels before the
	// store clears the pending question; questionRepliedMsg applies it after
	// recording. Zero-valued (Type == "") when no event is deferred.
	qDeferredSSE opcode42client.SSEEvent

	// choices is the connected provider/model catalog (model switcher).
	choices []modelChoice

	// Slash commands.
	commands []slashItem  // daemon commands (GET /command)
	ac       autocomplete // composer "/" popup state

	// Chrome.
	agent          string              // active agent (status bar "mode"); empty → default
	agents         []agentItem         // selectable agents (GET /agent)
	themeName      string              // active theme name (theme switcher)
	sidebarHidden  bool                // right sidebar visibility (toggle: ctrl+x b)
	streamWidth    int                 // transient: stream column width when the sidebar is shown
	leader         bool                // ctrl+x leader pressed, awaiting the chord key
	tasksOpen      bool                // tasks dock visibility (toggle: ctrl+x t)
	todos          []Todo              // current session's todos (tasks dock)
	scroll         scrollregion.Region // stream scrollback viewport (0 == live tail)
	view           viewState           // display toggles (timestamps, tool output, thinking)
	history        []string            // submitted prompts (persisted; recalled with up/down when empty)
	histIdx        int                 // browse cursor into history (-1 = not browsing)
	persistEnabled bool                // gate local-KV reads/writes (off in tests; on via Restore)
	// revertMessageID is the local undo checkpoint (opencode session.revert.messageID).
	// Set after a successful revert; cleared on unrevert. undoLastTurn skips user
	// messages at/after this id so repeated undos walk further back (08f H1b).
	revertMessageID string
	// messageActionID is the user message targeted by modalMessage
	// (DialogMessage — plan 08f H9). Cleared when the modal closes.
	messageActionID string

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
	// Also hosts the trailing streaming-block entries used by the incremental
	// streaming path (renderMarkdownStreaming) — the streaming block is hashed
	// per-frame and cached for the duration of one delta.
	mdCache mdCache

	// mdBlockCache is the per-stable-block incremental cache for streaming
	// markdown (plan 17 §D3). Key: (partID, blockIdx, width, theme). A growing
	// part's text is split into stable blocks (blank-line-terminated) + a
	// trailing streaming block; stable blocks are rendered once and served
	// from this cache, only the streaming block re-renders each frame. This
	// mirrors opencode's commitMarkdownBlocks + _stableBlockCount
	// (run/scrollback.surface.ts:287-305) and avoids the O(n²) re-parse a
	// full-text cache would incur on every delta.
	mdBlockCache mdBlockCache

	// diffCache is the rendered inline-diff cache for completed edit/apply_patch
	// tools (plan 17 Workstream C). Diffs arrive complete at phase=final and are
	// immutable thereafter, so the fully-rendered styled string can be cached
	// by (partID, patchHash, width, themeName) and reused across frames. This is
	// critical: toolRow runs every animation tick and re-rendering a multi-hunk
	// diff (with syntax highlighting) every frame would dominate the render
	// budget. The map is a reference type; ensureDiffCache initialises it on
	// the root Model so all copies share one map.
	diffCache diffRenderCache

	// animFrame is the monotonic animation frame counter incremented on each
	// animTickMsg.  Passed to scannerFrame() and (later) logo shimmer.
	// Reset to 0 when a new session opens so the sweep always starts from the left.
	// (plan 08c M9 — spinner.go)
	animFrame int

	// noAnim disables all per-frame animation when true (plan 08e §B3): the
	// logo paints at the static peak frame (logoPeakFrame, the same frame
	// logoStatic returns) and the bg-pulse is frozen off (the breath tint is
	// disabled, not animated). Set by the --no-anim CLI flag for
	// deterministic screenshot capture (tools/tui-shots) and accessibility
	// (vestibular sensitivity).
	noAnim bool

	// sixel forces Sixel capability on (plan 08e §E2). Set by the --sixel
	// CLI flag; read by renderImagePart's capability probe. Image rendering
	// is still gated behind viewState.images (ctrl+x i).
	sixel bool

	// toasts is the live toast queue (plan 08c M11).  Entries expire after
	// toastTTL; the animTick drives TTL countdown via toastTick().
	// pushToast enqueues and toastTick purges; the canvas composites the
	// toast layer at zToast bottom-right over the base Bg fill (canvas.go).
	toasts []toast

	// childStatusMap caches child session statuses (childID →
	// "running"/"completed"/etc) computed once per store change (plan 20
	// §1a). Without this, childStatus() does O(parent-msgs × parent-parts)
	// JSON decodes per child per frame — 75% of CPU with 52 subagents. The
	// map is a reference type; ensureMDCache initialises it on the root
	// Model so all copies share one map.
	childStatusMap map[string]string
	// childStatusVersion tracks the store.version when childStatusMap was
	// last recomputed. recomputeChildStatuses skips the O(sessions × msgs
	// × parts) scan when the version is unchanged (e.g. anim ticks, PTY
	// output, composer keypresses don't change child statuses).
	childStatusVersion int

	// animatingCache is the cached result of animating(), computed once per
	// Update cycle (plan 20 §1b). Without this, animating() iterates all
	// session messages × parts with JSON decodes every frame (called from
	// statusBarView).
	animatingCache bool

	// footerRendered is the pre-rendered footer string, computed in Update
	// (plan 20 §2). sessionLayers reads this directly — zero lipgloss
	// renders in View.
	footerRendered string
	// footerHeight is the pre-computed height of footerRendered.
	footerHeight int

	// sidebarRendered is the pre-rendered sidebar string, computed in
	// Update (plan 20 §3). sessionLayers reads this directly.
	sidebarRendered string

	// bodyLines is the pre-rendered body as individual lines, computed in
	// Update (plan 20 §4). View() just windows this via frameStreamLines —
	// zero rendering in View.
	bodyLines []string

	// frameCanvas is the reused lipgloss canvas for composeCanvas (plan 20
	// Layer 4 / 08f perf gate). Allocated/resized in Update on
	// WindowSizeMsg; View only Clear()s and redraws — avoids NewCanvas +
	// screen-buffer alloc per scroll frame (~260KB/op before reuse).
	frameCanvas *lipgloss.Canvas

	// pendingFiles are clipboard image attachments staged for the next
	// submit (plan 08f H2 / opencode pasteAttachment). Cleared on send.
	pendingFiles []pendingFile

	// terminalTitleEnabled gates OSC 0 window titles (plan 08f H6 / G.7).
	// Default true; persisted as terminal_title_enabled KV; suppressed when
	// OPENCODE_DISABLE_TERMINAL_TITLE is set.
	terminalTitleEnabled bool

	// mouseDisabled gates mouse capture (plan 08f H12 / G.14 —
	// OPENCODE_DISABLE_MOUSE, mirrors opencode app.tsx:197). Set once in
	// New() from the environment; View() reports MouseModeNone instead of
	// MouseModeAllMotion when set. There is no CLI flag or KV persistence —
	// opencode's flag is env-only. File-config `mouse: false` (H13) can also
	// set this when the env var is unset.
	mouseDisabled bool

	// scrollStep is the stream/diff scroll line increment (plan 08f H13).
	// Zero means use defaultScrollStep. Overridden by tui.json scroll_speed.
	scrollStep int

	// leaderTimeoutMs is the configured leader-key timeout from tui.json
	// (plan 08f H13). Stored for future armed-leader expiry; 0 = unset.
	leaderTimeoutMs int

	// composerMaxRows overrides maxComposerRows when set from prompt.max_height.
	composerMaxRows int

	// tuiKeybinds holds keybind overrides loaded from tui.json / opencode.json
	// (plan 08f H13). Remapping the Update switch is deferred; the map is
	// retained so a shared config parses cleanly today.
	tuiKeybinds map[string]string

	// fastBoot mirrors OPENCODE_FAST_BOOT (plan 08f H12 / G.14, read in
	// opencode's app.tsx:272-273 via process.env, not flag.ts). New() uses
	// it to jump straight to ScreenSession when a session id is already
	// known, or to freeze the splash animation when it isn't. Stored (not
	// just consulted transiently) so Restore()'s KV-based animation
	// preference doesn't clobber the fast-boot animation freeze, the same
	// way cfg.NoAnim survives Restore().
	fastBoot bool

	// osc52Enabled gates OSC 52 clipboard-write escapes (plan 08f H11 / G.13).
	// Default on locally, off over SSH (SSH_CONNECTION / SSH_TTY set);
	// persisted as osc52_write_enabled KV; forced off by --no-osc52.
	osc52Enabled bool

	// pasteSummaryEnabled collapses large pastes to [Pasted ~N lines] chips
	// (plan 08f H3). Default true; persisted as paste_summary_enabled.
	pasteSummaryEnabled bool

	// pasteParts are smart-pasted blobs staged for the next submit (08f H3).
	pasteParts []pastePart

	// fileContextEnabled and sessionDirFilterEnabled mirror opencode's
	// file_context_enabled / session_directory_filter_enabled kv toggles
	// (plan 08f H7 / G.11). Default true; persisted. Neither has a render
	// consumer yet in Opcode42 (no @-mention file-context injection or
	// session directory scoping exists to gate) — the palette entries
	// toggle+persist the preference so the KV contract exists ahead of
	// those features landing; documented future work.
	fileContextEnabled      bool
	sessionDirFilterEnabled bool

	// themeModeLocked pins the dark/light mode (m.termDark) across launches
	// when true, mirroring opencode's theme_mode_lock (plan 08f H7 / G.12).
	// When false, New() re-detects the terminal background on every launch.
	themeModeLocked bool
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
	ta.Placeholder = `Ask anything... "Fix a TODO in the codebase"`
	ta.Prompt = ""                   // we draw our own blue accent bar (composerView)
	ta.ShowLineNumbers = false       //
	ta.CharLimit = 0                 // no limit — prompts can be long
	ta.SetHeight(1)                  // grows with content up to maxComposerRows
	ta.KeyMap.InsertNewline.SetKeys( // Enter submits; these add a newline
		"shift+enter", "ctrl+j", "ctrl+enter", "alt+enter",
	)
	// Drop the focused cursor-line highlight + base frame to keep the minimal look.
	// bubbles v2 exposes styles via Styles()/SetStyles (a copy) rather than the
	// mutable FocusedStyle/BlurredStyle fields of v1.
	tst := ta.Styles()
	tst.Focused.CursorLine = lipgloss.NewStyle()
	tst.Focused.Base = lipgloss.NewStyle()
	tst.Blurred.CursorLine = lipgloss.NewStyle()
	ta.SetStyles(tst)
	ta.Focus()
	ri := textinput.New()
	ri.Placeholder = "Session title"
	ri.CharLimit = 200
	// The connect overlay's manual URL field (plan 08e §D2). Pre-filled with
	// cfg.URL when the overlay opens (openConnectModal), so a first-run user
	// sees the default 127.0.0.1:4096 hint and a returning user sees their
	// pinned URL.
	ci := textinput.New()
	ci.Placeholder = "http://host:port"
	ci.CharLimit = 200
	// status is the initial connection message. When a URL is known it
	// reads "connecting to <url>"; the first-run flow (empty URL, no client
	// yet) shows a neutral message — the connect overlay is what the user
	// sees, not a connection attempt.
	initialStatus := "connecting to " + cfg.URL
	if cfg.URL == "" {
		initialStatus = "ready"
	}
	m := Model{
		cfg:                     cfg,
		screen:                  ScreenSplash,
		conn:                    Connecting,
		status:                  initialStatus,
		ctx:                     ctx,
		cancel:                  cancel,
		store:                   newStore(),
		input:                   ta,
		renameInput:             ri,
		connectURLInput:         ci,
		serverProbe:             map[string]serverProbeState{},
		model:                   promptModel{Provider: cfg.Provider, Model: cfg.Model},
		terminalTitleEnabled:    true, // plan 08f H6 — default on (opencode kv default)
		pasteSummaryEnabled:     true, // plan 08f H3 — default on
		fileContextEnabled:      true, // plan 08f H7 — default on
		sessionDirFilterEnabled: true, // plan 08f H7 — default on
	}
	// OPENCODE_DISABLE_TERMINAL_TITLE also applies at construction time so
	// callers that skip Restore() (tests, embedders) still see the title
	// suppressed (plan 08f H12 / G.14; Restore() re-applies this after the
	// persisted-KV lookup so the env var still wins either way).
	if os.Getenv("OPENCODE_DISABLE_TERMINAL_TITLE") != "" {
		m.terminalTitleEnabled = false
	}
	// OPENCODE_DISABLE_MOUSE turns off mouse capture entirely (plan 08f H12
	// / G.14, mirrors opencode's app.tsx:197). Env-only — opencode has no
	// CLI flag or persisted preference for it, so neither do we.
	m.mouseDisabled = os.Getenv("OPENCODE_DISABLE_MOUSE") != ""
	m.scrollStep = defaultScrollStep
	// Plan 08f H13 / G.15: overlay opencode-compatible TUI file config
	// (OPENCODE_TUI_CONFIG / tui.json / opencode.json) onto flags + defaults.
	m.applyTUIFileConfig(loadMergedTUIConfig(cfg.TUIConfigPath, cfg.Directory))
	// The bg-pulse is on by default for the splash (plan 08e §B2). It is
	// turned off when the session screen is entered (sessionOpenedMsg /
	// sessionCreatedMsg / sessionsLoadedMsg / forkedMsg) and back on when
	// all sessions are closed (sessionDeletedMsg re-enters the splash).
	m.view.bgPulse = true
	// hideThinking defaults to true (collapsed) — matches opencode's full
	// TUI default ("hide", tui/context/thinking.ts:36). Reasoning parts
	// still render a 1-line "+ Thought: <title> · <duration>" header so the
	// user sees that reasoning happened; the body is hidden until toggled
	// (plan 17 §D1).
	m.view.hideThinking = true
	// --no-anim: freeze the logo and bg-pulse at their peak frame for
	// deterministic screenshot capture (tools/tui-shots) and accessibility.
	m.noAnim = cfg.NoAnim
	// --sixel: force Sixel capability on for terminals that support it but
	// don't advertise it via $TERM (plan 08e §E2).
	m.sixel = cfg.Sixel
	// OSC 52 clipboard writes default on locally, off over SSH; --no-osc52
	// forces off regardless (plan 08f H11 / G.13). Restore() will apply any
	// persisted KV override when the CLI flag was not passed.
	m.osc52Enabled = !cfg.NoOSC52 && defaultOsc52WriteEnabled()
	// OPENCODE_FAST_BOOT skips the splash "screen" (plan 08f H12 / G.14,
	// opencode app.tsx:272-273): jump straight to ScreenSession when a
	// session id is already known (--session), otherwise freeze the splash
	// logo/bg-pulse animation so the connecting screen paints once instead
	// of animating while sessions load.
	m.fastBoot = os.Getenv("OPENCODE_FAST_BOOT") != ""
	if m.fastBoot {
		if m.cfg.SessionID != "" {
			m.screen = ScreenSession
			m.view.bgPulse = false
		} else {
			m.noAnim = true
		}
	}
	// OPENCODE_ROUTE overrides the initial screen/session (plan 08f H12 /
	// G.14, opencode app.tsx:272-273 parses this as JSON; here it's a plain
	// string): "home" forces the splash, "session" opens the
	// already-configured session id (if any), and any other value is
	// treated as a literal session id to open directly. Takes priority
	// over --session / OPENCODE_FAST_BOOT since it's the more specific ask.
	if route := os.Getenv("OPENCODE_ROUTE"); route != "" {
		switch route {
		case "home":
			m.screen, m.cfg.SessionID = ScreenSplash, ""
			m.view.bgPulse = true
		case "session":
			if m.cfg.SessionID != "" {
				m.screen = ScreenSession
				m.view.bgPulse = false
			}
		default:
			m.cfg.SessionID = route
			m.screen = ScreenSession
			m.view.bgPulse = false
		}
	}
	// Auto-pick light vs dark by terminal background — mirrors opencode's
	// theme_mode_lock behaviour. Restore() will override with any pinned KV theme.
	// cfg.Theme (--theme flag) wins over both auto-pick and KV.
	m.termDark = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	defName := pickDefaultTheme(m.termDark)
	if cfg.Theme != "" {
		defName = cfg.Theme
	}
	def, _ := theme.ByNameForMode(defName, m.termDark)
	m = m.applyTheme(defName, def)
	m.histIdx = -1
	// Build the SDK client only when a URL is known. The first-run flow
	// (plan 08e §D3) defers client construction: New() is called with an
	// empty URL when --url was not passed and no KV pin yet exists, and the
	// connect overlay's connectTo() rebuilds the client once the user picks
	// a server. A nil client is handled throughout (Init and the connect
	// overlay are no-ops without one).
	if cfg.URL != "" {
		c, err := opcode42client.New(cfg.URL, opcode42client.Options{
			Directory: cfg.Directory, Username: cfg.Username, Password: cfg.Password,
		})
		if err != nil {
			m.conn, m.err = ConnError, err
			return m
		}
		m.client = c
	}
	// Ensure the markdown render cache is allocated so all Model copies
	// derived from this root share a non-nil map (maps are reference types).
	m.ensureMDCache()
	m.ensureDiffCache()
	// Plan 20 §1b: initialise the animatingCache from the current state so
	// Init()'s maybeKickAnim() works on the first frame (splash screen →
	// animatingCache is true). Subsequent updates recompute this in the
	// rerender() path.
	m.animatingCache = m.computeAnimating()
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
	// The textarea pads its current/empty line with CursorLine + Base styles; pin
	// their Bg too so the composer row fills with the theme background rather than
	// the terminal default (visible as a dark bar on a light terminal). plan 08c Tier 0.
	// bubbles v2 routes all of these through the Styles()/SetStyles copy.
	bg := lipgloss.NewStyle().Background(p.Bg)
	st := m.input.Styles()
	st.Focused.Text, st.Focused.Placeholder = txt, ph
	st.Blurred.Text, st.Blurred.Placeholder = txt, ph
	st.Focused.CursorLine, st.Focused.Base = bg, bg
	st.Blurred.CursorLine, st.Blurred.Base = bg, bg
	st.Focused.EndOfBuffer, st.Blurred.EndOfBuffer = bg, bg
	m.input.SetStyles(st)
	// bubbles' textarea caches an internal *Style pointer (set only by Focus/Blur)
	// to the active style; after this value-copy of Model that pointer still aims at
	// the pre-copy FocusedStyle, so our edits above wouldn't take effect on render.
	// Re-point it to the copy's style by re-applying the current focus state.
	if m.input.Focused() {
		_ = m.input.Focus()
	} else {
		m.input.Blur()
	}
	// Plan 20: a theme switch re-styles every pre-rendered string. Recompute
	// all of them so View() serves the new palette immediately.
	m = m.rerenderFull()
	return m
}

// Restore loads the persisted theme/model/history from the local KV and turns on
// persistence. Call once from the real entrypoint (not in tests, which want a
// hermetic New). CLI --provider/--model/--theme still win.
// Theme resolution order: cfg.Theme (CLI --theme) > pinned KV theme > auto-pick by terminal background.
//
// First-run flow (plan 08e §D3): if no --url was passed (cfg.URL == "" — the
// caller signals "no --url" by passing an empty URL) AND no KV-pinned
// server_url exists AND --no-discover is not set, the connect overlay opens
// on startup instead of the splash. When KV has server_url, it overrides
// cfg.URL and the TUI connects directly (mirrors Android F1's "skip the
// picker once a server is chosen").
func (m Model) Restore() Model {
	m.persistEnabled = true
	kv := loadKV()
	m.history, m.histIdx = kv.History, -1
	m.stash = kv.Stash
	m.diffTreeHidden = kv.HideDiffTree
	m.terminalTitleEnabled = kvTitleEnabled(kv)
	m.pasteSummaryEnabled = kvPasteSummaryEnabled(kv)
	m.fileContextEnabled = kvFileContextEnabled(kv)
	m.sessionDirFilterEnabled = kvSessionDirFilterEnabled(kv)
	if os.Getenv("OPENCODE_DISABLE_TERMINAL_TITLE") != "" {
		m.terminalTitleEnabled = false
	}
	// --no-anim always wins (New() already set m.noAnim = cfg.NoAnim); only
	// consult the persisted preference when the CLI flag was not passed
	// (plan 08f H7 / G.11: "CLI --no-anim still forces off"). OPENCODE_FAST_BOOT's
	// splash-freeze (plan 08f H12 / G.14) gets the same treatment — it would
	// be pointless if a KV animations-enabled preference undid it here.
	if !m.cfg.NoAnim && !m.fastBoot {
		m.noAnim = !kvAnimationsEnabled(kv)
	}
	// --no-osc52 always wins (New() already applied cfg.NoOSC52); only
	// consult the persisted preference when the CLI flag was not passed
	// (plan 08f H11 / G.13: mirrors --no-anim's "CLI still forces off").
	if !m.cfg.NoOSC52 {
		m.osc52Enabled = kvOsc52WriteEnabled(kv)
	}
	// Theme mode lock (plan 08f H7 / G.12): pins m.termDark across launches,
	// overriding the live terminal-background probe New() just performed.
	// Re-resolve the already-selected theme name (chosen in New() using the
	// pre-override termDark) for the corrected mode before the explicit
	// theme overrides below run with the now-locked termDark.
	if dark, locked := kvThemeModeLocked(kv); locked {
		m.themeModeLocked = true
		if m.termDark != dark {
			m.termDark = dark
			m = m.applyThemeForMode(m.themeName, m.termDark)
		}
	}
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

	// First-run / KV-pinned server resolution (plan 08e §D3).
	// cfg.URL is set by --url; the absence of --url is signalled by the caller
	// passing an empty cfg.URL (cmd/opcode-tui passes "" when --url was not
	// on the command line). A KV-pinned server_url wins over the empty
	// default and skips the overlay. When neither --url nor a KV pin exists,
	// the connect overlay opens on startup regardless of --no-discover —
	// the user still needs a way to enter a URL; --no-discover only
	// suppresses the mDNS browser (the manual URL field still works).
	urlFromFlag := m.cfg.URL
	if urlFromFlag == "" && kv.ServerURL != "" {
		m = m.applyServerURL(kv.ServerURL)
	}
	if urlFromFlag == "" && m.cfg.URL == "" {
		m = m.openConnectModal()
	}
	return m
}

// applyThemeByName switches to a palette by name (no-op if unknown).
// Resolves the palette for the terminal's dark/light mode (m.termDark) so that
// embedded opencode themes use the correct dark or light token variant.
func (m Model) applyThemeByName(name string) Model {
	return m.applyThemeForMode(name, m.termDark)
}

// applyThemeForMode resolves name for the given dark/light mode. Native
// opcode42-dark / opcode42-light are mode-specific names (not dual variants),
// so when the active name is one of those, swap to pickDefaultTheme(dark).
func (m Model) applyThemeForMode(name string, dark bool) Model {
	switch name {
	case "opcode42-dark", "opcode42-light", "":
		name = pickDefaultTheme(dark)
	}
	if p, ok := theme.ByNameForMode(name, dark); ok {
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

// ensureFrameCanvas allocates or resizes m.frameCanvas to match m.width×m.height
// (plan 20 Layer 4). Called from Update on WindowSizeMsg so View's composeCanvas
// can Clear+redraw without NewCanvas.
func (m Model) ensureFrameCanvas() Model {
	if m.width <= 0 || m.height <= 0 {
		return m
	}
	if m.frameCanvas == nil {
		m.frameCanvas = lipgloss.NewCanvas(m.width, m.height)
		return m
	}
	if m.frameCanvas.Width() != m.width || m.frameCanvas.Height() != m.height {
		m.frameCanvas.Resize(m.width, m.height)
	}
	return m
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The composer placeholder follows the mode/screen (opencode
	// footer.prompt.tsx:284-294): shell mode shows a run-a-command hint, the
	// splash/first-prompt shows opencode's "Ask anything..." text, and an open
	// session shows a reply hint (Opcode42 convention).
	m.input.Placeholder = m.composerPlaceholder()
	// Keep the composer sized to the current left column: a screen change
	// (splash→session) or sidebar toggle alters the available width even when no
	// key was pressed. WindowSizeMsg re-runs this after updating m.width.
	m = m.resizeComposer()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m = m.ensureFrameCanvas()
		m = m.resizeComposer()
		if cmd := m.resizePTY(); cmd != nil {
			return m, cmd
		}
		// Plan 20: width/height change re-wraps the body, footer, and
		// sidebar. Recompute all pre-rendered strings.
		m = m.rerenderFull()
		return m, nil

	case tea.PasteMsg:
		// Bracketed paste (DECSET 2004). Large pastes may collapse to a
		// smart-paste chip (plan 08f H3); otherwise forward to the textarea.
		m.histIdx = -1
		m.exiting = false
		m.deleting = false
		return m.maybeSmartPaste(msg.Content)

	case tea.MouseWheelMsg:
		// Plan 18 §A2: mouse wheel scrolls the stream. Ignore when an overlay
		// owns the view (focused PTY, diff reviewer, modal, pending
		// permission/question) — same guard pattern as the key handlers.
		if (m.pty.open && m.pty.focused) || m.diff.open || m.modal != modalNone ||
			m.pendingPermission() != nil || m.pendingQuestion() != nil {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll.Back(m.scrollLines())
		case tea.MouseWheelDown:
			m.scroll.Forward(m.scrollLines())
		}
		return m, nil

	case tea.MouseMotionMsg:
		// Plan 08f H4 (G.3): hovering a modal row previews the selection;
		// hovering an autocomplete row does the same. Deliberately scoped to
		// just these two surfaces — tool-row / user-message hover is deferred.
		// Permission/question overlays outrank a stale modal (same priority
		// as KeyPressMsg / MouseWheelMsg).
		if m.pendingPermission() != nil || m.pendingQuestion() != nil {
			return m, nil
		}
		if m.modal != modalNone {
			if row, ok := m.modalRowAtY(msg.X, msg.Y); ok {
				m.modalSel = row
			}
			return m, nil
		}
		if m.ac.open {
			if row, ok := m.acRowAtY(msg.X, msg.Y); ok {
				m.ac.sel = row
			}
			return m, nil
		}
		return m, nil

	case tea.MouseClickMsg:
		// Plan 08f H4 (G.3): a left-click on a modal/autocomplete row selects
		// it and submits/accepts — mirroring "hover to select, enter to
		// accept" but in one motion.
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if m.pendingPermission() != nil || m.pendingQuestion() != nil {
			return m, nil
		}
		if m.modal != modalNone {
			row, ok := m.modalRowAtY(msg.X, msg.Y)
			if !ok {
				return m, nil
			}
			m.modalSel = row
			if m.modal == modalConnect {
				// Clicking a server row hands focus to the list (mirrors the
				// "tab" toggle) so the selection bar renders immediately.
				m.connectFieldFocus = false
				m.connectURLInput.Blur()
			}
			return m.modalSelect()
		}
		if m.ac.open {
			row, ok := m.acRowAtY(msg.X, msg.Y)
			if !ok {
				return m, nil
			}
			m.ac.sel = row
			return m.acceptAutocomplete()
		}
		return m, nil

	case tea.KeyPressMsg:
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
			// Plan 20: PTY closed → re-render footer (PTY pane removed).
			m = m.rerenderChrome()
			return m, nil
		}
		// Any non-ctrl+c keypress cancels the armed two-press exit guard
		// (opencode footer.ts:684-692 handleInputClear resets exit on input
		// activity). ctrl+c is handled below and advances/quits instead.
		if m.exiting && msg.String() != "ctrl+c" {
			m.exiting = false
		}
		// Same for the ctrl+d delete-session guard (plan 08f H1a).
		if m.deleting && msg.String() != "ctrl+d" {
			m.deleting = false
		}
		// ctrl+c is context-dependent (opencode prompt-input.tsx:806 +
		// app.tsx:963-966, footer.ts:987-1006): with text in the composer it
		// clears the input; with an empty composer it enters a two-press
		// exit guard — the first press shows the EXIT chip + "press ctrl+c
		// again to exit", the second quits, and any other key / a 5s
		// timeout cancels it. A focused PTY keeps the unconditional quit
		// above via handlePTYKey.
		if msg.String() == "ctrl+c" {
			if strings.TrimSpace(m.input.Value()) != "" || len(m.pendingFiles) > 0 || len(m.pasteParts) > 0 {
				m.input.SetValue("")
				m.pendingFiles = nil
				m.pasteParts = nil
				m = m.resizeComposer()
				m, acCmd := m.refreshAutocomplete()
				// Plan 20: composer cleared → re-render footer.
				m = m.rerenderFull()
				return m, acCmd
			}
			if m.exiting {
				if m.stream != nil {
					m.stream.Close()
				}
				if m.cancel != nil {
					m.cancel() // cancel any in-flight health/open cmd + SDK work
				}
				return m, tea.Quit
			}
			m.exiting = true
			// Plan 20: exit guard armed → status bar shows the EXIT chip.
			m = m.rerenderChrome()
			return m, exitTickCmd()
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
		// The which-key overlay (plan 08e §F2) renders the chord options as a
		// Z=15 layer over the status bar instead of mutating m.status — the
		// status line keeps carrying connection/model state, and the overlay
		// is a dedicated, transient affordance. whichKeyView() reads m.leader.
		if m.leader {
			m.leader = false
			return m.handleLeaderKey(msg)
		}
		if msg.String() == "ctrl+x" {
			m.leader = true
			return m, nil
		}
		// F1 opens the help overlay (plan 08e §F3) — matches opencode's
		// keybindings dialog trigger. Static content generated from the
		// keybind table (helpRows); the same overlay is reachable via
		// ctrl+x h and /help.
		if msg.String() == "f1" {
			m.modal, m.modalSel = modalHelp, 0
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
		case "ctrl+r":
			// session_rename (opencode ctrl+r) — plan 08f H1a.
			return m.openRename()
		case "ctrl+v":
			// prompt.paste (opencode ctrl+v) — read system clipboard and
			// insert / attach (plan 08f H2). Overlay owners keep the key.
			if m.modal != modalNone || m.diff.open ||
				(m.pty.open && m.pty.focused) ||
				m.pendingPermission() != nil || m.pendingQuestion() != nil {
				break
			}
			return m, readClipboardCmd()
		case "ctrl+z":
			// terminal.suspend (opencode ctrl+z) — plan 08f H6 / G.8.
			// Bubble Tea puts the terminal in raw mode, so we must emit
			// SuspendMsg ourselves. Disabled on Windows (opencode hides it).
			if runtime.GOOS == "windows" {
				break
			}
			if m.modal != modalNone || m.diff.open ||
				(m.pty.open && m.pty.focused) ||
				m.pendingPermission() != nil || m.pendingQuestion() != nil {
				break
			}
			return m, tea.Suspend
		case "ctrl+d":
			// session_delete (opencode ctrl+d) with two-press confirm — plan 08f
			// H1a. Only when the composer is empty: with text, fall through so
			// the textarea gets forward-delete (ctrl+d).
			if strings.TrimSpace(m.input.Value()) == "" {
				return m.confirmDeleteSession()
			}
		case "ctrl+t":
			m = m.cycleVariant() // cycle model variants (opencode variant_cycle)
			// Plan 20: variant changed → re-render footer (status bar variant
			// chip).
			m = m.rerenderChrome()
			return m, nil
		case "ctrl+up", "pgup":
			// Scroll the stream one step toward older content. These keys
			// never reach the composer, so the input box behaviour (plain
			// ↑/↓ below) is untouched. Plan 17 §A3: kept as Opcode42's
			// line-scroll convention (opencode uses ctrl+alt+y/e — see the
			// known-divergence registry).
			m.scroll.Back(m.scrollLines())
			return m, nil
		case "ctrl+down", "pgdown", "pgdn":
			m.scroll.Forward(m.scrollLines())
			return m, nil
		case "ctrl+alt+u":
			// Half-page up (opencode messages_half_page_up, plan 17 §A3).
			// ±bodyH/4 matches opencode's "half page" semantic.
			m.scroll.Back(m.scrollBodyHeight() / 4)
			return m, nil
		case "ctrl+alt+d":
			// Half-page down (opencode messages_half_page_down).
			m.scroll.Forward(m.scrollBodyHeight() / 4)
			return m, nil
		case "home", "ctrl+g":
			// Home / ctrl+g jump to the first message (opencode
			// messages_first: "ctrl+g,home"). scrollregion.Apply maps Top
			// to a deliberately large offset that Window/Clamp bound to the
			// oldest line at render time.
			m.scroll.Apply(scrollregion.Top, 0, 0)
			return m, nil
		case "end", "ctrl+alt+g":
			// End / ctrl+alt+g jump to the tail (opencode messages_last:
			// "ctrl+alt+g,end").
			m.scroll.Apply(scrollregion.Bottom, 0, 0)
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
				m.input.Placeholder = m.composerPlaceholder()
				// Plan 20: shell mode changes the composer accent + placeholder.
				m = m.rerenderChrome()
				return m, nil
			}
		case "esc":
			if m.shellMode {
				m.shellMode = false
				m.input.Placeholder = m.composerPlaceholder()
				// Plan 20: shell mode exited → re-render footer.
				m = m.rerenderChrome()
				return m, nil
			}
		case "backspace":
			// Shell mode exits on backspace at cursor offset 0 (opencode
			// footer.prompt.tsx:1084-1091); a backspace mid-text falls through
			// to the textarea so the user can still edit.
			if m.shellMode && m.input.Line() == 0 && m.input.LineInfo().CharOffset == 0 {
				m.shellMode = false
				m.input.Placeholder = m.composerPlaceholder()
				// Plan 20: shell mode exited → re-render footer.
				m = m.rerenderChrome()
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
		// Plan 20: composer text changed → re-render footer.
		m = m.rerenderFull()
		return m, tea.Batch(cmd, acCmd)

	case sessionOpenedMsg:
		if msg.err != nil {
			m.status = "create session failed: " + msg.err.Error()
			// Plan 20: status changed → re-render footer (status bar).
			m = m.rerenderChrome()
			return m, nil
		}
		if msg.session.ID == "" { // daemon returned 200 + {} or similar
			m.status, m.modal = "create session: empty response", modalNone
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		m.store.sessions = upsertSession(m.store.sessions, msg.session)
		m.store.version++
		m.cfg.SessionID, m.screen, m.modal = msg.session.ID, ScreenSession, modalNone
		// Reset animation frame so the sweep starts from the left in the new session.
		m.animFrame = 0
		// Entering the session screen — the bg-pulse is splash-only (plan 08e §B2).
		m.view.bgPulse = false
		// Plan 20: session switch → re-render body + footer + sidebar.
		m = m.rerenderFull()
		return m, nil

	case sessionDeletedMsg:
		if msg.err != nil {
			m.status = "delete failed: " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		for _, dm := range m.store.messages[msg.id] { // drop the session's parts too
			delete(m.store.parts, dm.ID)
		}
		m.store.sessions = removeSession(m.store.sessions, msg.id)
		delete(m.store.messages, msg.id)
		m.store.version++
		if m.modalSel > 0 && m.modalSel >= m.modalCount() {
			m.modalSel = m.modalCount() - 1
		}
		if m.cfg.SessionID == msg.id { // the open session was deleted
			if ss := m.orderedSessions(); len(ss) > 0 {
				m.cfg.SessionID = ss[0].ID
				// Plan 20: session switched → re-render body + footer + sidebar.
				m = m.rerenderFull()
				return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)
			}
			m.cfg.SessionID, m.screen = "", ScreenSplash
			// Re-entering the splash — kick the logo shimmer tick (plan 08c M10)
			// and re-enable the bg-pulse (plan 08e §B2).
			m.view.bgPulse = true
			// Plan 20: screen changed → re-render chrome (no body on splash).
			m = m.rerenderChrome()
			return m, m.maybeKickAnim()
		}
		// Plan 20: store changed → re-render all.
		m = m.rerenderFull()
		return m, nil

	case connectedMsg:
		m.conn, m.status, m.attempt = Connected, "connected", 0
		// Plan 20: status changed → re-render footer (status bar).
		m = m.rerenderChrome()
		// Subscribe to events, bootstrap the session list, resolve the model, and
		// preload the provider + command catalogs so the switcher/slash popup open
		// populated. Also bootstrap the LSP/MCP status chrome (plan 08f G.5/G.6)
		// so the sidebar/footer counts are populated without opening either modal.
		return m, tea.Batch(openSSECmd(m.ctx, m.client), loadSessionsCmd(m.ctx, m.client), loadConfigCmd(m.ctx, m.client), loadProvidersCmd(m.ctx, m.client), loadCommandsCmd(m.ctx, m.client), loadAgentsCmd(m.ctx, m.client), loadLSPCmd(m.ctx, m.client), loadMCPCmd(m.ctx, m.client), loadMCPResourcesCmd(m.ctx, m.client))

	case configLoadedMsg:
		if !m.model.ok() {
			m.model = promptModel{Provider: msg.provider, Model: msg.model}
			// Plan 20: model changed → re-render footer (status bar shows the
			// model) + sidebar (context limit reads m.model).
			m = m.rerenderFull()
		}
		return m, nil

	case providersLoadedMsg:
		if msg.err != nil {
			if m.modal == modalModels {
				m.status = "providers: " + msg.err.Error()
				// Plan 20: status changed → re-render footer.
				m = m.rerenderChrome()
			}
			return m, nil
		}
		m.choices = msg.choices
		if m.modal == modalModels { // re-highlight the active model now the list is in
			m.modalSel = m.modelSelIndex()
		}
		// Plan 20: provider catalog changed → re-render sidebar (context limit
		// reads m.choices) + footer (status bar model label).
		m = m.rerenderFull()
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
				// Plan 20: status changed → re-render footer.
				m = m.rerenderChrome()
			}
			return m, nil
		}
		m.agents = msg.items
		agentDropped := false
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
				agentDropped = true
			}
		}
		if m.modal == modalAgents { // re-highlight the active agent now the list is in
			m.modalSel = m.agentSelIndex()
		}
		// Plan 20: if the active agent was dropped, the status bar mode chip
		// changed — re-render the footer.
		if agentDropped {
			m = m.rerenderChrome()
		}
		return m, nil

	case sessionCreatedMsg:
		if msg.err != nil {
			m.status = "create session failed: " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		m.store.sessions = upsertSession(m.store.sessions, msg.session)
		m.store.version++
		m.cfg.SessionID = msg.session.ID
		m.screen = ScreenSession
		m.view.bgPulse = false // session screen — no bg-pulse (plan 08e §B2)
		// Plan 20: session switch → re-render body + footer + sidebar.
		m = m.rerenderFull()
		if msg.command != "" { // a "/command" created this session — run it
			return m, runCommandCmd(m.ctx, m.client, msg.session.ID, msg.command, msg.arguments)
		}
		return m, promptCmd(m.ctx, m.client, msg.session.ID, msg.text, m.model, m.agent, msg.files)

	case promptSentMsg:
		if msg.err != nil {
			m.status = "prompt failed: " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
		}
		return m, nil

	case revertedMsg:
		if msg.err != nil {
			m.status = "revert failed: " + msg.err.Error()
			m = m.rerenderChrome()
			return m, nil
		}
		if msg.redo {
			m.revertMessageID = ""
			m.status = "redone"
		} else {
			m.revertMessageID = msg.messageID
			m.status = "reverted"
		}
		m = m.rerenderChrome()
		if m.cfg.SessionID != "" {
			return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)
		}
		return m, nil

	case renamedMsg:
		if msg.err != nil {
			m.status = "rename failed: " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		if msg.session.ID != "" {
			m.store.sessions = upsertSession(m.store.sessions, msg.session)
			m.store.version++
		}
		m.status = "renamed"
		// Plan 20: store + status changed → re-render all (sidebar shows title).
		m = m.rerenderFull()
		return m, nil

	case sharedMsg:
		if msg.err != nil {
			m.status = "share failed: " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		if msg.session.ID != "" {
			m.store.sessions = upsertSession(m.store.sessions, msg.session)
			m.store.version++
		}
		if msg.shared {
			if sh := msg.session.Share; sh != nil && sh.URL != "" {
				m.status = "shared · " + sh.URL + " (copied)"
				// Plan 20: store + status changed → re-render all.
				m = m.rerenderFull()
				return m, copyClipboardCmd(sh.URL, m.osc52Enabled)
			}
			m.status = "shared"
		} else {
			m.status = "unshared"
		}
		// Plan 20: store + status changed → re-render all.
		m = m.rerenderFull()
		return m, nil

	case summarizedMsg:
		if msg.err != nil {
			m.status = "summarize failed: " + msg.err.Error()
		} else {
			m.status = "summarizing context…"
		}
		// Plan 20: status changed → re-render footer.
		m = m.rerenderChrome()
		return m, nil

	case abortedMsg:
		// Show a toast for interrupt outcomes (plan 08c M11 source #2).
		// The status line continues to carry the text for accessibility; the toast
		// is an additional transient notice in the bottom-right corner.
		if msg.err != nil {
			m.status = "interrupt failed: " + msg.err.Error()
			cmd := m.pushToast(toastError, "interrupt failed")
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, cmd
		}
		m.status = "interrupted"
		cmd := m.pushToast(toastInfo, "interrupted")
		// Plan 20: status changed → re-render footer.
		m = m.rerenderChrome()
		return m, cmd

	case forkedMsg:
		if msg.err != nil {
			m.status = "fork failed: " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		if msg.session.ID == "" {
			m.status = "fork: empty response"
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		m.store.sessions = upsertSession(m.store.sessions, msg.session)
		m.store.version++
		m.cfg.SessionID, m.screen = msg.session.ID, ScreenSession
		m.view.bgPulse = false // session screen — no bg-pulse (plan 08e §B2)
		m.status = "forked"
		if msg.prompt != "" {
			m.input.SetValue(msg.prompt)
			m.input.CursorEnd()
			m = m.resizeComposer()
		}
		// Plan 20: session switch → re-render body + footer + sidebar.
		m = m.rerenderFull()
		return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)

	case mcpLoadedMsg:
		if msg.err != nil {
			if m.modal == modalMCP {
				m.status = "mcp: " + msg.err.Error()
				// Plan 20: status changed → re-render footer.
				m = m.rerenderChrome()
			}
			return m, nil
		}
		m.mcpServers = msg.items
		// Plan 20: MCP count changed → re-render sidebar + footer (MCP counts).
		m = m.rerenderFull()
		return m, nil

	case mcpResourcesLoadedMsg:
		// Soft-fail: resources are optional autocomplete extras; leave the
		// prior list alone on error so a flaky experimental endpoint doesn't
		// wipe useful mentions mid-session.
		if msg.err == nil {
			m.mcpResources = msg.items
		}
		return m, nil

	case lspLoadedMsg:
		if msg.err != nil {
			// Best-effort: the LSP section simply shows "0 LSP" until the next
			// successful fetch (bootstrap retries via the next lsp.updated event
			// or reconnect); no modal surfaces this status today.
			return m, nil
		}
		m.lspServers = msg.items
		// Plan 20: LSP count changed → re-render sidebar + footer (LSP counts).
		m = m.rerenderFull()
		return m, nil

	case skillsLoadedMsg:
		if msg.err != nil {
			if m.modal == modalSkills {
				m.status = "skills: " + msg.err.Error()
				// Plan 20: status changed → re-render footer.
				m = m.rerenderChrome()
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
		// Plan 20: a toast was pushed → re-render chrome (toast layer).
		m = m.rerenderChrome()
		return m, cmd

	case openURLDoneMsg:
		// docs.open / /docs (plan 08f H8). Status reflects success or failure.
		if msg.Err != nil {
			m.status = "open docs: " + msg.Err.Error()
		} else {
			m.status = "opened " + msg.URL
		}
		m = m.rerenderChrome()
		return m, nil

	case clipboardReadMsg:
		// ctrl+v / prompt.paste result (plan 08f H2).
		return m.applyClipboardRead(msg)

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
		// Plan 20: composer text or status changed → re-render footer.
		m = m.rerenderChrome()
		return m, nil

	case shellSentMsg:
		if msg.err != nil {
			m.status = "shell failed: " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
		}
		return m, nil

	case permissionRepliedMsg:
		m.permState = permSetReplying(m.permState, false)
		if msg.err != nil {
			// E3 (plan 16 Bug 1): a 404 means the permission was already answered
			// or cancelled elsewhere (the optimistic clear already removed it
			// from the UI). Swallow it silently — don't surface a misleading
			// "reply failed" status. Non-404 errors still keep the request so
			// the user can retry.
			if isHTTPNotFound(msg.err) {
				m.permState = newPermissionState()
				m.permRequestID = ""
				m.store.permissions = removeByID(m.store.permissions, msg.id, func(q Permission) string { return q.ID })
				m.store.version++
				// Plan 20: store changed → re-render all.
				m = m.rerenderFull()
				return m, nil
			}
			// Keep the request so the user can retry — the daemon is still blocked.
			m.status = "permission reply failed (try again): " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		m.permState = newPermissionState()
		m.permRequestID = ""
		m.store.permissions = removeByID(m.store.permissions, msg.id, func(q Permission) string { return q.ID })
		m.store.version++
		// Plan 20: store changed → re-render all.
		m = m.rerenderFull()
		return m, nil

	case questionRepliedMsg:
		m.qBody = questionSetReplying(m.qBody, false, m.qBody.rejecting)
		if msg.err != nil {
			// E3 (plan 16 Bug 1): a 404 means the question was already answered
			// or cancelled elsewhere (the optimistic clear already removed it
			// from the UI). Swallow it silently — don't surface a misleading
			// "reply failed" status. Non-404 errors still keep the request so
			// the user can retry.
			if isHTTPNotFound(msg.err) {
				// Plan 08e §E4: apply any deferred SSE event (the daemon already
				// processed the reply; the 404 confirms it) before clearing.
				if m.qDeferredSSE.Type != "" {
					m.store = m.store.Reduce(m.qDeferredSSE)
					m.qDeferredSSE = opcode42client.SSEEvent{}
				}
				m.store.questions = removeByID(m.store.questions, msg.id, func(x Question) string { return x.ID })
				m.store.version++
				m = m.resetQuestion()
				// Plan 20: store changed → re-render all.
				m = m.rerenderFull()
				return m, nil
			}
			// Plan 08e §E4: a non-404 error means the daemon rejected the
			// reply (e.g. transient 500). The deferred SSE event (if any) is
			// for a prior successful processing — clear it so a retry doesn't
			// apply a stale event. The user retries against the live daemon
			// state.
			m.qDeferredSSE = opcode42client.SSEEvent{}
			m.status = "question reply failed (try again): " + msg.err.Error()
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		// Plan 08e §E4: record the finalized question for the in-stream
		// answered card BEFORE clearing the pending slice + per-request state.
		// The local reply path knows the specific selected labels (from
		// qBody.answers), so the card shows the labels rather than a bare
		// "Answered" (the SSE path's fallback). Deduped + upgraded by id
		// against the SSE path inside recordAnsweredQuestion.
		m = m.recordLocalAnsweredQuestion(msg.id)
		// Apply any deferred SSE question.replied/rejected event for this
		// question (plan 08e §E4): if the SSE event arrived while
		// qBody.replying was true, it was deferred so this handler could
		// record the labels first. The SSE path's recordAnsweredQuestion is a
		// no-op (deduped by id) or an upgrade (the local labels win), then it
		// clears the pending question.
		if m.qDeferredSSE.Type != "" {
			m.store = m.store.Reduce(m.qDeferredSSE)
			m.qDeferredSSE = opcode42client.SSEEvent{}
		}
		m.store.questions = removeByID(m.store.questions, msg.id, func(x Question) string { return x.ID })
		m.store.version++
		m = m.resetQuestion()
		// Plan 20: store changed → re-render all.
		m = m.rerenderFull()
		return m, nil

	case permissionsReconciledMsg:
		// E3: REPLACE the store's pending permissions with the freshly-fetched
		// list (matches Android StoreReducer.kt:115). On error leave the store
		// unchanged — a flaky GET must not wipe the UI.
		if msg.err == nil {
			m.store.permissions = msg.permissions
			m.store.version++
			// Plan 20: store changed → re-render all.
			m = m.rerenderFull()
		}
		return m, nil

	case questionsReconciledMsg:
		// E3: REPLACE the store's pending questions with the freshly-fetched
		// list (matches Android StoreReducer.kt:116). On error leave the store
		// unchanged. If the active question disappeared from the store, reset
		// the per-request answer state so the overlay closes cleanly.
		if msg.err == nil {
			prevQ := questionID(m.pendingQuestion())
			m.store.questions = msg.questions
			m.store.version++
			if questionID(m.pendingQuestion()) != prevQ {
				m = m.resetQuestion()
			}
			// Plan 20: store changed → re-render all.
			m = m.rerenderFull()
		}
		return m, nil

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
		m.store.version++
		// Open the requested session, else the newest.
		if m.cfg.SessionID == "" && len(msg.sessions) > 0 {
			m.cfg.SessionID = msg.sessions[0].ID
		}
		if m.cfg.SessionID != "" {
			m.screen = ScreenSession
			m.view.bgPulse = false // session screen — no bg-pulse (plan 08e §B2)
			// Plan 20: session switch + store changed → re-render all (the
			// subsequent messagesLoadedMsg will trigger another re-render once
			// the stream is loaded).
			m = m.rerenderFull()
			return m, loadMessagesCmd(m.ctx, m.client, m.cfg.SessionID)
		}
		// Plan 20: store changed → re-render all.
		m = m.rerenderFull()
		return m, nil

	case messagesLoadedMsg:
		if msg.err == nil {
			// Replace (not upsert) so a post-revert reload drops turns the
			// daemon no longer returns (08f H1b review).
			m.store = m.store.replaceHistory(msg.sessionID, msg.items)
			m.store.version++
		}
		m.todos = nil // todos are per-session; refetch for the opened one if the dock is up
		cmds := []tea.Cmd{}
		if msg.sessionID != "" { // keep the sub-agent footer fresh (GET /session/{id}/children)
			cmds = append(cmds, loadChildrenCmd(m.ctx, m.client, msg.sessionID))
		}
		if m.tasksOpen && m.cfg.SessionID != "" {
			cmds = append(cmds, loadTodosCmd(m.ctx, m.client, m.cfg.SessionID))
		}
		// Plan 20: history ingest → re-render body + footer + sidebar
		// (recompute childStatuses covers the new messages).
		m = m.rerenderFull()
		return m, tea.Batch(cmds...)

	case connErrMsg:
		m.conn, m.err = ConnError, msg.err
		// Plan 20: status changed → re-render footer.
		m = m.rerenderChrome()
		return m, nil

	case streamOpenedMsg:
		if msg.err != nil {
			m.conn = Reconnecting
			m.status = "reconnecting…"
			cmd := backoffCmd(m.attempt)
			m.attempt++
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, cmd
		}
		if m.stream != nil {
			m.stream.Close() // close any prior stream before replacing it
		}
		m.stream = msg.stream
		m.conn = Connected
		m.attempt = 0 // a successful reopen resets the backoff
		// Plan 20: status changed → re-render footer.
		m = m.rerenderChrome()
		// E3: re-fetch the pending permission/question lists on reconnect — the
		// daemon may have cancelled one without an SSE event (agent finalizer),
		// leaving a stale entry in the store. REPLACE the store's slices with the
		// daemon's current view (matches Android reconcilePending on reconnect).
		return m, tea.Batch(listenCmd(m.stream), reconcilePendingCmd(m.ctx, m.client))

	case sseEventMsg:
		// Plan 17 §D2 — deferred token write is event-driven, NOT a 100ms
		// tick: SSE deltas accumulate in store.Part.Text between View() calls,
		// and Bubble Tea re-renders on each sseEventMsg (event-driven). The
		// deferral is the event-loop batching between renders, NOT the 100ms
		// animTick — the animTick (spinner.go) drives only animation (spinner,
		// logo, toasts), not token flush. No explicit coalescing queue is
		// needed because Bubble Tea's event-driven re-render already batches
		// whatever arrived. opencode's coalescing drain is `queueMicrotask`
		// (run/footer.ts:560) — sub-millisecond; the effective streaming
		// cadence is "as fast as the event loop + surface settle allow."
		m.eventCount++
		prevQ := questionID(m.pendingQuestion())
		// Plan 08e §E4: when a local question reply is in flight
		// (qBody.replying), defer applying the SSE
		// question.replied/rejected event for OUR own pending question. The
		// SSE event for our own reply may arrive before the HTTP response;
		// applying it now would clear the question from the store and make
		// pendingQuestion() returns nil before
		// questionRepliedMsg can record the locally-selected labels in the
		// answered-questions store. The questionRepliedMsg handler applies the
		// deferred event after recording the labels, so the store clears
		// cleanly. Other SSE events (and question.replied/rejected for a
		// different request id) are applied normally.
		if m.qBody.replying && (msg.ev.Type == "question.replied" || msg.ev.Type == "question.rejected") {
			// Match the in-flight request via qBody.requestID — not
			// pendingQuestion(), which is scope-filtered (08f H18) and can
			// return nil after navigating to a child/splash while the reply
			// HTTP round-trip is still open.
			var p struct {
				RequestID string `json:"requestID"`
			}
			if m.qBody.requestID != "" && decode(msg.ev.Properties, &p) && p.RequestID == m.qBody.requestID {
				m.qDeferredSSE = msg.ev
				msg.ev = opcode42client.SSEEvent{} // neutralize; not applied below
			}
		}
		if msg.ev.Type != "" {
			// Plan 18 §A3-simple: capture tail state BEFORE the body grows,
			// then re-pin to the tail if we were there. When scrolled up
			// (Offset>0) the offset is left untouched — the simple tail-sticky
			// model does NOT content-anchor (deferred per the plan).
			wasAtTail := m.scroll.AtTail()
			m.store = m.store.Reduce(msg.ev)
			if wasAtTail {
				m.scroll.ToTail()
			}
		}
		if questionID(m.pendingQuestion()) != prevQ { // active question cleared/replaced
			if !m.qBody.replying {
				m = m.resetQuestion()
			}
		}
		m.status = fmt.Sprintf("connected · %d events · %d sessions", m.eventCount, len(m.store.sessions))
		// Plan 20: store changed → re-render body + footer + sidebar
		// (recompute childStatuses covers the new/updated task parts).
		m = m.rerenderFull()
		cmds := []tea.Cmd{listenCmd(m.stream)}
		// Ring the bell when the agent blocks on input — the terminal may be unfocused.
		if msg.ev.Type == "permission.asked" || msg.ev.Type == "question.asked" {
			cmds = append(cmds, bellCmd())
		}
		// A todowrite tool part changed the todos — refetch (no todo SSE event).
		if m.tasksOpen && m.cfg.SessionID != "" && isTodoWriteEvent(msg.ev) {
			cmds = append(cmds, loadTodosCmd(m.ctx, m.client, m.cfg.SessionID))
		}
		// G.5/G.6: a client spawned/handshook — refetch LSP status so the
		// sidebar/footer counts stay current (lsp/lsp.ts:294 fires lsp.updated
		// after each first-successful-client spawn; the event carries no
		// payload, so re-fetch rather than reduce it into the store).
		if msg.ev.Type == "lsp.updated" {
			cmds = append(cmds, loadLSPCmd(m.ctx, m.client))
		}
		// E3: when the open session goes idle, reconcile pending permissions/
		// questions. A cancelled-without-event request (agent finalizer) would
		// otherwise linger in the store; the idle transition is the natural
		// moment to re-sync (matches Android's session.status → idle watcher).
		if msg.ev.Type == "session.status" && m.cfg.SessionID != "" && isSessionIdleFor(msg.ev, m.cfg.SessionID) {
			cmds = append(cmds, reconcilePendingCmd(m.ctx, m.client))
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
			// Plan 20: todos changed → re-render footer (tasks dock).
			m = m.rerenderChrome()
		}
		return m, nil

	case childrenLoadedMsg:
		if msg.err == nil {
			for _, ss := range msg.children {
				m.store.sessions = upsertSession(m.store.sessions, ss)
			}
			m.store.version++
			// Plan 20: store changed → re-render all (recompute childStatuses
			// covers the new children).
			m = m.rerenderFull()
		}
		return m, nil

	case childMessagesLoadedMsg:
		// Plan 08e §C1: ingest the child session's messages into the store so
		// taskTranscript can render them inline under the expanded task card.
		// Reuses the ingestHistory path (same wire shape, same reducer); the
		// child id is the store key. On error, leave the store unchanged — a
		// flaky GET must not wipe the transcript; the user can re-toggle the
		// card to retry.
		if msg.err == nil {
			m.store = m.store.ingestHistory(msg.childID, msg.items)
			m.store.version++
			// Plan 20: store changed → re-render body (task card transcript)
			// + footer + sidebar.
			m = m.rerenderFull()
		}
		return m, nil

	case diffLoadedMsg:
		if !m.diff.open { // reviewer was closed while the fetch was in flight
			return m, nil
		}
		if msg.gen != m.diff.gen { // a fetch from a prior source (before a ctrl+x s toggle) — discard
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
			// Plan 20: PTY state changed → re-render footer (PTY pane shows
			// the error).
			m = m.rerenderChrome()
			return m, nil
		}
		m.pty.id, m.pty.conn = msg.id, msg.conn
		// Plan 20: PTY connected → re-render footer (PTY pane now visible).
		m = m.rerenderChrome()
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
		// Plan 20: PTY output arrived → re-render footer (PTY pane content).
		// The PTY terminal is drawn from m.pty.term's screen; the footer layer
		// must be rebuilt so the new cells show.
		m = m.rerenderChrome()
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
		// Plan 20: PTY closed → re-render footer (PTY pane removed/errored).
		m = m.rerenderChrome()
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
		// Plan 20: status changed → re-render footer.
		m = m.rerenderChrome()
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
		// Plan 20: recompute animatingCache BEFORE checking it. The tick fires
		// asynchronously; a tool may have completed (via sseEventMsg) since the
		// last Update, so the cached value may be stale. This is the one place
		// the cache is recomputed in the check path rather than after a
		// mutation — the tick is the only async event that needs a fresh
		// animating() read without a prior mutation.
		m.animatingCache = m.computeAnimating()
		if m.animating() {
			m.animFrame++
			// Plan 20: the animTick advances the spinner glyph in the footer
			// (status bar) and the sidebar (task list). The body content is
			// unchanged between ticks, so rerenderChrome() (footer + sidebar +
			// childStatus + animating) is enough — the body rebuild is
			// skipped. If a reasoning header spinner is in the body, a
			// subsequent sseEventMsg (the next delta) will trigger a full
			// rerender.
			m = m.rerenderChrome()
			return m, animTickCmd()
		}
		// Not animating — stop; the next animating state will re-kick via maybeKickAnim.
		// Plan 20: still re-render chrome once so the final frame reflects the
		// stopped state (e.g. the spinner is gone).
		m = m.rerenderChrome()
		return m, nil

	case exitTickMsg:
		// The two-press exit/delete guards timed out (opencode footer.ts:954-961):
		// cancel the armed exit so the status bar drops the EXIT chip; same for
		// the ctrl+d delete-session guard (08f H1a).
		m.exiting = false
		m.deleting = false
		// Plan 20: exit guard cleared → re-render footer (EXIT chip gone).
		m = m.rerenderChrome()
		return m, nil

	// discoverStartedMsg (plan 08e §D2): the mDNS browser has opened. Stash
	// the channel on m.discoverOut so discoverNextCmd can pump it; if a first
	// service was already resolved, append it and kick its reachability probe.
	// Then re-issue discoverNextCmd to read the next service.
	case discoverStartedMsg:
		if m.modal != modalConnect {
			return m, nil // browser completed after the overlay closed — drop
		}
		m.discoverOut = msg.out
		var cmds []tea.Cmd
		if msg.first != nil {
			m.discoveredServers = append(m.discoveredServers, *msg.first)
			cmds = append(cmds, probeServerCmd(connectProbeKey(*msg.first), "http://"+msg.first.Host+":"+strconv.Itoa(msg.first.Port)))
		}
		cmds = append(cmds, discoverNextCmd(m.discoverCtx, m.discoverOut))
		return m, tea.Batch(cmds...)

	// discoveredServerMsg: one more daemon surfaced. Append it (dedupe by
	// host:port — the browser already dedupes but a late duplicate across
	// service types is harmless to guard against), kick its probe, and pump
	// the next read.
	case discoveredServerMsg:
		if m.modal != modalConnect {
			return m, nil
		}
		// Dedupe by host:port (defensive — mdns.Browse already dedupes).
		for _, ex := range m.discoveredServers {
			if ex.Host == msg.service.Host && ex.Port == msg.service.Port {
				return m, discoverNextCmd(m.discoverCtx, m.discoverOut)
			}
		}
		m.discoveredServers = append(m.discoveredServers, msg.service)
		probeURL := "http://" + msg.service.Host + ":" + strconv.Itoa(msg.service.Port)
		return m, tea.Batch(
			probeServerCmd(connectProbeKey(msg.service), probeURL),
			discoverNextCmd(m.discoverCtx, m.discoverOut),
		)

	// serverProbeMsg: a reachability probe returned. Update the cache so the
	// row's dot color flips from amber to green on the next render.
	case serverProbeMsg:
		if m.serverProbe == nil {
			m.serverProbe = map[string]serverProbeState{}
		}
		m.serverProbe[msg.key] = serverProbeState{reachable: msg.reachable}
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
	// The connect overlay (plan 08e §D2) has two focus targets: the manual
	// URL text field and the nearby-servers list. Tab toggles focus. When the
	// URL field is focused, typing edits it and enter connects to the typed
	// URL; when the list is focused, up/down move and enter connects to the
	// selected server. esc cancels the browser and closes the overlay.
	if m.modal == modalConnect {
		switch msg.String() {
		case "esc":
			return m.closeConnectModal()
		case "tab":
			m.connectFieldFocus = !m.connectFieldFocus
			if m.connectFieldFocus {
				m.connectURLInput.Focus()
				m.connectURLInput.CursorEnd()
			} else {
				m.connectURLInput.Blur()
			}
			return m, nil
		case "enter":
			if m.connectFieldFocus {
				return m.connectTo(m.connectURLInput.Value())
			}
			return m.modalSelect()
		}
		if m.connectFieldFocus {
			var cmd tea.Cmd
			m.connectURLInput, cmd = m.connectURLInput.Update(msg)
			return m, cmd
		}
		// List-focused: up/down move, other keys fall through to no-op.
		switch msg.String() {
		case "up", "k", "ctrl+p":
			if m.modalSel > 0 {
				m.modalSel--
			}
			return m, nil
		case "down", "j", "ctrl+n":
			if m.modalSel < len(m.discoveredServers)-1 {
				m.modalSel++
			}
			return m, nil
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.modal, m.modalSel, m.messageActionID = modalNone, 0, ""
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
	case "t":
		// Toggle subtree mode in the sessions modal (plan 08e §C4).
		// Children render indented under their parent instead of flat.
		if m.modal == modalSessions {
			m.view.sessionsSubtree = !m.view.sessionsSubtree
			if m.modalSel > 0 && m.modalSel >= m.modalCount() {
				m.modalSel = m.modalCount() - 1
			}
			if m.view.sessionsSubtree {
				m.status = "sessions: subtree"
			} else {
				m.status = "sessions: flat"
			}
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
		}
		return m, nil
	case "ctrl+d":
		if m.modal == modalSessions {
			if id := m.sessionIDAtModalSel(); id != "" {
				return m, deleteSessionCmd(m.ctx, m.client, id)
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
					// Plan 20: status changed → re-render footer.
					m = m.rerenderChrome()
					return m, copyClipboardCmd(txt, m.osc52Enabled)
				}
			}
		}
		return m, nil
	}
	return m, nil
}

// maxComposerRows caps the auto-growing composer; beyond it the textarea
// scrolls. Matches opencode's TEXTAREA_MAX_ROWS=6 (footer.prompt.tsx:35-37);
// the full TUI's max(6, floor(height/3)) floor (prompt/index.tsx:1340) is also
// 6, so 6 is the common cap for any terminal tall enough to show the bar.
const maxComposerRows = 6

// composerPlaceholder returns the textarea placeholder for the current mode.
// opencode (footer.prompt.tsx:284-294): shell mode → `Run a command... "git
// status"`; a first prompt (no messages yet) → `Ask anything... "Fix a TODO in
// the codebase"`; otherwise empty. Opcode42 keeps a reply hint on the session
// screen (its own convention) in place of opencode's empty string.
func (m Model) composerPlaceholder() string {
	if m.shellMode {
		return `Run a command... "git status"`
	}
	if m.screen != ScreenSession {
		return `Ask anything... "Fix a TODO in the codebase"`
	}
	return "Reply, or / for commands"
}

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
	cap := maxComposerRows
	if m.composerMaxRows > 0 {
		cap = m.composerMaxRows
	}
	if h > cap {
		h = cap
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
	case "h":
		// ctrl+x h opens the help overlay (plan 08e §F3) — same target as F1
		// and /help. The leader is cleared by the caller (handleLeaderKey is
		// reached only after m.leader is reset to false).
		m.modal, m.modalSel = modalHelp, 0
		return m, nil
	case "c":
		// session_compact / summarize (opencode <leader>c) — plan 08f H1a.
		// Connect moved to ctrl+x k so this chord matches opencode.
		return m.compactSession()
	case "k":
		// Open the connect overlay (plan 08e §D2): mDNS browser + manual URL.
		// Was ctrl+x c; moved so <leader>c can be compact (08f H1a).
		m = m.openConnectModal()
		if m.discoverCtx != nil {
			return m, startDiscoverCmd(m.discoverCtx)
		}
		return m, nil
	case "p":
		m.modal, m.modalSel = modalPalette, 0
		return m, nil
	case "b":
		m.sidebarHidden = !m.sidebarHidden
		m = m.resizeComposer() // width changed → re-fit the composer
		// Plan 20: sidebar visibility toggled → re-render all (width changed).
		m = m.rerenderFull()
		return m, nil
	case "t":
		m.tasksOpen = !m.tasksOpen
		if m.tasksOpen {
			// Plan 20: tasks dock opened → re-render footer (dock shows).
			m = m.rerenderChrome()
			return m, loadTodosCmd(m.ctx, m.client, m.cfg.SessionID)
		}
		// Plan 20: tasks dock closed → re-render footer (dock hidden).
		m = m.rerenderChrome()
		return m, nil
	case "u":
		// messages_undo (opencode <leader>u) — plan 08f H1b.
		return m.undoLastTurn()
	case "U":
		// messages_redo (opencode <leader>r) — keep ctrl+x r for thinking;
		// shift+u is redo so undo/redo share a chord family (08f H1b).
		return m.redoTurn()
	case "L":
		// messages_last_user — jump scroll toward the last user turn.
		m = m.jumpLastUser()
		return m, nil
	case "y":
		if txt := m.lastAssistantText(); txt != "" {
			m.status = "copied last response"
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, copyClipboardCmd(txt, m.osc52Enabled)
		}
		m.status = "nothing to copy"
		// Plan 20: status changed → re-render footer.
		m = m.rerenderChrome()
		return m, nil
	case "r":
		// ctrl+x r toggles the ThinkingMode collapse/expand (plan 17 §D1).
		// messages_redo uses ctrl+x U (shift+u) so this chord stays thinking.
		m.view.hideThinking = !m.view.hideThinking
		if m.view.hideThinking {
			m.status = "thinking: hide"
		} else {
			m.status = "thinking: show"
		}
		// Plan 20: view toggle affects body (reasoning rendering) + footer
		// (status hint).
		m = m.rerenderFull()
		return m, nil
	case "f":
		// ctrl+x f toggles the per-reasoning expanded signal (plan 17 §D1):
		// in hide mode the body is collapsed to the 1-line header; flipping
		// expandedThinking opens the body under the hide-mode header. In
		// show mode the body always renders and this toggle is a no-op
		// (opencode's ReasoningHeader ignores `open` when toggleable is
		// false — index.tsx:1594-1597, 1657-1659).
		m.view.expandedThinking = !m.view.expandedThinking
		if m.view.expandedThinking {
			m.status = "thought: expanded"
		} else {
			m.status = "thought: collapsed"
		}
		// Plan 20: view toggle affects body (reasoning rendering) + footer.
		m = m.rerenderFull()
		return m, nil
	case "o":
		m.view.hideTools = !m.view.hideTools
		m.status = toggleHint("tool output", !m.view.hideTools)
		// Plan 20: view toggle affects body (tool rows) + footer (status).
		m = m.rerenderFull()
		return m, nil
	case "v":
		// Toggle collapse on the last tool part (plan 08c M7 per-tool collapse).
		// ctrl+x v collapses/expands the most recent tool's output panel.
		// For a `task` tool part, expanding also fires loadChildMessagesCmd so
		// the inline transcript populates on first expand (plan 08e §C1).
		if id := m.lastToolPartID(); id != "" {
			m.view = m.view.toggleToolCollapse(id)
			if m.view.isToolCollapsed(id) {
				m.status = "tool output: collapsed"
			} else {
				m.status = "tool output: expanded"
			}
			// Plan 20: tool collapse toggle affects body (tool output panel)
			// + footer (status).
			m = m.rerenderFull()
			if !m.view.isToolCollapsed(id) {
				if cmd := m.maybeLoadTaskChildMessages(id); cmd != nil {
					return m, cmd
				}
			}
			return m, nil
		}
		m.status = "no tool to fold"
		// Plan 20: status changed → re-render footer.
		m = m.rerenderChrome()
		return m, nil
	case ">":
		// Descend into the last task tool's child session (plan 08e §C1).
		// ctrl+x > opens the most recent sub-agent's child as a full chat
		// view (openSession), matching Android's "Open in new view" button.
		// Mnemonic: ">" = "go deeper into". When the child id can't be
		// recovered (no metadata, no <task id> wrapper) fall back to
		// enterFirstChild (the existing "first child" nav) so the chord is
		// still useful when the task part predates the metadata.
		if task := m.lastTaskPart(); task != nil {
			st, _ := parseToolState(task.State)
			if cid := childSessionID(st); cid != "" {
				var cmd tea.Cmd
				m, cmd = m.openSession(cid)
				// Plan 20: session switch → re-render all.
				m = m.rerenderFull()
				return m, cmd
			}
		}
		nm, cmd := m.enterFirstChild()
		m = nm.(Model)
		// Plan 20: session switch → re-render all.
		m = m.rerenderFull()
		return m, cmd
	case "e":
		return m, openEditorCmd(m.input.Value())
	case "d":
		var cmd tea.Cmd
		m, cmd = m.openDiff() // full-screen diff reviewer
		// Plan 20: diff reviewer open → re-render chrome (modal layer reads
		// the diff state; body is skipped by modalClassActive).
		m = m.rerenderChrome()
		return m, cmd
	case "`":
		nm, cmd := m.focusOrOpenPTY() // embedded terminal (ctrl+] to exit)
		m = nm.(Model)
		// Plan 20: PTY pane opened → re-render footer (PTY pane shows).
		m = m.rerenderChrome()
		return m, cmd
	case "w":
		m = m.stashDraft() // park the current composer draft
		// Plan 20: composer cleared → re-render footer.
		m = m.rerenderChrome()
		return m, nil
	case "i":
		// Toggle inline image rendering for image file parts (plan 08e §E2).
		// Default off: most terminals can't decode Sixel/iTerm2 escapes and
		// emitting them to an unsupported terminal produces garbage. When
		// on, renderImagePart probes the terminal and emits the matching
		// escape (Sixel or iTerm2 inline) or falls back to a placeholder
		// glyph when no support is advertised.
		m.view.images = !m.view.images
		m.status = toggleHint("images", m.view.images)
		// Plan 20: view toggle affects body (image rendering) + footer.
		m = m.rerenderFull()
		return m, nil
	case "down":
		nm, cmd := m.enterFirstChild() // descend into the first sub-agent child
		m = nm.(Model)
		// Plan 20: session switch → re-render all.
		m = m.rerenderFull()
		return m, cmd
	case "up":
		nm, cmd := m.gotoParent() // return to the parent session
		m = nm.(Model)
		// Plan 20: session switch → re-render all.
		m = m.rerenderFull()
		return m, cmd
	case "]":
		nm, cmd := m.cycleSibling(+1) // next sibling sub-agent
		m = nm.(Model)
		// Plan 20: session switch → re-render all.
		m = m.rerenderFull()
		return m, cmd
	case "[":
		nm, cmd := m.cycleSibling(-1) // previous sibling sub-agent
		m = nm.(Model)
		// Plan 20: session switch → re-render all.
		m = m.rerenderFull()
		return m, cmd
	}
	return m, nil
}

// submit sends the composer's text: it creates a session first if none is open,
// then prompts. In shell mode it runs the text as a shell command instead.
// Requires a resolved model.
func (m Model) submit() (tea.Model, tea.Cmd) {
	text := m.composeSubmitText()
	if text == "" && len(m.pendingFiles) == 0 {
		return m, nil
	}
	m.exiting = false  // sending a prompt cancels the armed exit guard
	m.deleting = false // and the delete-session guard (08f H1a)
	if !m.model.ok() {
		m.status = "no model configured (pass --provider/--model)"
		// Plan 20: status changed → re-render footer.
		m = m.rerenderChrome()
		return m, nil
	}
	if text != "" {
		m = m.pushHistory(text) // remember the submission for up/down recall
	}
	m.persist()
	files := m.pendingFiles
	m.pendingFiles = nil
	m.pasteParts = nil
	// Shell mode: run the text as a command in the open session; output streams
	// back as tool parts. Requires an existing session (no implicit create).
	if m.shellMode {
		m.shellMode = false
		if m.cfg.SessionID == "" {
			m.status = "open a session before running a shell command"
			// Plan 20: status changed → re-render footer.
			m = m.rerenderChrome()
			return m, nil
		}
		m.input.SetValue("")
		m = m.resizeComposer()
		// Plan 20: composer cleared + shell mode exited → re-render footer.
		m = m.rerenderChrome()
		return m, shellCmd(m.ctx, m.client, m.cfg.SessionID, text, m.effectiveAgent(), m.model)
	}
	m.input.SetValue("")
	m = m.resizeComposer() // collapse back to one row
	m.scroll.ToTail()      // a new prompt snaps the stream back to the live tail
	// Plan 20: composer cleared → re-render footer.
	m = m.rerenderChrome()
	if m.cfg.SessionID == "" {
		return m, createSessionCmd(m.ctx, m.client, text, files)
	}
	return m, promptCmd(m.ctx, m.client, m.cfg.SessionID, text, m.model, m.agent, files)
}

// scrollStep is the default lines moved per scrollback keypress / wheel notch
// when no scroll_speed is configured (plan 08f H13). Prefer m.scrollLines().
const scrollStep = defaultScrollStep

// applyTUIFileConfig overlays a loaded tui.json / opencode.json TUI section
// onto the Model (plan 08f H13 / G.15). CLI/env knobs already applied in New()
// win over the file (e.g. OPENCODE_DISABLE_MOUSE, --theme).
func (m *Model) applyTUIFileConfig(cfg tuiFileConfig) {
	if cfg.Mouse != nil && !*cfg.Mouse && os.Getenv("OPENCODE_DISABLE_MOUSE") == "" {
		m.mouseDisabled = true
	}
	if cfg.ScrollSpeed != nil {
		m.scrollStep = scrollStepFromSpeed(*cfg.ScrollSpeed)
	}
	if cfg.DiffStyle == "stacked" {
		m.diffTreeHidden = true
	}
	if cfg.Theme != "" && m.cfg.Theme == "" {
		m.cfg.Theme = cfg.Theme
	}
	if cfg.LeaderTimeout != nil && *cfg.LeaderTimeout > 0 {
		m.leaderTimeoutMs = *cfg.LeaderTimeout
	}
	if cfg.Prompt != nil && cfg.Prompt.MaxHeight != nil && *cfg.Prompt.MaxHeight > 0 {
		m.composerMaxRows = *cfg.Prompt.MaxHeight
	}
	if len(cfg.Keybinds) > 0 {
		m.tuiKeybinds = cfg.Keybinds
	}
}

// scrollLines returns the effective stream scroll step (config overlay).
func (m Model) scrollLines() int {
	if m.scrollStep > 0 {
		return m.scrollStep
	}
	return defaultScrollStep
}

// scrollBodyHeight returns the height of the stream viewport (screen height
// minus the footer's height) — the dimension the half-page scroll keys (plan
// 17 §A3) scale against. Plan 20: reads the pre-computed m.footerHeight
// (set by renderFooter in Update) so no lipgloss.Height call here. Falls
// back to a live buildFooter measurement when the pre-rendered height
// isn't available yet (e.g. pre-first-resize).
func (m Model) scrollBodyHeight() int {
	if m.height <= 0 {
		return 1
	}
	h := m.height - m.footerHeight
	if h < 1 {
		h = 1
	}
	return h
}

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

// applyClipboardRead handles clipboardReadMsg from ctrl+v (plan 08f H2).
// Text inserts via the bracketed-paste path; images stage a pendingFile and
// insert an [Image N] marker into the composer (opencode pasteAttachment).
func (m Model) applyClipboardRead(msg clipboardReadMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.status = "clipboard: " + msg.Err.Error()
		m = m.rerenderChrome()
		return m, nil
	}
	m.histIdx = -1
	m.exiting = false
	m.deleting = false
	if strings.HasPrefix(msg.Mime, "image/") {
		n := 1
		for _, f := range m.pendingFiles {
			if strings.HasPrefix(f.Mime, "image/") {
				n++
			}
		}
		name := "clipboard"
		m.pendingFiles = append(m.pendingFiles, pendingFile{
			Filename: name,
			Mime:     msg.Mime,
			URL:      dataURL(msg.Mime, msg.Data),
		})
		marker := "[Image " + humanInt(n) + "] "
		return m.insertComposerText(marker)
	}
	// text/plain (and any non-image fallback)
	return m.maybeSmartPaste(string(msg.Data))
}

// insertComposerText inserts s at the cursor via the textarea's PasteMsg
// handler (same path as bracketed paste), then re-fits chrome.
func (m Model) insertComposerText(s string) (Model, tea.Cmd) {
	var cmd, acCmd tea.Cmd
	m.input, cmd = m.input.Update(tea.PasteMsg{Content: s})
	m = m.resizeComposer()
	m, acCmd = m.refreshAutocomplete()
	m = m.rerenderFull()
	return m, tea.Batch(cmd, acCmd)
}

// View renders the active screen via the v2 canvas compositor (plan 08e §A1+A2).
// View satisfies bubbletea v2's Model interface, which now returns a tea.View
// struct (was a string in v1). The wrapper declares the program-level terminal
// toggles that used to be tea.NewProgram options (AltScreen replaces
// tea.WithAltScreen in cmd/opcode-tui/main.go).
//
// The whole-frame compositing lives in composeView (canvas.go): it builds a
// NewCanvas(w,h), fills the base with the theme Bg, and composes each pane and
// overlay as a Layer at its (x,y,z). Every cell is owned by the canvas — no
// terminal-default bleed, no manual bg re-emit, no string-splice overlays.
func (m Model) View() tea.View {
	v := tea.NewView(m.composeView())
	v.AltScreen = true
	// AllMotion is required for passive hover (plan 08f H4). CellMotion only
	// reports motion while a button is held (drag), which made modal/
	// autocomplete row preview unreachable. OPENCODE_DISABLE_MOUSE (plan
	// 08f H12 / G.14) drops capture entirely instead.
	v.MouseMode = tea.MouseModeAllMotion
	if m.mouseDisabled {
		v.MouseMode = tea.MouseModeNone
	}
	v.WindowTitle = m.windowTitle()
	return v
}

// windowTitle returns the OSC 0 title for the current screen (plan 08f H6 /
// opencode app.tsx:447-471). Empty when titles are disabled.
func (m Model) windowTitle() string {
	if !m.terminalTitleEnabled {
		return ""
	}
	if m.screen != ScreenSession || m.cfg.SessionID == "" {
		return "Opcode42"
	}
	title := m.sessionTitle(m.cfg.SessionID)
	if title == "" || isDefaultSessionTitle(title) {
		return "Opcode42"
	}
	return "OC | " + truncate(title, 40)
}

// isDefaultSessionTitle matches the daemon's untouched auto title
// ("New session - <RFC3339-millis>") and the TUI's own "session <id>"
// placeholder — same intent as opencode isDefaultTitle / session.IsDefaultTitle.
func isDefaultSessionTitle(title string) bool {
	if strings.HasPrefix(title, "session ") {
		return true
	}
	return defaultSessionTitleRe.MatchString(title)
}

var defaultSessionTitleRe = regexp.MustCompile(`^New session - \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// renderView is the v1 render entry, retained as a thin shim around the v2
// composeView so tests that call renderView() keep working through the
// transition. New code should call composeView directly.
//
// Deprecated: use composeView (the v2 canvas path). This shim exists only to
// keep the existing test surface green while the canvas adoption lands; a
// follow-up will migrate the tests over and delete this alias.
func (m Model) renderView() string { return m.composeView() }

// viewSplash renders the splash screen content for the pre-resize fallback
// (width/height <= 0). The canvas path (splashLayers) calls splashContent
// directly and wraps it in a Layer; this entry point only runs when
// dimensions are non-positive, so it forwards to splashContent which handles
// the plain-stack pre-resize layout.
func (m Model) viewSplash() string {
	return m.splashContent(m.width, m.height)
}
