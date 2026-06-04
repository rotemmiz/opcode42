//go:build !windows

package lsp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd in its own process group (Setpgid) so the whole
// descendant tree can be signalled at once on cleanup (plan 03 risk #5;
// lsp/server.ts spawn). POSIX-only; the Windows build uses a no-op variant.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// processGroupID resolves the process-group id of a started cmd. With Setpgid
// set, the group id equals the pid; if the lookup fails we fall back to the pid
// directly. POSIX-only.
func processGroupID(cmd *exec.Cmd) int {
	if cmd.Process == nil {
		return 0
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Fall back to the pid as its own group (Setpgid should make pgid == pid).
		return cmd.Process.Pid
	}
	return pgid
}

// terminateProcessGroup SIGTERMs the whole process group (negative pgid) when a
// group id was resolved, otherwise signals the single process. POSIX-only.
func terminateProcessGroup(h *handle) {
	if h.pgid > 0 {
		_ = syscall.Kill(-h.pgid, syscall.SIGTERM)
		return
	}
	_ = h.cmd.Process.Signal(syscall.SIGTERM)
}
