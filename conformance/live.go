package conformance

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rotemmiz/opcode42/conformance/result"
)

// LiveModel is the model every live scenario is pinned to. LLM output is
// non-deterministic, so the live dual-run asserts SHAPE (response field
// presence/types + the SSE event-type sequence), not exact text — see the
// live normalizer (normalize.NewLive). gemini-2.5-flash is fast, cheap, and
// both daemons reach it the same way (Opcode42 via the google OpenAI-compatible
// endpoint; see cmd/opcoded builtinBaseURL).
const (
	LiveProvider = "google"
	LiveModel    = "gemini-2.5-flash"
)

// liveEventCatalog is the set of SSE event TYPES the live dual-run compares. It
// is the SHARED lifecycle catalog — the subset of opencode's authoritative SSE
// event catalog (enumerated from openapi.json's Event union; see
// TestLiveEventCatalogIsAuthoritative in catalog_test.go) that Opcode42's engine
// also emits for the core agent flow. The cross-daemon diff asserts these event
// kinds appear in the same order on both daemons.
//
// Ambiguity #2 ("cassettes vs masterplan list") is RESOLVED: opencode source is
// authoritative (the openapi Event union, generated from the Bus/sync event
// definitions). This set is gated against that catalog — every entry here must
// be a real opencode event type, enforced by the catalog test. It deliberately
// EXCLUDES opencode's experimental session.next.* telemetry stream
// (session-event.ts; gated behind opencode's experimentalEventSystem flag and
// not part of the stable wire contract Opcode42 targets), which Opcode42 does not
// emit. Those are tracked as a known, intentional divergence (catalog_test.go +
// the live divergence registry), not faked.
//
// session.status is INTENTIONALLY omitted from the ordering diff even though
// Opcode42 now emits it (and the catalog test asserts it is authoritative): opencode
// re-publishes session.status{busy} at the TOP of every loop iteration
// (prompt.ts:1253, status.ts:77 publishes unconditionally), so on a multi-step
// run it appears repeatedly, NON-consecutively, interleaved with message events.
// The tap collapses only CONSECUTIVE duplicates, so gating session.status would
// make ordering diff on every tool-call turn count — a harness artifact, not a
// wire divergence. session.idle (terminal, fires once) and session.compacted
// (fires once on summarize) are the safe, deterministic lifecycle markers to gate.
var liveEventCatalog = map[string]bool{
	"session.idle":         true,
	"message.updated":      true,
	"message.part.updated": true,
	"permission.asked":     true,
	"permission.replied":   true,
	"question.asked":       true,
	"question.replied":     true,
	"session.compacted":    true,
}

// LiveScenario is one named live-prompt interaction. Like Scenario it records
// observable steps; the dual-run diff decides conformance.
type LiveScenario struct {
	Name string
	Run  func(c *Client) ([]result.Step, error)
}

// LiveScenarios is the agent-driven set (plan 12 §c "Prompt and SSE event
// stream" / "Permission round-trip", plan 02 M11). Each needs a real provider
// key, so they only run under the live dual-run gate (TestLiveSuite skips
// without -target; run-conformance.sh live injects the key into both daemons).
var LiveScenarios = []LiveScenario{
	{Name: "live-prompt-text", Run: liveScenarioPromptText},
	{Name: "live-tool-call", Run: liveScenarioToolCall},
	{Name: "live-permission-grant", Run: liveScenarioPermissionGrant},
	{Name: "live-compaction", Run: liveScenarioCompaction},
	{Name: "live-abort", Run: liveScenarioAbort},
}

// eventTap subscribes to a daemon's instance SSE stream and records the ordered
// sequence of event-type strings (filtered to liveEventCatalog, with consecutive
// duplicates collapsed). It runs for the lifetime of a scenario so events
// emitted during a synchronous prompt are captured.
type eventTap struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	types  []string
	done   chan struct{}
}

// openEventTap connects to GET /event and starts recording. Caller must Close it.
func (c *Client) openEventTap() (*eventTap, error) {
	ctx, cancel := context.WithCancel(context.Background())
	req, err := c.buildRequest(ctx, http.MethodGet, "/event", ReqOpts{})
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := c.HTTP.Do(req) //nolint:bodyclose // resp.Body is closed by t.read's defer
	if err != nil {
		cancel()
		return nil, fmt.Errorf("event tap: %w", err)
	}
	t := &eventTap{cancel: cancel, done: make(chan struct{})}
	go t.read(resp.Body)
	return t, nil
}

