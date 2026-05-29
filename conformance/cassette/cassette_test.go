package cassette

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden cassette fixture from the encoder output")

const fixturePath = "testdata/sample.json"

func loadFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// TestRoundTripByteForByte decodes the committed sample and re-encodes it,
// asserting the bytes are identical. Run with -update to (re)generate the
// fixture in the encoder's canonical form.
func TestRoundTripByteForByte(t *testing.T) {
	raw := loadFixture(t)
	c, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	out, err := c.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if *update {
		if err := os.WriteFile(fixturePath, out, 0o644); err != nil {
			t.Fatalf("update fixture: %v", err)
		}
		t.Log("fixture updated")
		return
	}
	if !bytes.Equal(raw, out) {
		t.Errorf("round-trip not byte-for-byte\n--- want ---\n%s\n--- got ---\n%s", raw, out)
	}
}

func TestDecodeRejectsUnknownVersion(t *testing.T) {
	if _, err := Decode([]byte(`{"version":2,"interactions":[]}`)); err == nil {
		t.Error("expected error for version 2")
	}
}

func TestTransportFilters(t *testing.T) {
	c, err := Decode(loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.HTTPInteractions()); got != 1 {
		t.Errorf("http interactions: want 1, got %d", got)
	}
	if got := len(c.WebSocketInteractions()); got != 1 {
		t.Errorf("websocket interactions: want 1, got %d", got)
	}
}

// TestFrameBytesDecodesPTYControlFrame verifies the binary-frame decode path and
// the PTY control-frame contract: byte 0 is 0x00, the rest is UTF-8 JSON {cursor}
// (Finding #3 / pty/index.ts:44-52).
func TestFrameBytesDecodesPTYControlFrame(t *testing.T) {
	c, err := Decode(loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	ws := c.WebSocketInteractions()[0]
	b, err := ws.Server[0].Bytes()
	if err != nil {
		t.Fatalf("decode frame bytes: %v", err)
	}
	if len(b) == 0 || b[0] != 0x00 {
		t.Fatalf("control frame must start with 0x00, got %v", b)
	}
	var ctrl struct {
		Cursor int `json:"cursor"`
	}
	if err := json.Unmarshal(b[1:], &ctrl); err != nil {
		t.Fatalf("control frame payload is not {cursor}: %v", err)
	}
	if ctrl.Cursor != 0 {
		t.Errorf("cursor: want 0, got %d", ctrl.Cursor)
	}
}

func TestTextFrameBytes(t *testing.T) {
	f := Frame{Kind: FrameText, Body: "ls\n"}
	b, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "ls\n" {
		t.Errorf("text frame bytes: want %q, got %q", "ls\n", string(b))
	}
}
