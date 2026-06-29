package tui

// TUI <-> Opcode42 dual-run parity (plan 08 U13).
//
// These tests drive the REAL TUI Model.Update loop against a REAL, in-process
// Opcode42 daemon (the production internal/server handler wired to the agent engine
// + a deterministic mock provider). They prove the dogfood client's core flows
// work end-to-end against Opcode42 — not opencode — over the actual HTTP+SSE wire:
//
//   - health + global SSE subscribe (connectedMsg -> stream open -> listen)
//   - session list bootstrap + history load
//   - prompt -> streamed message/part SSE rendered into the store
//   - permission round-trip (permission.asked overlay -> POST reply -> unblock)
//   - abort (POST /session/{id}/abort)
//
// Determinism: the mock provider replays a scripted llm.Event stream, so there is
// no real LLM and no provider key — CI stays green. The full-LLM path lives in
// scripts/run-conformance.sh `live` (skip-gated on a provider key) and is NOT
// depended on here.
//
// This is the TUI side of the U13 parity gate: the conformance suite (plan 12)
// already records the TUI's READ surface against opencode and dual-runs it vs
// Opcode42; this exercises the TUI's own reduce/render against a live Opcode42 daemon.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rotemmiz/opcode42/internal/auth"
	"github.com/rotemmiz/opcode42/internal/bus"
	"github.com/rotemmiz/opcode42/internal/engine/catalog"
	"github.com/rotemmiz/opcode42/internal/engine/enginetest"
	"github.com/rotemmiz/opcode42/internal/engine/llm"
	"github.com/rotemmiz/opcode42/internal/engine/message"
	"github.com/rotemmiz/opcode42/internal/engine/permission"
	"github.com/rotemmiz/opcode42/internal/engine/registry"
	"github.com/rotemmiz/opcode42/internal/engine/tool"
	"github.com/rotemmiz/opcode42/internal/instance"
	"github.com/rotemmiz/opcode42/internal/server"
	"github.com/rotemmiz/opcode42/internal/session"
	"github.com/rotemmiz/opcode42/internal/storage"
	"github.com/rotemmiz/opcode42/internal/worktree"
)

// opcode42Rig is a real Opcode42 daemon (engine + mock provider) fronted by httptest,
// plus the per-directory instance so a test can reach its permission manager.
type opcode42Rig struct {
	srv  *httptest.Server
	dir  string
	inst *instance.Context
	mock *enginetest.MockProvider
}

