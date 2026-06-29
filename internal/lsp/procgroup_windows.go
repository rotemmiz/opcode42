//go:build windows

package lsp

import "os/exec"

// setProcessGroup is a no-op on Windows: there are no POSIX process groups, so
// the daemon relies on a plain process kill (terminateProcessGroup) for cleanup.
// (Job-object based tree termination is a possible future enhancement.)
func setProcessGroup(_ *exec.Cmd) {}

// processGroupID returns 0 on Windows: process groups are not used, so killGroup
// always takes the single-process kill path.
func processGroupID(_ *exec.Cmd) int { return 0 }

// terminateProcessGroup kills the spawned process on Windows. Unlike the POSIX
// path it cannot signal a whole tree (no process groups); LSP servers spawned by
// Opcode42 are single processes, so a direct Kill is sufficient.
func terminateProcessGroup(h *handle) {
	_ = h.cmd.Process.Kill()
}
