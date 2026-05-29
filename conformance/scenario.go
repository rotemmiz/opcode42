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
	{Name: "sse-instance-connected", Run: scenarioSSEInstanceConnected},
	{Name: "sse-global-connected", Run: scenarioSSEGlobalConnected},
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