// newOpcode42Rig boots a real in-process Opcode42 daemon wired to the agent engine and
// a deterministic mock provider replaying the given scripts.
func newOpcode42Rig(t *testing.T, scripts ...[]llm.Event) *opcode42Rig {
	t.Helper()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dir := t.TempDir()
	mock := enginetest.NewMockProvider(scripts...)
	global := bus.NewGlobal()
	instances := instance.NewManager(global)

	handler, err := server.New(server.Options{
		Version:   "u13-test",
		Auth:      auth.Config{},
		Cwd:       dir,
		Sessions:  session.NewStore(db),
		Instances: instances,
		Messages:  message.NewStore(db),
		Catalog:   catalog.Fixture(),
		Registry:  registry.New(tool.Read{}, tool.Bash{}),
		Global:    global,
		Providers: func(context.Context, string, string) (llm.Provider, error) { return mock, nil },
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// The daemon's directory middleware normalizes the request directory via
	// worktree.Resolve (symlink-canonicalised on macOS: /var -> /private/var).
	// Resolve here too so r.inst is the SAME per-directory instance the TUI's
	// requests (x-opencode-directory: dir) land on.
	return &opcode42Rig{srv: srv, dir: dir, inst: instances.Get(worktree.Resolve(dir)), mock: mock}
}

// newModel builds a real TUI Model pointed at the rig's daemon, with the prompt
// model pre-resolved to the fixture catalog's openai/gpt-4o so submit() proceeds.
func (r *opcode42Rig) newModel() Model {
	m := New(Config{
		URL: r.srv.URL, Directory: r.dir,
		Provider: "openai", Model: "gpt-4o",
		Theme: "opcode42-dark", // deterministic palette for capture
	})
	m.width, m.height = 120, 40 // give the View() a real layout
	return m
}

// driver pumps the Bubble Tea Model the way tea.Program would, but synchronously
// and deterministically: it runs each returned tea.Cmd (in a goroutine, since SSE
// listen/health block), funnelling the resulting tea.Msg back through Update via a
// channel. It mirrors the real runtime without a terminal.
type driver struct {
	t  *testing.T
	m  Model
	in chan tea.Msg
}

func newDriver(t *testing.T, m Model) *driver {
	d := &driver{t: t, m: m, in: make(chan tea.Msg, 256)}
	// Init kicks the health check (+ anim tick).
	d.run(m.Init())
	return d
}

// run executes a command off the Update loop in its own goroutine (health/SSE
// cmds block, so they can't run inline). A tea.Batch is unwrapped into its leaf
// cmds, each run the same way; every other result is posted to the driver's
// channel for pump/inject to feed back through Update. The flows under test only
// ever produce Batch/leaf cmds (never tea.Sequence), so no sequence handling is
// needed; an unexpected sequenceMsg would surface as a pump timeout.
func (d *driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		switch v := msg.(type) {
		case tea.BatchMsg:
			for _, c := range v {
				d.run(c)
			}
		case nil:
			// no-op (e.g. a tick that chose not to reschedule)
		default:
			d.in <- v
		}
	}()
}

// pump waits up to a deadline for messages, feeding each through Update and
// running the resulting cmd, until stop(model) returns true. It drops anim ticks
// to avoid a busy-loop (they don't affect the assertions here).
func (d *driver) pump(stop func(Model) bool) {
	d.t.Helper()
	deadline := time.After(8 * time.Second)
	for !stop(d.m) {
		select {
		case msg := <-d.in:
			if _, ok := msg.(animTickMsg); ok {
				continue // ignore animation frames
			}
			var cmd tea.Cmd
			d.m, cmd = d.step(msg)
			d.run(cmd)
		case <-deadline:
			d.t.Fatalf("pump timed out; conn=%v events=%d sessions=%d", d.m.conn, d.m.eventCount, len(d.m.store.sessions))
		}
	}
}

// inject feeds a synthetic message (e.g. a key press) into Update and runs its cmd.
func (d *driver) inject(msg tea.Msg) {
	var cmd tea.Cmd
	d.m, cmd = d.step(msg)
	d.run(cmd)
}

func (d *driver) step(msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := d.m.Update(msg)
	return next.(Model), cmd
}

// TestOpcode42Parity_ConnectAndStream proves the TUI connects to a real Opcode42 daemon,
// opens the global SSE stream, and bootstraps — the connection lifecycle the
// dogfood client depends on, exercised over the wire (not via injected msgs).
func TestOpcode42Parity_ConnectAndStream(t *testing.T) {
	r := newOpcode42Rig(t)
	d := newDriver(t, r.newModel())

	d.pump(func(m Model) bool { return m.conn == Connected && m.stream != nil })

	if d.m.conn != Connected {
		t.Fatalf("conn = %v, want Connected", d.m.conn)
	}
	if d.m.stream == nil {
		t.Fatal("global SSE stream never opened against Opcode42")
	}
	d.m.stream.Close()
}

// TestOpcode42Parity_PromptStreamsParts is the hero flow: against a real Opcode42
// daemon, create a session, submit a prompt, and assert the streamed
// message/part SSE lands in the TUI store and renders. No real LLM — the mock
// provider scripts the assistant turn.
func TestOpcode42Parity_PromptStreamsParts(t *testing.T) {
	script := enginetest.NewScript().StepStart().
		Text("t1", "Hello", ", ", "Opcode42").
		StepFinish("stop", llm.TokenUsage{Input: 4, Output: 3}).Finish().Events()
	r := newOpcode42Rig(t, script)
	d := newDriver(t, r.newModel())

	// Wait until connected + streaming before prompting.
	d.pump(func(m Model) bool { return m.conn == Connected && m.stream != nil })

	// Type a prompt and submit — submit() creates a session then prompts, both
	// over the real wire.
	d.m.input.SetValue("hi opcode42")
	d.inject(key("enter"))

	// The assistant text streams back as message.part deltas/updates over
	// /global/event. Wait until the rendered session contains the scripted reply.
	d.pump(func(m Model) bool {
		return m.cfg.SessionID != "" && assistantTextContains(m, "Hello, Opcode42")
	})

	if got := assistantText(d.m); got != "Hello, Opcode42" {
		t.Fatalf("streamed assistant text = %q, want %q", got, "Hello, Opcode42")
	}
	// The session view must render the streamed reply (markdown may reflow, so
	// match a distinctive word on the ANSI-stripped frame).
	if !strings.Contains(stripANSI(d.m.View()), "Opcode42") {
		t.Fatalf("session view missing streamed assistant text")
	}
	d.m.stream.Close()
}

// TestOpcode42Parity_PermissionRoundTrip proves the blocking permission overlay works
// end-to-end against Opcode42: a real permission.asked (published by the daemon's
// permission manager, the exact path the engine uses) surfaces in the TUI; the
// user allows it; the TUI POSTs the reply; the daemon's Ask() unblocks.
func TestOpcode42Parity_PermissionRoundTrip(t *testing.T) {
	r := newOpcode42Rig(t)
	d := newDriver(t, r.newModel())
	d.pump(func(m Model) bool { return m.conn == Connected && m.stream != nil })

	// Drive a real permission ask through the daemon's manager (the same call the
	// engine's executor makes for a tool that needs approval). It blocks until the
	// TUI replies over the wire.
	askDone := make(chan error, 1)
	go func() {
		askDone <- r.inst.Permissions.Ask(context.Background(), permission.AskInput{
			SessionID:  "ses_x",
			Permission: "bash",
			Patterns:   []string{"rm -rf /"},
			Metadata:   map[string]any{"command": "rm -rf /"},
		})
	}()

	// The permission.asked event flows through /global/event into the overlay.
	d.pump(func(m Model) bool { return m.pendingPermission() != nil })

	if got := d.m.pendingPermission(); got == nil || got.Permission != "bash" {
		t.Fatalf("pending permission = %+v, want bash", got)
	}
	// The overlay must render.
	if !strings.Contains(stripANSI(d.m.View()), "Permission required") {
		t.Fatal("permission overlay did not render")
	}

	// Reject it ("r") — the TUI POSTs /permission/{id}/reply over the wire.
	d.inject(key("r"))

	// The daemon's Ask() must unblock with a denial (reject -> DeniedError).
	select {
	case err := <-askDone:
		if err == nil {
			t.Fatal("Ask returned nil; expected denial after reject")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("daemon Ask never unblocked after the TUI replied")
	}

	// And the overlay must clear (optimistically and/or via permission.replied).
	d.pump(func(m Model) bool { return m.pendingPermission() == nil })
	d.m.stream.Close()
}

// TestOpcode42Parity_Abort proves the abort flow reaches the real daemon: with a
// session open, POST /session/{id}/abort returns cleanly and the TUI reports the
// interrupt.
func TestOpcode42Parity_Abort(t *testing.T) {
	r := newOpcode42Rig(t)
	d := newDriver(t, r.newModel())
	d.pump(func(m Model) bool { return m.conn == Connected && m.stream != nil })

	// Create a session first (abort needs one) via the real wire.
	d.run(createSessionCmd(d.m.ctx, d.m.client, ""))
	d.pump(func(m Model) bool { return m.cfg.SessionID != "" })

	// Abort the (idle) session — opencode/Opcode42 both 200 a no-op abort.
	d.run(abortSessionCmd(d.m.ctx, d.m.client, d.m.cfg.SessionID))
	d.pump(func(m Model) bool { return m.status == "interrupted" })

	if d.m.status != "interrupted" {
		t.Fatalf("status = %q, want interrupted", d.m.status)
	}
	d.m.stream.Close()
}

// --- assertion helpers -------------------------------------------------------

// assistantText concatenates the text of all assistant message parts in the
// model's open session.
func assistantText(m Model) string {
	var out string
	for _, msg := range m.store.messages[m.cfg.SessionID] {
		if msg.Role != "assistant" {
			continue
		}
		for _, p := range m.store.parts[msg.ID] {
			if p.Type == "text" {
				out += p.Text
			}
		}
	}
	return out
}

func assistantTextContains(m Model, sub string) bool {
	return strings.Contains(assistantText(m), sub)
}