// read consumes the SSE body, recording catalog event types until the stream
// ends (the scenario's Close cancels the request). It owns and closes the body.
func (t *eventTap) read(body io.ReadCloser) {
	defer close(t.done)
	defer func() { _ = body.Close() }()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		data, ok := strings.CutPrefix(strings.TrimRight(sc.Text(), "\r"), "data:")
		if !ok {
			continue
		}
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &ev) != nil {
			continue
		}
		if !liveEventCatalog[ev.Type] {
			continue
		}
		t.mu.Lock()
		if len(t.types) == 0 || t.types[len(t.types)-1] != ev.Type {
			t.types = append(t.types, ev.Type)
		}
		t.mu.Unlock()
	}
}

// Types returns a snapshot of the captured, de-duplicated event-type sequence.
func (t *eventTap) Types() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.types))
	copy(out, t.types)
	return out
}

// Close stops the tap and waits for the reader goroutine to drain.
func (t *eventTap) Close() {
	t.cancel()
	<-t.done
}

// promptBody builds the POST /session/:id/message request for the pinned model.
func promptBody(text string, extra map[string]any) map[string]any {
	b := map[string]any{
		"model": map[string]any{"providerID": LiveProvider, "modelID": LiveModel},
		"parts": []map[string]any{{"type": "text", "text": text}},
	}
	for k, v := range extra {
		b[k] = v
	}
	return b
}

// liveSession creates a session and returns its id, recording the create step.
func liveSession(c *Client, steps *[]result.Step) (string, error) {
	if err := do(c, steps, "create", http.MethodPost, "/session", map[string]any{}); err != nil {
		return "", err
	}
	var cs createdSession
	if err := c.LastJSON(&cs); err != nil {
		return "", err
	}
	return cs.ID, nil
}

// settleEvents gives the SSE stream a brief window to deliver any trailing
// events the synchronous prompt response may have raced ahead of (the bus
// publish and the HTTP return are not strictly ordered). Bounded so a stuck
// stream can never hang the scenario.
func settleEvents() { time.Sleep(500 * time.Millisecond) }

// liveScenarioPromptText: a text-only prompt. Asserts the response shape and the
// message/part lifecycle event sequence (plan 12 §c #5).
func liveScenarioPromptText(c *Client) ([]result.Step, error) {
	var steps []result.Step
	sid, err := liveSession(c, &steps)
	if err != nil {
		return steps, err
	}
	tap, err := c.openEventTap()
	if err != nil {
		return steps, err
	}
	defer tap.Close()

	step, err := c.Do("prompt", http.MethodPost, "/session/"+sid+"/message",
		promptBody("Reply with exactly the word: pong. No punctuation.", nil))
	if err != nil {
		return steps, err
	}
	settleEvents()
	step.EventTypes = tap.Types()
	steps = append(steps, step)
	return steps, nil
}

// liveScenarioToolCall: a prompt that forces one built-in tool round-trip (read
// a file we create in the project dir). Asserts the assistant message shape and
// that a tool part lifecycle appears (plan 12 §c #6). The build agent is
// allow-all so the tool runs without a permission ask.
func liveScenarioToolCall(c *Client) ([]result.Step, error) {
	var steps []result.Step
	sid, err := liveSession(c, &steps)
	if err != nil {
		return steps, err
	}
	tap, err := c.openEventTap()
	if err != nil {
		return steps, err
	}
	defer tap.Close()

	prompt := "Use the read tool to read the file named MARKER.txt in the current " +
		"directory, then reply with only the word it contains. Use the tool; do not guess."
	step, err := c.Do("prompt", http.MethodPost, "/session/"+sid+"/message", promptBody(prompt, nil))
	if err != nil {
		return steps, err
	}
	settleEvents()
	step.EventTypes = tap.Types()
	steps = append(steps, step)
	return steps, nil
}

