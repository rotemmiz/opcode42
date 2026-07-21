package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// External editor + terminal bell (plan 08a §F, §I).

// editorDoneMsg is returned after $EDITOR exits; path is the temp buffer file.
type editorDoneMsg struct {
	path string
	err  error
}

// openEditorCmd writes the composer buffer to a temp file and opens it in
// $EDITOR (falling back to $VISUAL, then nano/vi) via tea.ExecProcess, which
// suspends the TUI for the duration. The file is read back on editorDoneMsg.
func openEditorCmd(buffer string) tea.Cmd {
	editor := firstNonEmpty(os.Getenv("EDITOR"), os.Getenv("VISUAL"))
	f, err := os.CreateTemp("", "opcode42-compose-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	path := f.Name()
	_, _ = f.WriteString(buffer)
	_ = f.Close()

	name, args := editorCommand(editor, path)
	cmd := exec.Command(name, args...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorDoneMsg{path: path, err: err}
	})
}

// editorCommand resolves the program + args. An empty/whitespace editor falls
// back to nano then vi; a multi-word $EDITOR (e.g. "code --wait") is split.
func editorCommand(editor, path string) (string, []string) {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		if _, err := exec.LookPath("nano"); err == nil {
			return "nano", []string{path}
		}
		return "vi", []string{path}
	}
	return fields[0], append(fields[1:], path)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// bellCmd rings the terminal bell (attention on a blocking prompt while the
// terminal may be unfocused). Written to /dev/tty so it bypasses the renderer.
func bellCmd() tea.Cmd {
	return func() tea.Msg {
		if f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			_, _ = f.WriteString("\a")
			_ = f.Close()
		}
		return nil
	}
}
