package tui

import (
	"context"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rotemmiz/opcode42/internal/mdns"
	opcode42client "github.com/rotemmiz/opcode42/sdk/go"
)

// Connect-overlay messages (plan 08e §D2).
type (
	// discoveredServerMsg carries one mDNS-resolved daemon to the Update loop.
	// The browser goroutine emits one per DiscoveredService surfaced by
	// mdns.Browse; Update appends it to m.discoveredServers and kicks a
	// reachability probe.
	discoveredServerMsg struct {
		service mdns.DiscoveredService
	}

	// discoverStartedMsg carries the live browser channel to Update so it can
	// be stashed on m.discoverOut; `first` is the first resolved service (if
	// any) so Update can append it without a separate round-trip.
	discoverStartedMsg struct {
		out   <-chan mdns.DiscoveredService
		first *mdns.DiscoveredService
	}

	// serverProbeMsg carries the result of a best-effort GET /global/health
	// probe for one discovered daemon. The probe is async and best-effort — a
	// slow/unreachable daemon reads as "unknown" (amber dot), never blocks
	// selection, and is re-run only when the service (re)surfaces.
	serverProbeMsg struct {
		key       string // host:port — matches connectProbeKey
		reachable bool
	}
)

// serverProbeState is the cached reachability for one discovered daemon.
type serverProbeState struct {
	reachable bool
}

// probeTimeout caps the best-effort health probe. Short on purpose: the dot
// is a hint, not a guarantee, and we don't want a slow daemon to keep the
// amber dot lit forever when it's actually fine.
const probeTimeout = 2 * time.Second

// openConnectModal starts the mDNS browser and opens the connect overlay. The
// browser is cancelled when the modal closes (closeConnectModal). The manual
// URL field is pre-filled with cfg.URL (or the default 127.0.0.1:4096 when
// empty) so a first-run user has a sensible starting point.
func (m Model) openConnectModal() Model {
	m.modal = modalConnect
	m.modalSel = 0
	m.connectFieldFocus = false
	m.discoveredServers = nil
	m.serverProbe = map[string]serverProbeState{}
	// Pre-fill the URL field with the current/default URL.
	prefill := m.cfg.URL
	if prefill == "" {
		prefill = "http://127.0.0.1:4096"
	}
	m.connectURLInput.SetValue(prefill)
	m.connectURLInput.CursorEnd()
	m.connectURLInput.Blur()
	if m.cfg.NoDiscover {
		// --no-discover: skip the browser, leave the list empty. The manual
		// URL field is the only entry path (plan 08e §D3).
		m.status = "connect — enter a daemon URL"
		return m
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.discoverCtx, m.discoverCancel = ctx, cancel
	m.status = "browsing for nearby daemons…"
	return m
}

// startDiscoverCmd is the tea.Cmd returned by openConnectModal's caller to
// actually start the mDNS browser. It's a separate Cmd (not inline) so the
// modal opens immediately and the browser starts asynchronously.
func startDiscoverCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		out, err := mdns.Browse(ctx, nil, nil)
		if err != nil {
			return nil // best-effort — the manual URL field is the fallback
		}
		// Read the first service synchronously so the overlay can show one
		// row immediately; subsequent services arrive via discoverNextCmd.
		select {
		case svc, ok := <-out:
			if !ok {
				return discoverStartedMsg{out: out}
			}
			return discoverStartedMsg{out: out, first: &svc}
		case <-ctx.Done():
			return nil
		}
	}
}

// discoverNextCmd reads the next service from the live browser channel and
// returns it as a discoveredServerMsg. Update re-issues this after each
// service to keep pumping until the channel closes (ctx cancel).
func discoverNextCmd(ctx context.Context, out <-chan mdns.DiscoveredService) tea.Cmd {
	if out == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case svc, ok := <-out:
			if !ok {
				return nil
			}
			return discoveredServerMsg{service: svc}
		case <-ctx.Done():
			return nil
		}
	}
}

// probeServerCmd issues a best-effort GET /global/health to one discovered
// daemon and emits serverProbeMsg with the result. The probe uses a fresh
// http.Client (no auth, no directory header) — reachability is the only
// question; auth/directory are applied by the real connect path.
func probeServerCmd(key, url string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: probeTimeout}
		req, err := http.NewRequest(http.MethodGet, url+"/global/health", nil)
		if err != nil {
			return serverProbeMsg{key: key, reachable: false}
		}
		resp, err := client.Do(req)
		if err != nil {
			return serverProbeMsg{key: key, reachable: false}
		}
		_ = resp.Body.Close()
		return serverProbeMsg{key: key, reachable: resp.StatusCode == http.StatusOK}
	}
}

// closeConnectModal cancels the mDNS browser (if running) and resets the
// overlay state. Returns the modified Model (the cancel must propagate —
// closeConnectModal is a value-receiver method, so callers must capture the
// returned Model, not just call it for the side effect).
func (m Model) closeConnectModal() (Model, tea.Cmd) {
	if m.discoverCancel != nil {
		m.discoverCancel()
	}
	m.discoverCtx, m.discoverCancel = nil, nil
	m.discoverOut = nil
	m.modal, m.modalSel = modalNone, 0
	m.connectFieldFocus = false
	m.connectURLInput.Blur()
	return m, nil
}

// connectTo rebuilds the SDK client with the given URL, closes the connect
// overlay, and re-issues the health + SSE bootstrap — the same path a CLI
// --url connect takes. On success the URL is pinned to KV (server_url) so
// subsequent runs skip the overlay (plan 08e §D3). The URL is trimmed before
// dialing.
func (m Model) connectTo(url string) (tea.Model, tea.Cmd) {
	url = strings.TrimSpace(url)
	if url == "" {
		m.status = "no URL"
		return m, nil
	}
	c, err := opcode42client.New(url, opcode42client.Options{
		Directory: m.cfg.Directory, Username: m.cfg.Username, Password: m.cfg.Password,
	})
	if err != nil {
		m.status = "connect failed: " + err.Error()
		return m, nil
	}
	// Stop the browser, close the overlay, swap the client, persist the URL.
	if m.discoverCancel != nil {
		m.discoverCancel()
	}
	m.discoverCtx, m.discoverCancel = nil, nil
	m.discoverOut = nil
	m.client = c
	m.cfg.URL = url
	m.conn = Connecting
	m.status = "connecting to " + url
	m.modal, m.modalSel = modalNone, 0
	m.connectFieldFocus = false
	m.connectURLInput.Blur()
	m.persistServerURL(url)
	return m, tea.Batch(healthCmd(m.ctx, c), m.maybeKickAnim())
}

// applyServerURL rebinds the model to a different daemon URL without going
// through the connect overlay. Used by Restore when a KV-pinned server_url
// is present (plan 08e §D3): the TUI skips the picker and connects directly.
func (m Model) applyServerURL(url string) Model {
	url = strings.TrimSpace(url)
	if url == "" {
		return m
	}
	c, err := opcode42client.New(url, opcode42client.Options{
		Directory: m.cfg.Directory, Username: m.cfg.Username, Password: m.cfg.Password,
	})
	if err != nil {
		m.conn, m.err = ConnError, err
		return m
	}
	m.client = c
	m.cfg.URL = url
	m.status = "connecting to " + url
	return m
}
