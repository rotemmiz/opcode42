// Package pty spawns shells and bridges them to the WebSocket-PTY transport
// with an output ring buffer, matching opencode's framing (pty/index.ts).
//
// The buffer and cursor are measured in UTF-16 code units (JS string .length),
// not bytes or runes, so replay offsets line up byte-for-byte with opencode
// clients (pty/index.ts:239-262; conformance Finding: cursor = UTF-16 units).
package pty

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/creack/pty"

	"github.com/rotemmiz/forge/internal/id"
)

const (
	// bufferLimit is the max retained output in UTF-16 code units (2MB), and
	// bufferChunk is the replay slice size, matching opencode (pty/index.ts:17-18).
	bufferLimit = 2 * 1024 * 1024
	bufferChunk = 64 * 1024
	readChunk   = 64 * 1024
)

// ErrNotFound is returned when a PTY id is unknown.
var ErrNotFound = errors.New("pty session not found")

// Info is the PTY wire shape (pty/index.ts:53-61).
type Info struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Cwd     string   `json:"cwd"`
	Status  string   `json:"status"` // "running" | "exited"
	Pid     int      `json:"pid"`
}

// CreateInput is the POST /pty body (pty/index.ts:65-71).
type CreateInput struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Title   string            `json:"title,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Size is a terminal size.
type Size struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// UpdateInput is the PUT /pty/{id} body (pty/index.ts:80-90).
type UpdateInput struct {
	Title string `json:"title,omitempty"`
	Size  *Size  `json:"size,omitempty"`
}

// Frame is one outbound WebSocket frame to a subscriber: a text data frame or a
// binary control frame.
type Frame struct {
	Binary bool
	Data   []byte
}

// subBuffer is each subscriber's outbound queue depth; a client that fills it
// drops frames rather than stalling the read pump (matches opencode dropping a
// non-writable socket).
const subBuffer = 256

type subscriber struct {
	ch chan Frame
}

type session struct {
	mu      sync.Mutex
	info    Info
	ptmx    *os.File
	cmd     *exec.Cmd
	buffer  []uint16 // last <=bufferLimit UTF-16 code units of output
	bufCur  int      // absolute code-unit offset of buffer[0]
	cursor  int      // total code units ever emitted
	partial []byte   // trailing incomplete UTF-8 bytes between reads
	subs    map[int]*subscriber
	nextSub int
}

// Manager owns the PTY sessions (and their connect tickets) for one instance.
type Manager struct {
	mu          sync.Mutex
	dir         string
	configShell string
	sessions    map[string]*session
	tickets     *tickets
}

// NewManager creates a PTY manager rooted at dir; configShell overrides the
// spawned shell when set.
func NewManager(dir, configShell string) *Manager {
	return &Manager{
		dir:         dir,
		configShell: configShell,
		sessions:    make(map[string]*session),
		tickets:     newTickets(),
	}
}

// Create spawns a new PTY session.
func (m *Manager) Create(in CreateInput) (Info, error) {
	command := in.Command
	if command == "" {
		command = PreferredShell(m.configShell)
	}
	args := append([]string{}, in.Args...)
	if isLoginShell(command) {
		args = append(args, "-l")
	}
	cwd := in.Cwd
	if cwd == "" {
		cwd = m.dir
	}

	ptyID := id.Ascending(id.PTY)
	cmd := exec.Command(command, args...) //nolint:gosec // command is operator-chosen, like opencode
	cmd.Dir = cwd
	cmd.Env = buildEnv(in.Env)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return Info{}, fmt.Errorf("spawn pty: %w", err)
	}

	title := in.Title
	if title == "" {
		title = "Terminal " + lastN(ptyID, 4)
	}
	s := &session{
		info: Info{
			ID:      ptyID,
			Title:   title,
			Command: command,
			Args:    args,
			Cwd:     cwd,
			Status:  "running",
			Pid:     cmd.Process.Pid,
		},
		ptmx: ptmx,
		cmd:  cmd,
		subs: make(map[int]*subscriber),
	}

	m.mu.Lock()
	m.sessions[ptyID] = s
	m.mu.Unlock()

	go s.readLoop()
	return s.snapshot(), nil
}

// List returns every session's info.
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.snapshot())
	}
	return out
}

// Get returns one session's info or ErrNotFound.
func (m *Manager) Get(ptyID string) (Info, error) {
	s := m.lookup(ptyID)
	if s == nil {
		return Info{}, ErrNotFound
	}
	return s.snapshot(), nil
}

// Update applies a title/size change.
func (m *Manager) Update(ptyID string, in UpdateInput) (Info, error) {
	s := m.lookup(ptyID)
	if s == nil {
		return Info{}, ErrNotFound
	}
	s.mu.Lock()
	if in.Title != "" {
		s.info.Title = in.Title
	}
	running := s.info.Status == "running"
	ptmx := s.ptmx
	s.mu.Unlock()
	if in.Size != nil && running {
		_ = pty.Setsize(ptmx, &pty.Winsize{Rows: in.Size.Rows, Cols: in.Size.Cols})
	}
	return s.snapshot(), nil
}

// Remove terminates and forgets a session.
func (m *Manager) Remove(ptyID string) error {
	m.mu.Lock()
	s := m.sessions[ptyID]
	if s == nil {
		m.mu.Unlock()
		return ErrNotFound
	}
	delete(m.sessions, ptyID)
	m.mu.Unlock()
	s.kill()
	return nil
}

// Shutdown terminates all sessions (called on instance teardown).
func (m *Manager) Shutdown() {
	m.mu.Lock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*session)
	m.mu.Unlock()
	for _, s := range sessions {
		s.kill()
	}
}

func (m *Manager) lookup(ptyID string) *session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[ptyID]
}

// buildEnv composes the child environment the way opencode does
// (pty/index.ts:196-211): inherit, then overrides, then the terminal markers.
func buildEnv(extra map[string]string) []string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if i := indexByte(kv, '='); i >= 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	for k, v := range extra {
		env[k] = v
	}
	env["TERM"] = "xterm-256color"
	env["OPENCODE_TERMINAL"] = "1"
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func isDarwin() bool { return runtime.GOOS == "darwin" }

// --- session ---

func (s *session) snapshot() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := s.info
	info.Args = append([]string{}, s.info.Args...)
	return info
}

// readLoop pumps PTY output into the ring buffer and live subscribers until the
// process exits or the master closes.
func (s *session) readLoop() {
	buf := make([]byte, readChunk)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.onData(buf[:n])
		}
		if err != nil {
			s.onExit()
			return
		}
	}
}

// onData decodes a raw read (carrying any partial UTF-8 from the previous read),
// appends the decoded text to the ring buffer in UTF-16 units, advances the
// cursor, fans the text out to subscribers, and trims to bufferLimit.
func (s *session) onData(raw []byte) {
	data := raw
	if len(s.partial) > 0 {
		data = append(s.partial, raw...)
		s.partial = nil
	}
	text, rest := splitValidUTF8(data)
	if len(rest) > 0 {
		s.partial = append([]byte(nil), rest...)
	}
	if text == "" {
		return
	}
	units := utf16.Encode([]rune(text))

	s.mu.Lock()
	s.cursor += len(units)
	for key, sub := range s.subs {
		select {
		case sub.ch <- Frame{Data: []byte(text)}:
		default:
			// Slow subscriber: drop it (matches opencode dropping a stalled socket).
			delete(s.subs, key)
			close(sub.ch)
		}
	}
	s.buffer = append(s.buffer, units...)
	if len(s.buffer) > bufferLimit {
		excess := len(s.buffer) - bufferLimit
		s.buffer = append([]uint16(nil), s.buffer[excess:]...)
		s.bufCur += excess
	}
	s.mu.Unlock()
}

func (s *session) onExit() {
	s.mu.Lock()
	if s.info.Status != "exited" {
		s.info.Status = "exited"
	}
	for key, sub := range s.subs {
		delete(s.subs, key)
		close(sub.ch)
	}
	s.mu.Unlock()
}

func (s *session) kill() {
	s.mu.Lock()
	ptmx, cmd := s.ptmx, s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if ptmx != nil {
		_ = ptmx.Close()
	}
}

// write forwards client input to the PTY (no-op once exited).
func (s *session) write(p []byte) {
	s.mu.Lock()
	ptmx := s.ptmx
	running := s.info.Status == "running"
	s.mu.Unlock()
	if running && ptmx != nil {
		_, _ = ptmx.Write(p)
	}
}

// meta builds the control frame: byte 0x00 followed by UTF-8 JSON {"cursor":n}
// (pty/index.ts:44-51).
func meta(cursor int) []byte {
	b, _ := json.Marshal(struct {
		Cursor int `json:"cursor"`
	}{cursor})
	out := make([]byte, len(b)+1)
	out[0] = 0x00
	copy(out[1:], b)
	return out
}

// splitValidUTF8 returns the longest valid-UTF-8 prefix of b as a string plus
// the trailing bytes of an incomplete final rune (to be prepended to the next
// read), so multi-byte runes split across reads are never corrupted.
func splitValidUTF8(b []byte) (string, []byte) {
	if utf8.Valid(b) {
		return string(b), nil
	}
	// Walk back from the end over the bytes of a possibly-incomplete final rune
	// (continuation bytes 0x80-0xBF, up to 3, plus one lead byte).
	for i := 1; i <= utf8.UTFMax && i <= len(b); i++ {
		if utf8.Valid(b[:len(b)-i]) {
			r, _ := utf8.DecodeRune(b[len(b)-i:])
			if r == utf8.RuneError {
				return string(b[:len(b)-i]), b[len(b)-i:]
			}
			// The tail is itself valid (e.g. truly invalid bytes mid-stream);
			// emit everything and keep nothing.
			break
		}
	}
	// Fallback: emit the valid prefix Go can decode; drop nothing pathological.
	return string(b), nil
}

func utf16ToString(u []uint16) string {
	return string(utf16.Decode(u))
}
