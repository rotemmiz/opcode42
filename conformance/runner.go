package conformance

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rotemmiz/opcode42/conformance/result"
)

// Run executes every scenario against the target daemon and returns a result
// file. user/pass are the Basic-auth credentials sent on every request (empty
// when the daemon is unauthenticated). Each scenario gets a fresh temp project
// directory (passed via x-opencode-directory) so per-directory state — e.g. the
// session list — is isolated between scenarios and between runs. The directory
// and its symlink-resolved form are normalized to <path>.
func Run(target, user, pass string) (*result.File, error) {
	f := &result.File{Target: target}
	for _, sc := range Scenarios {
		f.Scenarios = append(f.Scenarios, runScenario(target, user, pass, sc))
	}
	return f, nil
}

func runScenario(target, user, pass string, sc Scenario) result.Scenario {
	dir, err := os.MkdirTemp("", "opcode42-conf-")
	if err != nil {
		return result.Scenario{Name: sc.Name, Steps: []result.Step{errStep("mkdtemp", err)}}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	resolved, rerr := filepath.EvalSymlinks(dir)
	paths := []string{}
	if rerr == nil && resolved != dir {
		paths = append(paths, resolved)
	}

	c := NewClient(target, dir, paths...)
	c.User, c.Pass = user, pass
	steps, runErr := sc.Run(c)
	if runErr != nil {
		// Record what we captured plus the error, so the diff surfaces a
		// step-count or status mismatch rather than silently passing.
		fmt.Fprintf(os.Stderr, "scenario %s: %v\n", sc.Name, runErr)
		steps = append(steps, errStep("error", runErr))
	}
	return result.Scenario{Name: sc.Name, Steps: steps}
}

func errStep(name string, err error) result.Step {
	return result.Step{Name: name, Body: fmt.Sprintf("ERROR: %v", err)}
}
