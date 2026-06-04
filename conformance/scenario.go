package conformance

import (
	"net/http"
	"time"

	"github.com/rotemmiz/forge/conformance/result"
)

// Scenario is one named sequence of interactions. Run records the observable
// steps; it does NOT assert opencode-specific values — the diff against the
// recorded truth is what decides conformance.
type Scenario struct {
	Name string
	Run  func(c *Client) ([]result.Step, error)
}

// Scenarios is the Phase-A, agent-free set (no LLM provider needed). Prompt,
// tool, permission, question, and MCP scenarios are Phase B (need plan 02).
var Scenarios = []Scenario{
	{Name: "session-create-list", Run: scenarioSessionCreateList},
	{Name: "session-get-delete", Run: scenarioSessionGetDelete},
	{Name: "session-fork-children", Run: scenarioSessionForkChildren},
	{Name: "config-get", Run: scenarioConfigGet},
	{Name: "provider-list", Run: scenarioProviderList},
	{Name: "agent-list", Run: scenarioAgentList},
	// GET /command IS a parity scenario, compared order-insensitively: opencode
	// returns the command list in a non-deterministic (map/glob) order while Forge
	// sorts by name (masterplan decision #6, a recorded known-addition). The
	// harness set-normalizes /command (orderInsensitiveListPath in client.go), so
	// two runs' identical command SET compares equal regardless of order while a
	// genuinely missing/extra command still fails. (forge-vs-opencode additionally
	// differs because opencode surfaces built-in/MCP/skill commands Forge doesn't —
	// that remains the `command-list` known-divergence, separate from ordering.)
	{Name: "command-list", Run: scenarioCommandList},
	{Name: "session-todo-empty", Run: scenarioSessionTodo},
	{Name: "session-message-list", Run: scenarioSessionMessageList},
	// The reply/reject endpoints' happy path needs a pending request (a running
	// tool), which isn't deterministic in a stateless scenario. Their error path
	// is: a missing request returns 404 {_tag, requestID, message}. Cover that as
	// the parity gate for the interactive endpoints (plan 08 U10).
	{Name: "permission-reply-unknown", Run: scenarioPermissionReplyUnknown},
	{Name: "question-reply-unknown", Run: scenarioQuestionReplyUnknown},
	{Name: "question-reject-unknown", Run: scenarioQuestionRejectUnknown},
	{Name: "sse-instance-connected", Run: scenarioSSEInstanceConnected},
	{Name: "sse-global-connected", Run: scenarioSSEGlobalConnected},
	{Name: "auth-basic-ok", Run: scenarioAuthBasicOK},
	{Name: "auth-missing-401", Run: scenarioAuthMissing},
	{Name: "auth-token-query", Run: scenarioAuthTokenQuery},
}

type createdSession struct {
	ID string `json:"id"`
}

// do performs one request and appends its step, or returns the error.
func do(c *Client, steps *[]result.Step, name, method, path string, body any) error {
	s, err := c.Do(name, method, path, body)
	if err != nil {
		return err
	}
	*steps = append(*steps, s)
	return nil
}

func scenarioSessionCreateList(c *Client) ([]result.Step, error) {
	var steps []result.Step
	if err := do(c, &steps, "create", http.MethodPost, "/session", map[string]any{}); err != nil {
		return steps, err
	}
	if err := do(c, &steps, "list", http.MethodGet, "/session", nil); err != nil {
		return steps, err
	}
	return steps, nil
}

func scenarioSessionGetDelete(c *Client) ([]result.Step, error) {
	var steps []result.Step
	if err := do(c, &steps, "create", http.MethodPost, "/session", map[string]any{}); err != nil {
		return steps, err
	}
	var cs createdSession
	if err := c.LastJSON(&cs); err != nil {
		return steps, err
	}
	if err := do(c, &steps, "get", http.MethodGet, "/session/"+cs.ID, nil); err != nil {
		return steps, err
	}
	if err := do(c, &steps, "delete", http.MethodDelete, "/session/"+cs.ID, nil); err != nil {
		return steps, err
	}
	// After delete, get should 404 (the status is the conformance signal).
	if err := do(c, &steps, "get-after-delete", http.MethodGet, "/session/"+cs.ID, nil); err != nil {
		return steps, err
	}
	return steps, nil
}

func scenarioSessionForkChildren(c *Client) ([]result.Step, error) {
	var steps []result.Step
	if err := do(c, &steps, "create", http.MethodPost, "/session", map[string]any{}); err != nil {
		return steps, err
	}
	var cs createdSession
	if err := c.LastJSON(&cs); err != nil {
		return steps, err
	}
	if err := do(c, &steps, "fork", http.MethodPost, "/session/"+cs.ID+"/fork", map[string]any{}); err != nil {
		return steps, err
	}
	if err := do(c, &steps, "children", http.MethodGet, "/session/"+cs.ID+"/children", nil); err != nil {
		return steps, err
	}
	return steps, nil
}

