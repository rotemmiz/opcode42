package pty

import (
	"os"
	"path/filepath"
)

// loginShells are shells that accept a "-l" login flag, mirroring opencode's
// shell META table (shell/shell.ts:10-19).
var loginShells = map[string]bool{
	"bash": true, "dash": true, "fish": true, "ksh": true, "sh": true, "zsh": true,
}

// candidateShells is the fallback list opencode advertises (shell/shell.ts:104).
var candidateShells = []string{"/bin/bash", "/bin/zsh", "/bin/sh"}

// PreferredShell returns the shell to spawn: the config shell if set, else
// $SHELL, else a platform default (shell/shell.ts:126-129,198).
func PreferredShell(configShell string) string {
	if configShell != "" {
		return configShell
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if isDarwin() {
		return "/bin/zsh"
	}
	return "/bin/sh"
}

// isLoginShell reports whether file is a shell that takes a "-l" login flag
// (shell/shell.ts:137-138).
func isLoginShell(file string) bool {
	return loginShells[filepath.Base(file)]
}

// ListShells returns the available shells opencode would advertise at
// GET /pty/shells (shell/shell.ts:104): the candidates that exist on disk.
func ListShells() []string {
	out := []string{}
	for _, s := range candidateShells {
		if _, err := os.Stat(s); err == nil {
			out = append(out, s)
		}
	}
	return out
}
