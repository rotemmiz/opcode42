package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Timeout constants port opencode's lsp/client.ts:18-23.
const (
	initializeTimeout       = 45 * time.Second
	diagnosticsRequestWait  = 3 * time.Second
	diagnosticsDocumentWait = 5 * time.Second
	diagnosticsFullWait     = 10 * time.Second
	diagnosticsDebounce     = 150 * time.Millisecond
)

// LSP didChangeWatchedFiles change kinds and the incremental sync kind
// (lsp/client.ts:26-28).
const (
	fileChangeCreated         = 1
	fileChangeChanged         = 2
	textDocumentSyncIncrement = 2
)

// DiagnosticsMode selects how aggressively TouchFile waits for diagnostics.
// "" means do not wait (open only); "document" waits for the touched file's
// diagnostics; "full" also drains workspace diagnostics (lsp/lsp.ts:346 +
// client.ts waitForDocumentDiagnostics/waitForFullDiagnostics).
type DiagnosticsMode string

const (
	// DiagModeDocument waits for the touched document's diagnostics only.
	DiagModeDocument DiagnosticsMode = "document"
	// DiagModeFull waits for document + workspace diagnostics.
	DiagModeFull DiagnosticsMode = "full"
)

// Client is the JSON-RPC client bound to one spawned language server. It runs
// the initialize/initialized handshake, tracks server capabilities, holds both
// push (textDocument/publishDiagnostics) and pull (textDocument/diagnostic,
// workspace/diagnostic) diagnostic maps, dedups them, and debounces push
// updates. Ports opencode's lsp/client.ts (create()).
type Client struct {
	serverID  string
	root      string
	directory string

	conn jsonrpc2.Conn
	proc io.Closer // closes the held stdio pipes on shutdown

	// initialization is the server's initializationOptions / didChangeConfiguration
	// settings, surfaced to workspace/configuration requests.
	initialization map[string]any

	// syncKind is the server's advertised textDocumentSync kind (incremental == 2).
	syncKind int
	// hasStaticPullDiagnostics reflects ServerCapabilities.diagnosticProvider.
	hasStaticPullDiagnostics bool

	mu        sync.Mutex
	pushDiags map[string][]protocol.Diagnostic
	pullDiags map[string][]protocol.Diagnostic
	// published records the last push time per file (for the debounce wait).
	published map[string]time.Time
	// files tracks open document versions for incremental didChange.
	files map[string]fileState
	// diagRegs holds dynamically registered textDocument/diagnostic capabilities.
	diagRegs map[string]diagRegistration

	// pushSignal is closed-and-replaced whenever a push arrives, so waiters can
	// observe fresh pushes without polling (a condition-variable-style broadcast).
	pushSignal chan struct{}
	// regSignal is closed-and-replaced on a diagnostic capability (un)registration.
	regSignal chan struct{}
}

type fileState struct {
	version int32
	text    string
}

type diagRegistration struct {
	id                   string
	identifier           string
	workspaceDiagnostics bool
}

// newClient runs the handshake against an already-spawned server process and
// returns a ready Client, or an error (the caller adds the server to the broken
// set on error). proc closes the held stdio pipes on shutdown. directory is the
// instance directory used to resolve relative paths.
func newClient(ctx context.Context, serverID, root, directory string, rwc io.ReadWriteCloser, proc io.Closer, initialization map[string]any) (*Client, error) {
	c := &Client{
		serverID:       serverID,
		root:           root,
		directory:      directory,
		proc:           proc,
		initialization: initialization,
		pushDiags:      make(map[string][]protocol.Diagnostic),
		pullDiags:      make(map[string][]protocol.Diagnostic),
		published:      make(map[string]time.Time),
		files:          make(map[string]fileState),
		diagRegs:       make(map[string]diagRegistration),
		pushSignal:     make(chan struct{}),
		regSignal:      make(chan struct{}),
	}
	c.conn = jsonrpc2.NewConn(jsonrpc2.NewStream(rwc))
	c.conn.Go(context.Background(), c.handle)

	if err := c.initialize(ctx); err != nil {
		_ = c.conn.Close()
		_ = proc.Close()
		return nil, err
	}
	return c, nil
}

// rootURI is the file:// URI for the server root.
func (c *Client) rootURI() string { return string(uri.File(c.root)) }

