// Package runstate is the per-session run lock for the agent loop, mirroring
// opencode's SessionRunState (packages/opencode/src/session/run-state.ts).
//
// Each session has at most one active run. EnsureRunning is idempotent:
// concurrent prompts on the same session share the in-flight run's result rather
// than starting a second one. Cancel interrupts the run's context. assertNotBusy
// (Busy) lets shell-style callers fail fast with a BusyError instead of queueing.
package runstate

import (
	"context"
	"fmt"
	"sync"

	"github.com/rotemmiz/opcode42/internal/engine/message"
)

// BusyError reports that a session already has an active run (HTTP 409).
type BusyError struct{ SessionID string }

func (e *BusyError) Error() string { return fmt.Sprintf("session %s is busy", e.SessionID) }

// Work is the unit a run executes: the loop body, returning the final message.
type Work func(ctx context.Context) (message.WithParts, error)

// Hooks observe a run's busy/idle lifecycle. OnBusy fires once, when this run
// actually starts (not for coalesced callers that join an in-flight run); OnIdle
// fires once, when it completes. They mirror opencode's runner onBusy/onIdle
// (run-state.ts:58-63), which drive the session.status / session.idle SSE events.
// Both are optional.
type Hooks struct {
	OnBusy func()
	OnIdle func()
}

// RunState tracks active runs keyed by session id.
type RunState struct {
	mu     sync.Mutex
	active map[string]*sessionRun
}

type sessionRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	result message.WithParts
	err    error
}

// New returns an empty run state.
func New() *RunState { return &RunState{active: map[string]*sessionRun{}} }

// EnsureRunning runs work under the session's run lock. If a run is already
// active for the session, it waits for and returns that run's result instead of
// starting a new one (idempotent — concurrent prompts coalesce). Optional hooks
// observe the busy/idle transitions of the run this call actually starts;
// coalesced callers that join an in-flight run do not re-fire them.
func (rs *RunState) EnsureRunning(parent context.Context, sessionID string, work Work, hooks ...Hooks) (message.WithParts, error) {
	rs.mu.Lock()
	if run, ok := rs.active[sessionID]; ok {
		rs.mu.Unlock()
		<-run.done
		return run.result, run.err
	}
	ctx, cancel := context.WithCancel(parent)
	run := &sessionRun{cancel: cancel, done: make(chan struct{})}
	rs.active[sessionID] = run
	rs.mu.Unlock()

	var h Hooks
	if len(hooks) > 0 {
		h = hooks[0]
	}
	if h.OnBusy != nil {
		h.OnBusy()
	}

	go func() {
		defer close(run.done)
		defer cancel()
		// onIdle fires after the run completes and the slot is cleared, mirroring
		// opencode's runner.onIdle ordering (run-state.ts:59-61).
		if h.OnIdle != nil {
			defer h.OnIdle()
		}
		run.result, run.err = work(ctx)
		rs.mu.Lock()
		// Only clear if still ours (a later run could have replaced us after done).
		if rs.active[sessionID] == run {
			delete(rs.active, sessionID)
		}
		rs.mu.Unlock()
	}()

	<-run.done
	return run.result, run.err
}

// Cancel interrupts the session's active run, if any.
func (rs *RunState) Cancel(sessionID string) {
	rs.mu.Lock()
	run := rs.active[sessionID]
	rs.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// Busy reports whether the session has an active run.
func (rs *RunState) Busy(sessionID string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	_, ok := rs.active[sessionID]
	return ok
}

// AssertNotBusy returns a *BusyError if the session is already running.
func (rs *RunState) AssertNotBusy(sessionID string) error {
	if rs.Busy(sessionID) {
		return &BusyError{SessionID: sessionID}
	}
	return nil
}
