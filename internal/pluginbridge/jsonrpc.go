package pluginbridge

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// JSON-RPC 2.0 message framing matches plan 05 §"RPC Transport Choice":
// a 4-byte big-endian uint32 length header followed by the UTF-8 JSON body.
// The Node/Bun host (packages/forge-plugin-host) uses the identical framing.

// maxFrameBytes caps a single decoded frame so a corrupt or hostile length
// header cannot make the bridge allocate unbounded memory. Hook payloads
// (e.g. experimental.chat.messages.transform with a large history) are well
// under this ceiling; plan 05 §Performance budgets ~1 MB for 500 messages.
const maxFrameBytes = 64 << 20 // 64 MiB

// rpcRequest is a JSON-RPC 2.0 request or notification. A nil ID marks a
// notification (no response expected); a non-nil ID expects a matching response.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response. Exactly one of Result/Error is set.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("plugin host error %d: %s", e.Code, e.Message) }

// rpcMessage is the union decoded off the wire: a frame may be a request,
// a notification, or a response. Method != "" distinguishes request/notification
// from a response.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// writeFrame length-prefixes and writes a single JSON value to w.
func writeFrame(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	if len(body) > maxFrameBytes {
		return fmt.Errorf("frame too large: %d bytes", len(body))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// readFrame reads one length-prefixed JSON frame into a raw byte slice.
func readFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n > maxFrameBytes {
		return nil, fmt.Errorf("frame length %d exceeds limit %d", n, maxFrameBytes)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}
