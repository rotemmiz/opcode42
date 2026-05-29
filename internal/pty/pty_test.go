package pty

import (
	"encoding/json"
	"strings"
	"testing"
)

func newSession() *session {
	return &session{subs: make(map[int]*subscriber)}
}

func TestMetaControlFrame(t *testing.T) {
	frame := meta(42)
	if frame[0] != 0x00 {
		t.Fatalf("control frame byte0 = %d, want 0", frame[0])
	}
	var got struct {
		Cursor int `json:"cursor"`
	}
	if err := json.Unmarshal(frame[1:], &got); err != nil {
		t.Fatalf("control payload not JSON: %v", err)
	}
	if got.Cursor != 42 {
		t.Errorf("cursor = %d, want 42", got.Cursor)
	}
}

func TestCursorCountsUTF16CodeUnits(t *testing.T) {
	s := newSession()
	s.onData([]byte("abc")) // 3 ASCII -> 3 code units
	if s.cursor != 3 {
		t.Errorf("cursor = %d, want 3", s.cursor)
	}
	// U+1F600 (😀) is one rune but TWO UTF-16 code units (a surrogate pair).
	s.onData([]byte("\U0001F600"))
	if s.cursor != 5 {
		t.Errorf("cursor = %d, want 5 (surrogate pair counts as 2)", s.cursor)
	}
	// A BMP 2-byte char (é) is one code unit.
	s.onData([]byte("é"))
	if s.cursor != 6 {
		t.Errorf("cursor = %d, want 6", s.cursor)
	}
}

func TestPartialUTF8AcrossReads(t *testing.T) {
	s := newSession()
	full := []byte("abé") // 'é' = 0xC3 0xA9
	s.onData(full[:3])    // "ab" + lead byte 0xC3 (incomplete)
	if s.cursor != 2 {
		t.Errorf("cursor after partial = %d, want 2 (incomplete rune held back)", s.cursor)
	}
	s.onData(full[3:]) // 0xA9 completes 'é'
	if s.cursor != 3 {
		t.Errorf("cursor after completion = %d, want 3", s.cursor)
	}
	if got := utf16ToString(s.buffer); got != "abé" {
		t.Errorf("buffer = %q, want abé", got)
	}
}

func TestRingBufferTrim(t *testing.T) {
	s := newSession()
	// Emit bufferLimit+10 ASCII code units.
	s.onData([]byte(strings.Repeat("x", bufferLimit+10)))
	if len(s.buffer) != bufferLimit {
		t.Errorf("buffer len = %d, want %d (trimmed)", len(s.buffer), bufferLimit)
	}
	if s.bufCur != 10 {
		t.Errorf("bufCur = %d, want 10 (oldest 10 units dropped)", s.bufCur)
	}
	if s.cursor != bufferLimit+10 {
		t.Errorf("cursor = %d, want %d", s.cursor, bufferLimit+10)
	}
}

// collectInitial concatenates the text replay frames and returns the trailing
// control frame's decoded cursor.
func collectInitial(t *testing.T, frames []Frame) (string, int) {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("no initial frames")
	}
	ctrl := frames[len(frames)-1]
	if !ctrl.Binary || ctrl.Data[0] != 0x00 {
		t.Fatalf("last initial frame is not the control frame: %+v", ctrl)
	}
	var meta struct {
		Cursor int `json:"cursor"`
	}
	if err := json.Unmarshal(ctrl.Data[1:], &meta); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, f := range frames[:len(frames)-1] {
		if f.Binary {
			t.Errorf("unexpected binary frame among replay: %+v", f)
		}
		sb.Write(f.Data)
	}
	return sb.String(), meta.Cursor
}

func TestConnectReplayFromZero(t *testing.T) {
	s := newSession()
	s.onData([]byte("hello"))
	initial, conn, err := s.connect(0)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	text, cursor := collectInitial(t, initial)
	if text != "hello" || cursor != 5 {
		t.Errorf("replay = %q cursor = %d, want hello/5", text, cursor)
	}
}

func TestConnectFromOffset(t *testing.T) {
	s := newSession()
	s.onData([]byte("hello"))
	initial, conn, _ := s.connect(2)
	defer conn.Close()
	text, cursor := collectInitial(t, initial)
	if text != "llo" || cursor != 5 {
		t.Errorf("replay = %q cursor = %d, want llo/5", text, cursor)
	}
}

func TestConnectMinusOneStartsAtEnd(t *testing.T) {
	s := newSession()
	s.onData([]byte("hello"))
	initial, conn, _ := s.connect(-1)
	defer conn.Close()
	text, cursor := collectInitial(t, initial)
	if text != "" || cursor != 5 {
		t.Errorf("replay = %q cursor = %d, want empty/5 (cursor=-1)", text, cursor)
	}
}

func TestConnectReplayChunking(t *testing.T) {
	s := newSession()
	s.onData([]byte(strings.Repeat("a", bufferChunk+100)))
	initial, conn, _ := s.connect(0)
	defer conn.Close()
	// replay frames (all but the trailing control frame) must each be <= bufferChunk.
	for _, f := range initial[:len(initial)-1] {
		if len(f.Data) > bufferChunk {
			t.Errorf("replay frame len %d exceeds chunk %d", len(f.Data), bufferChunk)
		}
	}
	text, _ := collectInitial(t, initial)
	if len(text) != bufferChunk+100 {
		t.Errorf("reassembled replay len = %d, want %d", len(text), bufferChunk+100)
	}
}

func TestLiveFanOutAfterConnect(t *testing.T) {
	s := newSession()
	_, conn, _ := s.connect(0)
	defer conn.Close()
	s.onData([]byte("live"))
	select {
	case f := <-conn.Live():
		if string(f.Data) != "live" || f.Binary {
			t.Errorf("live frame = %+v, want text live", f)
		}
	default:
		t.Fatal("no live frame delivered to subscriber")
	}
}