// initialize performs the LSP initialize handshake, sends initialized, and
// pushes the configuration. It records the sync kind and pull-diagnostic
// capability from the result. Ports lsp/client.ts:248-305.
func (c *Client) initialize(ctx context.Context) error {
	ictx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()

	params := map[string]any{
		"rootUri":   c.rootURI(),
		"processId": os.Getpid(),
		"workspaceFolders": []map[string]any{
			{"name": "workspace", "uri": c.rootURI()},
		},
		"initializationOptions": c.initialization,
		"capabilities": map[string]any{
			"window": map[string]any{"workDoneProgress": true},
			"workspace": map[string]any{
				"configuration": true,
				"didChangeWatchedFiles": map[string]any{
					"dynamicRegistration": true,
				},
				"diagnostics": map[string]any{"refreshSupport": false},
			},
			"textDocument": map[string]any{
				"synchronization": map[string]any{
					"didOpen":   true,
					"didChange": true,
				},
				"diagnostic": map[string]any{
					"dynamicRegistration":    true,
					"relatedDocumentSupport": true,
				},
				"publishDiagnostics": map[string]any{
					"versionSupport": false,
				},
			},
		},
	}

	// We decode capabilities into a minimal local shape (not protocol.Server-
	// Capabilities) so we control exactly the two fields we read — textDocumentSync
	// and diagnosticProvider (an LSP 3.17 field not present in the protocol
	// package's struct). Mirrors opencode's hand-rolled ServerCapabilities
	// (client.ts:79-87).
	var result struct {
		Capabilities serverCapabilities `json:"capabilities"`
	}
	if _, err := c.conn.Call(ictx, "initialize", params, &result); err != nil {
		return fmt.Errorf("lsp %s initialize: %w", c.serverID, err)
	}

	c.syncKind = syncKindOf(result.Capabilities)
	c.hasStaticPullDiagnostics = result.Capabilities.DiagnosticProvider != nil

	if err := c.conn.Notify(ctx, "initialized", map[string]any{}); err != nil {
		return fmt.Errorf("lsp %s initialized: %w", c.serverID, err)
	}
	if c.initialization != nil {
		if err := c.conn.Notify(ctx, "workspace/didChangeConfiguration", map[string]any{
			"settings": c.initialization,
		}); err != nil {
			return fmt.Errorf("lsp %s didChangeConfiguration: %w", c.serverID, err)
		}
	}
	return nil
}

// serverCapabilities is the minimal subset of the initialize result we read:
// the text sync kind and whether the server statically advertises pull
// diagnostics. Mirrors opencode's ServerCapabilities (client.ts:79-87).
type serverCapabilities struct {
	// TextDocumentSync is a number (kind) or an object {change}.
	TextDocumentSync any `json:"textDocumentSync"`
	// DiagnosticProvider, when present, means the server supports pull diagnostics.
	DiagnosticProvider any `json:"diagnosticProvider"`
}

// syncKindOf extracts the textDocumentSync change kind from the server
// capabilities (number form or {change} object). Ports getSyncKind
// (client.ts:94-99).
func syncKindOf(caps serverCapabilities) int {
	switch v := caps.TextDocumentSync.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case map[string]any:
		if ch, ok := v["change"].(float64); ok {
			return int(ch)
		}
	}
	return 0
}

// handle dispatches inbound server→client requests and notifications. Push
// diagnostics, workspace/configuration, and dynamic capability (un)registration
// are handled; everything else is replied to with a benign default so the
// server does not stall. Ports the connection.on* handlers in client.ts:191-244.
func (c *Client) handle(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	switch req.Method() {
	case "textDocument/publishDiagnostics":
		var p protocol.PublishDiagnosticsParams
		if err := json.Unmarshal(req.Params(), &p); err == nil {
			c.onPublishDiagnostics(p)
		}
		return reply(ctx, nil, nil)

	case "workspace/configuration":
		return reply(ctx, c.configurationResult(req.Params()), nil)

	case "client/registerCapability":
		c.onRegisterCapability(req.Params())
		return reply(ctx, nil, nil)

	case "client/unregisterCapability":
		c.onUnregisterCapability(req.Params())
		return reply(ctx, nil, nil)

	case "workspace/workspaceFolders":
		return reply(ctx, []map[string]any{
			{"name": "workspace", "uri": c.rootURI()},
		}, nil)

	case "window/workDoneProgress/create", "workspace/diagnostic/refresh":
		return reply(ctx, nil, nil)
	}

	// Server→client requests must be answered or the server blocks; reply null.
	// Notifications (no ID) are simply ignored.
	if _, ok := req.(*jsonrpc2.Call); ok {
		return reply(ctx, nil, nil)
	}
	return nil
}

