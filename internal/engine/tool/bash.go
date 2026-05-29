package tool

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// defaultBashTimeout / maxBashTimeout bound a command's wall-clock runtime.
const (
	defaultBashTimeout = 60 * time.Second
	maxBashTimeout     = 10 * time.Minute
)

// Bash runs a shell command in the working directory.
type Bash struct{}

// Info describes the bash tool.
func (Bash) Info() Info {
	return Info{
		ID:          "bash",
		Description: "Execute a shell command in the working directory and return its combined output.",
		Parameters: obj(map[string]any{
			"command":     strProp("The shell command to run."),
			"description": strProp("A short description of what the command does."),
			"timeout":     numProp("Optional timeout in milliseconds."),
		}, "command"),
	}
}

type bashParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// Run executes the command via `sh -c`, honoring ctx cancellation and a timeout.
// A non-zero exit is reported in the output and metadata, not as a Go error
// (the model is expected to read it); only spawn failures error.
func (Bash) Run(ctx context.Context, input map[string]any, tctx Context) (Result, error) {
	var p bashParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if p.Command == "" {
		return Result{}, fmt.Errorf("bash: command is required")
	}
	timeout := defaultBashTimeout
	if p.Timeout > 0 {
		timeout = time.Duration(p.Timeout) * time.Millisecond
		if timeout > maxBashTimeout {
			timeout = maxBashTimeout
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", p.Command)
	cmd.Dir = tctx.Directory
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	output := buf.String()
	meta := map[string]any{}
	if cmd.ProcessState != nil {
		meta["exit"] = cmd.ProcessState.ExitCode()
	}
	if runCtx.Err() == context.DeadlineExceeded {
		output += "\n[command timed out]"
		meta["timeout"] = true
	}
	var exitErr *exec.ExitError
	if err != nil && !asExit(err, &exitErr) && runCtx.Err() == nil {
		return Result{}, fmt.Errorf("bash: %w", err)
	}
	return Result{Title: p.Command, Output: output, Metadata: meta}, nil
}

func asExit(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}