func scenarioConfigGet(c *Client) ([]result.Step, error) {
	s, err := c.Do("config", http.MethodGet, "/config", nil)
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

func scenarioProviderList(c *Client) ([]result.Step, error) {
	s, err := c.Do("provider", http.MethodGet, "/provider", nil)
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// The TUI reads these to populate the agent/command switchers; cover them so the
// parity gate includes the TUI's read surface (plan 08 U8/U9).
func scenarioAgentList(c *Client) ([]result.Step, error) {
	s, err := c.Do("agent", http.MethodGet, "/agent", nil)
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// scenarioCommandList reads the slash-command list (the TUI's `/` switcher and
// autocomplete source, plan 08 U9). The body is set-normalized in client.go so
// opencode's non-deterministic order and Forge's sorted order both compare equal
// (masterplan decision #6).
func scenarioCommandList(c *Client) ([]result.Step, error) {
	s, err := c.Do("command", http.MethodGet, "/command", nil)
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// scenarioSessionTodo creates a session and reads its (empty) todo list — the
// tasks dock source (plan 08 U11).
func scenarioSessionTodo(c *Client) ([]result.Step, error) {
	var steps []result.Step
	if err := do(c, &steps, "create", http.MethodPost, "/session", map[string]any{}); err != nil {
		return steps, err
	}
	var cs createdSession
	if err := c.LastJSON(&cs); err != nil {
		return steps, err
	}
	if err := do(c, &steps, "todo", http.MethodGet, "/session/"+cs.ID+"/todo", nil); err != nil {
		return steps, err
	}
	return steps, nil
}

// scenarioPermissionReplyUnknown replies to a non-existent permission request;
// both daemons return 404 with {_tag, requestID, message}.
func scenarioPermissionReplyUnknown(c *Client) ([]result.Step, error) {
	s, err := c.Do("reply-unknown", http.MethodPost, "/permission/per_000/reply",
		map[string]any{"reply": "once"})
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// scenarioQuestionReplyUnknown replies to a non-existent question request.
func scenarioQuestionReplyUnknown(c *Client) ([]result.Step, error) {
	s, err := c.Do("reply-unknown", http.MethodPost, "/question/que_000/reply",
		map[string]any{"answers": [][]string{}})
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// scenarioQuestionRejectUnknown rejects a non-existent question request.
func scenarioQuestionRejectUnknown(c *Client) ([]result.Step, error) {
	s, err := c.Do("reject-unknown", http.MethodPost, "/question/que_000/reject", nil)
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// scenarioSessionMessageList creates a session and reads its (empty) message
// history — the conversation stream's bootstrap (plan 08 U3).
func scenarioSessionMessageList(c *Client) ([]result.Step, error) {
	var steps []result.Step
	if err := do(c, &steps, "create", http.MethodPost, "/session", map[string]any{}); err != nil {
		return steps, err
	}
	var cs createdSession
	if err := c.LastJSON(&cs); err != nil {
		return steps, err
	}
	if err := do(c, &steps, "messages", http.MethodGet, "/session/"+cs.ID+"/message", nil); err != nil {
		return steps, err
	}
	return steps, nil
}

// scenarioSSEInstanceConnected captures the first event from the instance SSE
// stream, which is a BARE {id,type,properties} (Finding #2).
func scenarioSSEInstanceConnected(c *Client) ([]result.Step, error) {
	s, err := c.SSE("event", "/event", 5*time.Second, 1)
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// scenarioSSEGlobalConnected captures the first event from the global SSE
// stream, which is WRAPPED as {payload:{id,type,properties},...} (Finding #2).
func scenarioSSEGlobalConnected(c *Client) ([]result.Step, error) {
	s, err := c.SSE("global-event", "/global/event", 5*time.Second, 1)
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// --- Auth (plan 12 #20-22; authorization.ts:9,11,82-86). These run against an
// auth-enabled daemon (the runner sets OPENCODE_SERVER_PASSWORD + client creds). ---

// scenarioAuthBasicOK: a request with valid Basic credentials succeeds (200).
func scenarioAuthBasicOK(c *Client) ([]result.Step, error) {
	s, err := c.Probe("basic", http.MethodGet, "/config", ReqOpts{Auth: AuthDefault})
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// scenarioAuthMissing: no credentials → 401 with WWW-Authenticate: Basic
// realm="Secure Area" (authorization.ts:11,53). The captured header is the signal.
func scenarioAuthMissing(c *Client) ([]result.Step, error) {
	s, err := c.Probe("no-auth", http.MethodGet, "/config",
		ReqOpts{Auth: AuthNone, Capture: []string{"Www-Authenticate"}})
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// scenarioAuthTokenQuery: ?auth_token=base64(user:pass) is equivalent to Basic
// (authorization.ts:9,82-84) → 200.
func scenarioAuthTokenQuery(c *Client) ([]result.Step, error) {
	s, err := c.Probe("token", http.MethodGet, "/config", ReqOpts{Auth: AuthToken})
	if err != nil {
		return nil, err
	}
	return []result.Step{s}, nil
}

// Directory-routing scenarios (#23-25) are deferred: opencode 1.15.x's session
// listing relative to the x-opencode-directory header vs the ?directory query is
// non-obvious (GET /session appears to return a global/accumulating list, and
// header/query filtering behaved inconsistently across probes). The Client
// supports DirHeader/DirQuery/DirNone so the scenarios can be added once the
// routing semantics are pinned down. See tasks/verify.md.