// liveScenarioPermissionGrant: a prompt under a restrictive agent (so a tool
// call triggers a permission ask), then grant it and confirm the run completes
// (plan 12 §c #8, ask→grant). The ask is handled concurrently by a watcher that
// polls GET /permission... is not exposed; instead we drive the documented
// reply endpoint once the ask event is observed on the tap.
func liveScenarioPermissionGrant(c *Client) ([]result.Step, error) {
	var steps []result.Step
	sid, err := liveSession(c, &steps)
	if err != nil {
		return steps, err
	}
	tap, err := c.openEventTap()
	if err != nil {
		return steps, err
	}
	defer tap.Close()

	// Grant any permission this run raises. The synchronous prompt blocks on the
	// ask, so a concurrent watcher (on its OWN client — never the shared one, to
	// avoid racing Client.lastBody) resolves the requestID from the ask event and
	// replies "once" to unblock the run. The bus has NO event backlog, so the
	// watcher must be CONNECTED before the prompt fires or it could miss the ask;
	// startGranter blocks until the SSE stream is established. grantCtx is
	// cancelled on return so the watcher cannot outlive the scenario.
	grantCtx, cancelGrant := context.WithCancel(context.Background())
	defer cancelGrant()
	if err := startGranter(grantCtx, c.grantClient()); err != nil {
		return steps, err
	}

	prompt := "Run the bash command: echo hello. Use the bash tool."
	step, err := c.Do("prompt", http.MethodPost, "/session/"+sid+"/message",
		promptBody(prompt, map[string]any{"agent": "asker"}))
	if err != nil {
		return steps, err
	}
	settleEvents()
	step.EventTypes = tap.Types()
	steps = append(steps, step)
	return steps, nil
}

// grantClient returns an independent client for the same target/dir/auth, so a
// concurrent helper (grantPending) shares no mutable state (notably lastBody)
// with the scenario's client.
func (c *Client) grantClient() *Client {
	gc := &Client{BaseURL: c.BaseURL, Dir: c.Dir, User: c.User, Pass: c.Pass, Norm: c.Norm, HTTP: c.HTTP}
	return gc
}

// startGranter connects to the instance SSE stream (synchronously, so the caller
// can fire the prompt knowing the watcher will see the ask) and then watches, in
// a goroutine, for a permission.asked event to reply "once" to. It returns once
// connected; the goroutine exits on grant, ctx cancel, or stream end.
func startGranter(ctx context.Context, c *Client) error {
	req, err := c.buildRequest(ctx, http.MethodGet, "/event", ReqOpts{})
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req) //nolint:bodyclose // resp.Body is closed by grantWatch's defer
	if err != nil {
		return fmt.Errorf("granter connect: %w", err)
	}
	go grantWatch(c, resp.Body)
	return nil
}