// onPublishDiagnostics records a push notification and its publish time (for the
// debounce wait). Ports client.ts:191-208 (without the typescript first-push
// seed: the document/full waits converge for gopls/pyright via the pull path).
func (c *Client) onPublishDiagnostics(p protocol.PublishDiagnosticsParams) {
	fp := filePathOf(string(p.URI))
	if fp == "" {
		return
	}
	c.mu.Lock()
	c.pushDiags[fp] = p.Diagnostics
	c.published[fp] = time.Now()
	c.signalPush()
	c.mu.Unlock()
}

// configurationResult answers a workspace/configuration request: one entry per
// requested item, resolved from the server's initialization settings by the
// item's dotted section. Ports client.ts:213-216 + configurationValue:125-132.
func (c *Client) configurationResult(raw json.RawMessage) []any {
	var params struct {
		Items []struct {
			Section string `json:"section"`
		} `json:"items"`
	}
	_ = json.Unmarshal(raw, &params)
	out := make([]any, 0, len(params.Items))
	for _, item := range params.Items {
		out = append(out, configurationValue(c.initialization, item.Section))
	}
	return out
}

// configurationValue walks settings by a dotted section path, returning nil when
// absent (client.ts:125-132).
func configurationValue(settings any, section string) any {
	if section == "" {
		if settings == nil {
			return nil
		}
		return settings
	}
	cur := settings
	for _, key := range strings.Split(section, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		next, ok := m[key]
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// onRegisterCapability records dynamically-registered textDocument/diagnostic
// capabilities (client.ts:217-226).
func (c *Client) onRegisterCapability(raw json.RawMessage) {
	var params struct {
		Registrations []struct {
			ID              string `json:"id"`
			Method          string `json:"method"`
			RegisterOptions struct {
				Identifier           string `json:"identifier"`
				WorkspaceDiagnostics bool   `json:"workspaceDiagnostics"`
			} `json:"registerOptions"`
		} `json:"registrations"`
	}
	_ = json.Unmarshal(raw, &params)
	changed := false
	c.mu.Lock()
	for _, r := range params.Registrations {
		if r.Method != "textDocument/diagnostic" {
			continue
		}
		c.diagRegs[r.ID] = diagRegistration{
			id:                   r.ID,
			identifier:           r.RegisterOptions.Identifier,
			workspaceDiagnostics: r.RegisterOptions.WorkspaceDiagnostics,
		}
		changed = true
	}
	if changed {
		c.signalReg()
	}
	c.mu.Unlock()
}

// onUnregisterCapability drops dynamically-registered diagnostic capabilities
// (client.ts:227-236).
func (c *Client) onUnregisterCapability(raw json.RawMessage) {
	var params struct {
		Unregisterations []struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		} `json:"unregisterations"`
	}
	_ = json.Unmarshal(raw, &params)
	changed := false
	c.mu.Lock()
	for _, r := range params.Unregisterations {
		if r.Method != "textDocument/diagnostic" {
			continue
		}
		if _, ok := c.diagRegs[r.ID]; ok {
			delete(c.diagRegs, r.ID)
			changed = true
		}
	}
	if changed {
		c.signalReg()
	}
	c.mu.Unlock()
}

// signalPush / signalReg broadcast to any waiters by closing and replacing the
// signal channel. Callers must hold c.mu.
func (c *Client) signalPush() {
	close(c.pushSignal)
	c.pushSignal = make(chan struct{})
}

func (c *Client) signalReg() {
	close(c.regSignal)
	c.regSignal = make(chan struct{})
}

// Open notifies the server that file is opened (or changed if already open) and
// returns the new document version. Ports client.ts notify.open:595-669.
func (c *Client) Open(ctx context.Context, file string) (int32, error) {
	abs := c.resolve(file)
	text, err := os.ReadFile(abs) //nolint:gosec // path is within the instance directory
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", abs, err)
	}
	languageID := languageIDFor(abs)
	fileURI := string(uri.File(abs))

	c.mu.Lock()
	doc, open := c.files[abs]
	c.mu.Unlock()

	if open {
		// didChangeWatchedFiles(changed) + textDocument/didChange. Cached
		// diagnostics are NOT cleared here (client.ts:604-608: some servers only
		// re-emit on a real content change).
		if err := c.conn.Notify(ctx, "workspace/didChangeWatchedFiles", map[string]any{
			"changes": []map[string]any{{"uri": fileURI, "type": fileChangeChanged}},
		}); err != nil {
			return 0, err
		}
		next := doc.version + 1
		var changes []map[string]any
		if c.syncKind == textDocumentSyncIncrement {
			changes = []map[string]any{{
				"range": map[string]any{
					"start": map[string]any{"line": 0, "character": 0},
					"end":   endPosition(doc.text),
				},
				"text": string(text),
			}}
		} else {
			changes = []map[string]any{{"text": string(text)}}
		}
		if err := c.conn.Notify(ctx, "textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": fileURI, "version": next},
			"contentChanges": changes,
		}); err != nil {
			return 0, err
		}
		c.mu.Lock()
		c.files[abs] = fileState{version: next, text: string(text)}
		c.mu.Unlock()
		return next, nil
	}

	if err := c.conn.Notify(ctx, "workspace/didChangeWatchedFiles", map[string]any{
		"changes": []map[string]any{{"uri": fileURI, "type": fileChangeCreated}},
	}); err != nil {
		return 0, err
	}
	c.mu.Lock()
	delete(c.pushDiags, abs)
	delete(c.pullDiags, abs)
	c.mu.Unlock()
	if err := c.conn.Notify(ctx, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        fileURI,
			"languageId": languageID,
			"version":    0,
			"text":       string(text),
		},
	}); err != nil {
		return 0, err
	}
	c.mu.Lock()
	c.files[abs] = fileState{version: 0, text: string(text)}
	c.mu.Unlock()
	return 0, nil
}

