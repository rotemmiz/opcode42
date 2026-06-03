package lsp

import "io"

// stdioRWC adapts a child process's separate stdout (read) and stdin (write)
// pipes into one io.ReadWriteCloser for the JSON-RPC stream. Close closes both
// pipes; the process group itself is reaped separately (killGroup).
type stdioRWC struct {
	r io.ReadCloser  // process stdout
	w io.WriteCloser // process stdin
}

func (s stdioRWC) Read(p []byte) (int, error)  { return s.r.Read(p) }
func (s stdioRWC) Write(p []byte) (int, error) { return s.w.Write(p) }

func (s stdioRWC) Close() error {
	werr := s.w.Close()
	rerr := s.r.Close()
	if werr != nil {
		return werr
	}
	return rerr
}