// grantWatch consumes the SSE body and replies "once" to the first
// permission.asked. It owns and closes the body.
func grantWatch(c *Client, body io.ReadCloser) {
	defer func() { _ = body.Close() }()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		data, ok := strings.CutPrefix(strings.TrimRight(sc.Text(), "\r"), "data:")
		if !ok {
			continue
		}
		var ev struct {
			Type       string `json:"type"`
			Properties struct {
				ID string `json:"id"`
			} `json:"properties"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &ev) != nil {
			continue
		}
		if ev.Type == "permission.asked" && ev.Properties.ID != "" {
			_, _ = c.Do("grant", http.MethodPost, "/permission/"+ev.Properties.ID+"/reply",
				map[string]any{"reply": "once"})
			return
		}
	}
}

// liveScenarioCompaction: drive a compaction by POSTing the documented summarize
// trigger, then assert the response shape (plan 12 §c #d). opencode exposes
// compaction via a prompt with a compaction part / a summarize endpoint; both
// daemons emit a summary assistant message. We use a normal prompt then request
// summarization so the captured shape is comparable.
func liveScenarioCompaction(c *Client) ([]result.Step, error) {
	var steps []result.Step
	sid, err := liveSession(c, &steps)
	if err != nil {
		return steps, err
	}
	// Seed one turn so there is history to compact.
	if err := do(c, &steps, "seed", http.MethodPost, "/session/"+sid+"/message",
		promptBody("Reply with exactly: ok", nil)); err != nil {
		return steps, err
	}
	tap, err := c.openEventTap()
	if err != nil {
		return steps, err
	}
	defer tap.Close()
	step, err := c.Do("summarize", http.MethodPost, "/session/"+sid+"/summarize",
		map[string]any{"providerID": LiveProvider, "modelID": LiveModel})
	if err != nil {
		return steps, err
	}
	settleEvents()
	step.EventTypes = tap.Types()
	steps = append(steps, step)
	return steps, nil
}

// liveScenarioAbort: start an async prompt then immediately abort it; assert the
// abort endpoint's response shape and that the session settles (plan 12 §c #e).
func liveScenarioAbort(c *Client) ([]result.Step, error) {
	var steps []result.Step
	sid, err := liveSession(c, &steps)
	if err != nil {
		return steps, err
	}
	// prompt_async returns 204 immediately; the run continues in the background.
	if err := do(c, &steps, "prompt-async", http.MethodPost, "/session/"+sid+"/prompt_async",
		promptBody("Count slowly from 1 to 100, one number per line.", nil)); err != nil {
		return steps, err
	}
	// Abort right away; both daemons return the abort result.
	if err := do(c, &steps, "abort", http.MethodPost, "/session/"+sid+"/abort", nil); err != nil {
		return steps, err
	}
	return steps, nil
}

// RunLive executes the live scenarios against target and returns a result file.
func RunLive(target, user, pass string) (*result.File, error) {
	f := &result.File{Target: target}
	for _, sc := range LiveScenarios {
		f.Scenarios = append(f.Scenarios, runLiveScenario(target, user, pass, sc))
	}
	return f, nil
}

// runLiveScenario gives each scenario a fresh temp project dir, seeds it with the
// fixtures the live scenarios rely on — a deterministic MARKER.txt for the
// tool-call read and an "asker" agent (permission: {bash: ask}) so the
// permission round-trip triggers an ask on BOTH daemons (the built-in agents'
// rulesets are not parity-comparable; see the agent-list known-divergence) — and
// runs it against a live (model-output-masking) client.
func runLiveScenario(target, user, pass string, sc LiveScenario) result.Scenario {
	dir, err := os.MkdirTemp("", "opcode42-live-")
	if err != nil {
		return result.Scenario{Name: sc.Name, Steps: []result.Step{errStep("mkdtemp", err)}}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := seedLiveFixtures(dir); err != nil {
		return result.Scenario{Name: sc.Name, Steps: []result.Step{errStep("seed", err)}}
	}

	resolved, rerr := filepath.EvalSymlinks(dir)
	paths := []string{}
	if rerr == nil && resolved != dir {
		paths = append(paths, resolved)
	}

	c := NewLiveClient(target, dir, paths...)
	c.User, c.Pass = user, pass
	steps, runErr := sc.Run(c)
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "live scenario %s: %v\n", sc.Name, runErr)
		steps = append(steps, errStep("error", runErr))
	}
	// A transient PROVIDER error (rate limit / quota / upstream 5xx) on one daemon
	// but not the other is not a wire-conformance signal — it would otherwise
	// manifest as a cascade of false structural diffs (info.error present, parts
	// len 0, a short SSE stream). Mark such a run Skipped so the diff ignores it
	// (the differ skips a scenario flagged Skipped on either side). A genuine
	// assistant error the engine is supposed to model would carry a different,
	// non-provider error name and still be compared.
	if name := providerErrorName(steps); name != "" {
		fmt.Fprintf(os.Stderr, "live scenario %s: skipped — transient provider error %q\n", sc.Name, name)
		return result.Scenario{Name: sc.Name, Skipped: true, Steps: steps}
	}
	return result.Scenario{Name: sc.Name, Steps: steps}
}

// providerErrorName returns the assistant error name if any step's response body
// carries a transient upstream provider error (APIError / rate limit), else "".
func providerErrorName(steps []result.Step) string {
	for _, s := range steps {
		if s.Body == "" {
			continue
		}
		var b struct {
			Info struct {
				Error *struct {
					Name string `json:"name"`
					Data struct {
						Message string `json:"message"`
					} `json:"data"`
				} `json:"error"`
			} `json:"info"`
		}
		if json.Unmarshal([]byte(s.Body), &b) != nil || b.Info.Error == nil {
			continue
		}
		if isTransientProviderError(b.Info.Error.Name, b.Info.Error.Data.Message) {
			return b.Info.Error.Name
		}
	}
	return ""
}

// isTransientProviderError reports whether an assistant error is an upstream
// provider failure that should not count as a conformance divergence — a network
// /rate-limit/quota/5xx error rather than a Opcode42-vs-opencode behavior gap.
func isTransientProviderError(name, message string) bool {
	switch name {
	case "APIError", "ProviderError", "AI_APICallError":
		return true
	}
	for _, sig := range []string{"429", "quota", "rate limit", "rate_limit", "503", "overloaded", "RESOURCE_EXHAUSTED"} {
		if strings.Contains(strings.ToLower(message), strings.ToLower(sig)) {
			return true
		}
	}
	return false
}

// seedLiveFixtures writes the per-scenario project fixtures both daemons read.
func seedLiveFixtures(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "MARKER.txt"), []byte("kumquat\n"), 0o644); err != nil {
		return err
	}
	agentDir := filepath.Join(dir, ".opencode", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return err
	}
	const asker = "---\ndescription: asks before bash\nmode: primary\npermission:\n  bash: ask\n---\nYou are a helper. Use the bash tool when asked.\n"
	return os.WriteFile(filepath.Join(agentDir, "asker.md"), []byte(asker), 0o644)
}