// WaitForDiagnostics blocks until diagnostics for file converge or the mode's
// timeout elapses. Ports client.ts waitForDocumentDiagnostics:540-560 /
// waitForFullDiagnostics:562-582 (simplified: race a fresh-push wait against a
// pull attempt, looping until matched or timeout).
func (c *Client) WaitForDiagnostics(ctx context.Context, file string, mode DiagnosticsMode) {
	abs := c.resolve(file)
	deadline := diagnosticsDocumentWait
	if mode == DiagModeFull {
		deadline = diagnosticsFullWait
	}
	started := time.Now()
	for time.Since(started) < deadline {
		var matched bool
		if mode == DiagModeFull {
			matched = c.requestFullDiagnostics(ctx, abs)
		} else {
			matched = c.requestDocumentDiagnostics(ctx, abs)
		}
		if matched {
			return
		}
		remaining := deadline - time.Since(started)
		if remaining <= 0 {
			return
		}
		// Race a fresh push against a diagnostic-registration change. Only a
		// registration change re-loops (it can enable a previously-unsupported
		// pull); a fresh push has already populated pushDiags, and a bare timeout
		// stops. This mirrors opencode's `next !== "registration"` return
		// (client.ts:558,580) and — critically — avoids re-looping on the same
		// stale push, which would busy-spin for a push-only server until deadline.
		if c.waitForChange(ctx, abs, started, remaining) != changeRegistration {
			return
		}
	}
}

// diagChange is the outcome of waiting for fresh diagnostics during a touch.
type diagChange int

const (
	changeTimeout diagChange = iota
	changePush
	changeRegistration
)

