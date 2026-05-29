// Package cassette reads and writes opencode http-recorder cassettes — the
// versioned JSON format used to record HTTP and WebSocket traffic. The Go types
// mirror opencode's schema exactly (packages/http-recorder/src/schema.ts:3-68)
// so cassettes recorded by opencode's TS recorder (task C2) can be replayed and
// asserted against from Go.
//
// Cassette shape:
//
//	{ "version": 1, "metadata"?: {...}, "interactions": [ ... ] }
//
// Each interaction is tagged by "transport":
//
//	http:      { transport:"http", request:{...}, response:{...} }
//	websocket: { transport:"websocket", open:{url,headers}, client:[frame], server:[frame] }
//
// A frame is { kind:"text", body } or { kind:"binary", body, bodyEncoding:"base64" }.
package cassette

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Transport kinds.
const (
	TransportHTTP      = "http"
	TransportWebSocket = "websocket"
)

// Frame kinds.
const (
	FrameText   = "text"
	FrameBinary = "binary"
)

// Cassette is a recorded session of HTTP/WebSocket interactions.
type Cassette struct {
	Version      int            `json:"version"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Interactions []Interaction  `json:"interactions"`
}

// Interaction is a tagged union over Transport. HTTP interactions carry
// Request/Response; WebSocket interactions carry Open/Client/Server. The unused
// half stays nil and is omitted from JSON, preserving the tagged-union shape.
type Interaction struct {
	Transport string `json:"transport"`

	// HTTP
	Request  *RequestSnapshot  `json:"request,omitempty"`
	Response *ResponseSnapshot `json:"response,omitempty"`

	// WebSocket
	Open   *WebSocketOpen `json:"open,omitempty"`
	Client []Frame        `json:"client,omitempty"`
	Server []Frame        `json:"server,omitempty"`
}

// RequestSnapshot captures an HTTP request (schema.ts:3-8).
type RequestSnapshot struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// ResponseSnapshot captures an HTTP response (schema.ts:11-16). For SSE the
// streamed body is captured whole in Body as the "data: {...}\n\n" sequence.
type ResponseSnapshot struct {
	Status       int               `json:"status"`
	Headers      map[string]string `json:"headers"`
	Body         string            `json:"body"`
	BodyEncoding string            `json:"bodyEncoding,omitempty"` // "text" | "base64"
}

// WebSocketOpen captures the upgrade request (schema.ts:37-40).
type WebSocketOpen struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// Frame is one WebSocket message (schema.ts:29-33).
type Frame struct {
	Kind         string `json:"kind"`                   // "text" | "binary"
	Body         string `json:"body"`                   // raw text, or base64 when binary
	BodyEncoding string `json:"bodyEncoding,omitempty"` // "base64" for binary frames
}

// Bytes returns the decoded payload of a frame: the UTF-8 bytes for text frames,
// or the base64-decoded bytes for binary frames.
func (f Frame) Bytes() ([]byte, error) {
	if f.Kind == FrameBinary || f.BodyEncoding == "base64" {
		return base64.StdEncoding.DecodeString(f.Body)
	}
	return []byte(f.Body), nil
}

// Decode parses cassette JSON.
func Decode(data []byte) (*Cassette, error) {
	var c Cassette
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("decode cassette: %w", err)
	}
	if c.Version != 1 {
		return nil, fmt.Errorf("unsupported cassette version %d (want 1)", c.Version)
	}
	return &c, nil
}

// Encode serializes the cassette to match opencode's recorder output:
// JSON.stringify(value, null, 2) — 2-space indent, no HTML escaping of <>&, and
// no trailing newline.
//
// Caveat: Go marshals map keys (headers, metadata) in sorted order, whereas JS
// preserves insertion order. So Encode round-trips byte-for-byte only when a
// cassette's map keys are already sorted; for arbitrary recorded cassettes,
// compare structurally (Decode both sides) rather than by bytes.
func (c *Cassette) Encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("encode cassette: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// HTTPInteractions returns only the HTTP interactions (schema.ts:55).
func (c *Cassette) HTTPInteractions() []Interaction {
	return c.filter(TransportHTTP)
}

// WebSocketInteractions returns only the WebSocket interactions (schema.ts:57).
func (c *Cassette) WebSocketInteractions() []Interaction {
	return c.filter(TransportWebSocket)
}

func (c *Cassette) filter(transport string) []Interaction {
	var out []Interaction
	for _, it := range c.Interactions {
		if it.Transport == transport {
			out = append(out, it)
		}
	}
	return out
}
