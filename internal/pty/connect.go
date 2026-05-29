package pty

// Conn is one subscriber's live attachment to a session.
type Conn struct {
	s   *session
	key int
	ch  chan Frame
}

// Connect attaches a subscriber to a session and returns the frames to send
// immediately (buffered replay split into <=64KB-unit text chunks, followed by
// the binary control frame carrying the current cursor) plus a Conn for live
// streaming and input. cursor semantics match opencode (pty/index.ts:312-360):
// -1 starts at the current end, >=0 starts at that absolute code-unit offset,
// any other value starts at 0.
func (m *Manager) Connect(ptyID string, cursor int) ([]Frame, *Conn, error) {
	s := m.lookup(ptyID)
	if s == nil {
		return nil, nil, ErrNotFound
	}
	return s.connect(cursor)
}

func (s *session) connect(cursor int) ([]Frame, *Conn, error) {
	s.mu.Lock()
	start, end := s.bufCur, s.cursor

	from := 0
	switch {
	case cursor == -1:
		from = end
	case cursor >= 0:
		from = cursor
	}

	var replay []uint16
	if len(s.buffer) > 0 && from < end {
		offset := from - start
		if offset < 0 {
			offset = 0
		}
		if offset < len(s.buffer) {
			replay = append([]uint16(nil), s.buffer[offset:]...)
		}
	}

	key := s.nextSub
	s.nextSub++
	sub := &subscriber{ch: make(chan Frame, subBuffer)}
	s.subs[key] = sub
	s.mu.Unlock()

	initial := make([]Frame, 0, len(replay)/bufferChunk+1)
	for i := 0; i < len(replay); i += bufferChunk {
		j := i + bufferChunk
		if j > len(replay) {
			j = len(replay)
		}
		initial = append(initial, Frame{Data: []byte(utf16ToString(replay[i:j]))})
	}
	initial = append(initial, Frame{Binary: true, Data: meta(end)})

	return initial, &Conn{s: s, key: key, ch: sub.ch}, nil
}

// Live is the channel of live output frames. It is closed when the session
// exits or this subscriber is dropped.
func (c *Conn) Live() <-chan Frame { return c.ch }

// Write forwards client input bytes to the PTY.
func (c *Conn) Write(p []byte) { c.s.write(p) }

// Close unsubscribes. Safe to call more than once and concurrently with the
// session closing the channel (the map guard ensures a single close).
func (c *Conn) Close() {
	c.s.mu.Lock()
	if sub, ok := c.s.subs[c.key]; ok {
		delete(c.s.subs, c.key)
		close(sub.ch)
	}
	c.s.mu.Unlock()
}