// waitForChange waits until either a fresh push (newer than 'after', after the
// debounce) arrives for file, or a diagnostic capability (un)registration
// happens, or timeout. It returns which occurred. Ports the Promise.race of
// pushWait vs waitForRegistrationChange (client.ts:554-557).
func (c *Client) waitForChange(ctx context.Context, file string, after time.Time, timeout time.Duration) diagChange {
	if timeout <= 0 {
		return changeTimeout
	}
	pushCh := make(chan struct{}, 1)
	regCh := make(chan struct{}, 1)
	go func() {
		if c.waitForFreshPush(ctx, file, after, timeout) {
			pushCh <- struct{}{}
		}
	}()
	go func() {
		if c.waitForRegistrationChange(ctx, timeout) {
			regCh <- struct{}{}
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-pushCh:
		return changePush
	case <-regCh:
		return changeRegistration
	case <-timer.C:
		return changeTimeout
	case <-ctx.Done():
		return changeTimeout
	}
}

// waitForFreshPush waits until file's recorded push is at/after 'after' and the
// debounce window has elapsed, or until the timeout. Returns true if a fresh
// push arrived. Ports waitForFreshPush:503-538.
func (c *Client) waitForFreshPush(ctx context.Context, file string, after time.Time, timeout time.Duration) bool {
	if timeout <= 0 {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		c.mu.Lock()
		at, ok := c.published[file]
		sig := c.pushSignal
		c.mu.Unlock()
		if ok && !at.Before(after) {
			// Debounce: settle DIAGNOSTICS_DEBOUNCE_MS after the publish.
			wait := diagnosticsDebounce - time.Since(at)
			if wait <= 0 {
				return true
			}
			db := time.NewTimer(wait)
			select {
			case <-db.C:
				return true
			case <-timer.C:
				db.Stop()
				return false
			case <-ctx.Done():
				db.Stop()
				return false
			}
		}
		select {
		case <-sig:
		case <-timer.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

// waitForRegistrationChange waits up to timeout for a diagnostic capability
// (un)registration. Ports waitForRegistrationChange:485-501.
func (c *Client) waitForRegistrationChange(ctx context.Context, timeout time.Duration) bool {
	if timeout <= 0 {
		return false
	}
	c.mu.Lock()
	sig := c.regSignal
	c.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-sig:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// documentPullState reports whether document pull diagnostics are supported and
// the registered document identifiers. Ports documentPullState:394-404.
func (c *Client) documentPullState() (identifiers []string, supported bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]bool{}
	for _, r := range c.diagRegs {
		if r.workspaceDiagnostics {
			continue
		}
		if r.identifier != "" && !seen[r.identifier] {
			seen[r.identifier] = true
			identifiers = append(identifiers, r.identifier)
		}
	}
	supported = c.hasStaticPullDiagnostics || c.hasDocumentRegistrations()
	sort.Strings(identifiers)
	return identifiers, supported
}

// hasDocumentRegistrations reports whether any non-workspace diagnostic
// registration exists. Caller holds c.mu.
func (c *Client) hasDocumentRegistrations() bool {
	for _, r := range c.diagRegs {
		if !r.workspaceDiagnostics {
			return true
		}
	}
	return false
}

// workspacePullState reports whether workspace pull diagnostics are supported
// and the registered workspace identifiers. Ports workspacePullState:406-416.
func (c *Client) workspacePullState() (identifiers []string, supported bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := map[string]bool{}
	for _, r := range c.diagRegs {
		if !r.workspaceDiagnostics {
			continue
		}
		supported = true
		if r.identifier != "" && !seen[r.identifier] {
			seen[r.identifier] = true
			identifiers = append(identifiers, r.identifier)
		}
	}
	sort.Strings(identifiers)
	return identifiers, supported
}

// requestDocumentDiagnostics pulls document diagnostics for file (default +
// per-identifier) and merges them, returning whether file got non-empty
// diagnostics. Ports requestDocumentDiagnostics:455-466.
func (c *Client) requestDocumentDiagnostics(ctx context.Context, file string) bool {
	identifiers, supported := c.documentPullState()
	if !supported {
		return false
	}
	matched := c.pullDocument(ctx, file, "")
	for _, id := range identifiers {
		if c.pullDocument(ctx, file, id) {
			matched = true
		}
	}
	return matched
}

// requestFullDiagnostics pulls document and workspace diagnostics. Returns true
// if anything was handled. Ports requestFullDiagnostics:468-483.
func (c *Client) requestFullDiagnostics(ctx context.Context, file string) bool {
	docIDs, docSupported := c.documentPullState()
	wsIDs, wsSupported := c.workspacePullState()
	if !docSupported && !wsSupported {
		return false
	}
	handled := false
	if docSupported {
		if c.pullDocument(ctx, file, "") {
			handled = true
		}
	}
	for _, id := range docIDs {
		if c.pullDocument(ctx, file, id) {
			handled = true
		}
	}
	if wsSupported {
		if c.pullWorkspace(ctx, file, "") {
			handled = true
		}
	}
	for _, id := range wsIDs {
		if c.pullWorkspace(ctx, file, id) {
			handled = true
		}
	}
	return handled
}

// pullDocument issues a textDocument/diagnostic request and stores the result,
// returning whether file itself got diagnostics. Ports
// requestDiagnosticReport:332-366.
func (c *Client) pullDocument(ctx context.Context, file, identifier string) bool {
	rctx, cancel := context.WithTimeout(ctx, diagnosticsRequestWait)
	defer cancel()
	params := map[string]any{
		"textDocument": map[string]any{"uri": string(uri.File(file))},
	}
	if identifier != "" {
		params["identifier"] = identifier
	}
	var report struct {
		Items            []protocol.Diagnostic `json:"items"`
		RelatedDocuments map[string]struct {
			Items []protocol.Diagnostic `json:"items"`
		} `json:"relatedDocuments"`
	}
	if _, err := c.conn.Call(rctx, "textDocument/diagnostic", params, &report); err != nil {
		return false
	}
	matched := false
	if report.Items != nil {
		c.storePull(file, report.Items)
		matched = true
	}
	for u, related := range report.RelatedDocuments {
		rp := filePathOf(u)
		if rp == "" || related.Items == nil {
			continue
		}
		c.storePull(rp, related.Items)
		if rp == file {
			matched = true
		}
	}
	return matched
}

// pullWorkspace issues a workspace/diagnostic request and stores per-file
// results, returning whether file itself got diagnostics. Ports
// requestWorkspaceDiagnosticReport:368-392.
func (c *Client) pullWorkspace(ctx context.Context, file, identifier string) bool {
	rctx, cancel := context.WithTimeout(ctx, diagnosticsRequestWait)
	defer cancel()
	params := map[string]any{"previousResultIds": []any{}}
	if identifier != "" {
		params["identifier"] = identifier
	}
	var report struct {
		Items []struct {
			URI   string                `json:"uri"`
			Items []protocol.Diagnostic `json:"items"`
		} `json:"items"`
	}
	if _, err := c.conn.Call(rctx, "workspace/diagnostic", params, &report); err != nil {
		return false
	}
	matched := false
	for _, item := range report.Items {
		rp := filePathOf(item.URI)
		if rp == "" || item.Items == nil {
			continue
		}
		c.storePull(rp, item.Items)
		if rp == file {
			matched = true
		}
	}
	return matched
}

// storePull dedups and records pull diagnostics for a file.
func (c *Client) storePull(file string, diags []protocol.Diagnostic) {
	c.mu.Lock()
	c.pullDiags[file] = dedupeDiagnostics(diags)
	c.mu.Unlock()
}

// Diagnostics returns the merged (push+pull, deduped) diagnostics per file.
// Ports the diagnostics getter:671-677.
func (c *Client) Diagnostics() map[string][]protocol.Diagnostic {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := map[string]bool{}
	for k := range c.pushDiags {
		keys[k] = true
	}
	for k := range c.pullDiags {
		keys[k] = true
	}
	out := make(map[string][]protocol.Diagnostic, len(keys))
	for k := range keys {
		merged := append([]protocol.Diagnostic{}, c.pushDiags[k]...)
		merged = append(merged, c.pullDiags[k]...)
		out[k] = dedupeDiagnostics(merged)
	}
	return out
}

// Shutdown closes the connection and the held process pipes. The process group
// is reaped by the Service (killGroup); here we just release the JSON-RPC side.
func (c *Client) Shutdown() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	if c.proc != nil {
		_ = c.proc.Close()
	}
}

// resolve makes file absolute against the instance directory and cleans it.
func (c *Client) resolve(file string) string {
	if !filepath.IsAbs(file) {
		file = filepath.Join(c.directory, file)
	}
	return filepath.Clean(file)
}

// dedupeDiagnostics removes duplicates keyed by {code, severity, message,
// source, range}. Ports dedupeDiagnostics:109-123.
func dedupeDiagnostics(items []protocol.Diagnostic) []protocol.Diagnostic {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]bool, len(items))
	out := make([]protocol.Diagnostic, 0, len(items))
	for _, item := range items {
		key, _ := json.Marshal(struct {
			Code     any                         `json:"code"`
			Severity protocol.DiagnosticSeverity `json:"severity"`
			Message  string                      `json:"message"`
			Source   string                      `json:"source"`
			Range    protocol.Range              `json:"range"`
		}{item.Code, item.Severity, item.Message, item.Source, item.Range})
		if seen[string(key)] {
			continue
		}
		seen[string(key)] = true
		out = append(out, item)
	}
	return out
}

// endPosition returns the {line, character} at the end of text, for incremental
// full-range replacement. Ports endPosition:101-107.
func endPosition(text string) map[string]any {
	lines := splitLines(text)
	last := ""
	if len(lines) > 0 {
		last = lines[len(lines)-1]
	}
	return map[string]any{"line": len(lines) - 1, "character": len([]rune(last))}
}

// splitLines splits on \r\n, \r, or \n (matching JS text.split(/\r\n|\r|\n/)).
func splitLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.Split(normalized, "\n")
}

// filePathOf converts a file:// URI to a cleaned local path, or "" for non-file
// URIs. Ports getFilePath:89-92.
func filePathOf(u string) string {
	if !strings.HasPrefix(u, "file://") {
		return ""
	}
	return filepath.Clean(uri.New(u).Filename())
}
