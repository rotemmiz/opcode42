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

	"github.com/rotemmiz/forge/conformance/result"
)

// LiveModel is the model every live scenario is pinned to. LLM output is
// non-deterministic, so the live dual-run asserts SHAPE (response field
// presence/types + the SSE event-type sequence), not exact text — see the
// live normalizer (normalize.NewLive). gemini-2.5-flash is fast, cheap, and
// both daemons reach it the same way (Forge via the google OpenAI-compatible
// endpoint; see cmd/forged builtinBaseURL).
const (
	LiveProvider = "google"
	LiveModel    = "gemini-2.5-flash"
)

// liveEventCatalog is the set of SSE event TYPES the live dual-run compares. It
// is intentionally the INTERSECTION of what both daemons emit during a prompt —
// the core message/part/permission lifecycle that Forge and opencode share. It
// deliberately excludes opencode's richer session.next.* telemetry events
// (session.next.step.started, .text.started, .tool.called, …), which Forge does
// not emit: the authoritative cross-daemon SSE catalog is an open contract
// question (tasks/progress.md "Ambiguities" #2), so a full-stream equality diff
// is not yet a valid gate. Capturing only the shared catalog keeps the live diff
// honest — it asserts the lifecycle both daemons agree on without failing on the
// telemetry gap. Expand this set (and re-baseline) once #2 is resolved.
var liveEventCatalog = map[string]bool{
	"message.updated":      true,
	"message.part.updated": true,
	"permission.asked":     true,
	"permission.replied":   true,
	"question.asked":       true,
	"question.replied":     true,
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
	// ask, so a concurrent watcher resolves the requestID from the ask event and
	// replies "once" to unblock the run. The watcher is self-bounded (4-minute
	// stream timeout) so it cannot outlive the run.
	go grantPending(c)

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

// grantPending watches the tap for a permission.asked event and replies "once".
// The requestID is read from the raw ask event via a second short-lived stream
// read; if no ask arrives the goroutine exits when the scenario closes the tap.
func grantPending(c *Client) {
	// Open a dedicated raw stream so we can read the permission requestID from the
	// ask event's properties (the tap only keeps types).
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	req, err := c.buildRequest(ctx, http.MethodGet, "/event", ReqOpts{})
	if err != nil {
		return
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
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
	dir, err := os.MkdirTemp("", "forge-live-")
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
// /rate-limit/quota/5xx error rather than a Forge-vs-opencode behavior gap.
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
