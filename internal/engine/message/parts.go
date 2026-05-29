package message

import (
	"encoding/json"
	"fmt"
)

// Part is one element of a message's content. Concrete part types embed
// PartBase and carry a "type" discriminator matching opencode's MessageV2 Part
// union (message-v2.ts:352-365). Stored parts marshal to the part.data JSON
// column verbatim, so JSON field names are wire-significant.
type Part interface {
	// base returns the shared id/session/message identity.
	base() PartBase
	// partType is the "type" discriminator value.
	partType() string
}

// PartBase is the identity shared by every part.
type PartBase struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
}

func (b PartBase) base() PartBase { return b }

// PartTime is the optional start/end timestamp block (epoch millis).
type PartTime struct {
	Start int64  `json:"start"`
	End   *int64 `json:"end,omitempty"`
}

// TextPart is streamed/assistant or user text (message-v2.ts:97-111).
type TextPart struct {
	PartBase
	Type      string         `json:"type"` // "text"
	Text      string         `json:"text"`
	Synthetic bool           `json:"synthetic,omitempty"`
	Ignored   bool           `json:"ignored,omitempty"`
	Time      *PartTime      `json:"time,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func (*TextPart) partType() string { return "text" }

// ReasoningPart is a model reasoning/thinking span (message-v2.ts:113-123).
type ReasoningPart struct {
	PartBase
	Type     string         `json:"type"` // "reasoning"
	Text     string         `json:"text"`
	Time     PartTime       `json:"time"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (*ReasoningPart) partType() string { return "reasoning" }

// FilePartSource is the optional provenance of a file part (file/symbol/resource).
// Stored verbatim; not interpreted in M1.
type FilePartSource = json.RawMessage

// FilePart is an attached file/media reference (message-v2.ts:160-168).
type FilePart struct {
	PartBase
	Type     string          `json:"type"` // "file"
	MIME     string          `json:"mime"`
	Filename string          `json:"filename,omitempty"`
	URL      string          `json:"url"`
	Source   json.RawMessage `json:"source,omitempty"`
}

func (*FilePart) partType() string { return "file" }

// ToolState statuses.
const (
	ToolPending   = "pending"
	ToolRunning   = "running"
	ToolCompleted = "completed"
	ToolError     = "error"
)

// ToolPart is a tool invocation and its evolving state (message-v2.ts:310-320).
// State is kept as raw JSON so a part round-trips losslessly regardless of
// status; typed accessors decode the active variant.
type ToolPart struct {
	PartBase
	Type     string          `json:"type"` // "tool"
	CallID   string          `json:"callID"`
	Tool     string          `json:"tool"`
	State    json.RawMessage `json:"state"`
	Metadata map[string]any  `json:"metadata,omitempty"`
}

func (*ToolPart) partType() string { return "tool" }

// Status reads the tool state's status discriminator without fully decoding it.
func (t *ToolPart) Status() string {
	var probe struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(t.State, &probe)
	return probe.Status
}

// ToolStatePending is the initial state once a call ID is known.
type ToolStatePending struct {
	Status string         `json:"status"` // "pending"
	Input  map[string]any `json:"input"`
	Raw    string         `json:"raw"`
}

// ToolStateRunning is set when execution begins.
type ToolStateRunning struct {
	Status   string         `json:"status"` // "running"
	Input    map[string]any `json:"input"`
	Title    string         `json:"title,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Time     struct {
		Start int64 `json:"start"`
	} `json:"time"`
}

// ToolStateCompleted is a successful result.
type ToolStateCompleted struct {
	Status   string         `json:"status"` // "completed"
	Input    map[string]any `json:"input"`
	Output   string         `json:"output"`
	Title    string         `json:"title"`
	Metadata map[string]any `json:"metadata"`
	Time     struct {
		Start     int64  `json:"start"`
		End       int64  `json:"end"`
		Compacted *int64 `json:"compacted,omitempty"`
	} `json:"time"`
	Attachments []FilePart `json:"attachments,omitempty"`
}

// ToolStateError is a failed or interrupted result.
type ToolStateError struct {
	Status   string         `json:"status"` // "error"
	Input    map[string]any `json:"input"`
	Error    string         `json:"error"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Time     struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	} `json:"time"`
}

// StepStartPart marks the start of a provider step (message-v2.ts:222-226).
type StepStartPart struct {
	PartBase
	Type     string `json:"type"` // "step-start"
	Snapshot string `json:"snapshot,omitempty"`
}

func (*StepStartPart) partType() string { return "step-start" }

// TokenCounts is the per-message/step usage block (message-v2.ts:235-244).
type TokenCounts struct {
	Total     *float64 `json:"total,omitempty"`
	Input     float64  `json:"input"`
	Output    float64  `json:"output"`
	Reasoning float64  `json:"reasoning"`
	Cache     struct {
		Read  float64 `json:"read"`
		Write float64 `json:"write"`
	} `json:"cache"`
}

// StepFinishPart records cost/usage at the end of a provider step.
type StepFinishPart struct {
	PartBase
	Type     string      `json:"type"` // "step-finish"
	Reason   string      `json:"reason"`
	Snapshot string      `json:"snapshot,omitempty"`
	Cost     float64     `json:"cost"`
	Tokens   TokenCounts `json:"tokens"`
}

func (*StepFinishPart) partType() string { return "step-finish" }

// PatchPart is emitted when a git snapshot diff is non-empty (message-v2.ts:88-95).
type PatchPart struct {
	PartBase
	Type  string   `json:"type"` // "patch"
	Hash  string   `json:"hash"`
	Files []string `json:"files"`
}

func (*PatchPart) partType() string { return "patch" }

// CompactionPart marks where history was summarized (message-v2.ts:184-191).
type CompactionPart struct {
	PartBase
	Type        string `json:"type"` // "compaction"
	Auto        bool   `json:"auto"`
	Overflow    bool   `json:"overflow,omitempty"`
	TailStartID string `json:"tail_start_id,omitempty"`
}

func (*CompactionPart) partType() string { return "compaction" }

// SubtaskPart records a subagent task attached to a user message.
type SubtaskPart struct {
	PartBase
	Type        string `json:"type"` // "subtask"
	Prompt      string `json:"prompt"`
	Description string `json:"description"`
	Agent       string `json:"agent"`
	Command     string `json:"command,omitempty"`
}

func (*SubtaskPart) partType() string { return "subtask" }

// rawPart preserves any part type Forge does not model explicitly (snapshot,
// agent, retry, …) so reads/writes round-trip without loss.
type rawPart struct {
	PartBase
	typ  string
	data json.RawMessage
}

func (r *rawPart) partType() string { return r.typ }

func (r *rawPart) MarshalJSON() ([]byte, error) { return r.data, nil }

// MarshalPart serializes a part to its JSON form (the part.data column).
func MarshalPart(p Part) ([]byte, error) { return json.Marshal(p) }

// UnmarshalPart decodes a stored part by its "type" discriminator. Unknown
// types are preserved via rawPart rather than dropped.
func UnmarshalPart(data []byte) (Part, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("part: read type: %w", err)
	}
	var p Part
	switch probe.Type {
	case "text":
		p = new(TextPart)
	case "reasoning":
		p = new(ReasoningPart)
	case "file":
		p = new(FilePart)
	case "tool":
		p = new(ToolPart)
	case "step-start":
		p = new(StepStartPart)
	case "step-finish":
		p = new(StepFinishPart)
	case "patch":
		p = new(PatchPart)
	case "compaction":
		p = new(CompactionPart)
	case "subtask":
		p = new(SubtaskPart)
	default:
		raw := &rawPart{typ: probe.Type, data: append(json.RawMessage(nil), data...)}
		if err := json.Unmarshal(data, &raw.PartBase); err != nil {
			return nil, fmt.Errorf("part %q: read base: %w", probe.Type, err)
		}
		return raw, nil
	}
	if err := json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("part %q: %w", probe.Type, err)
	}
	return p, nil
}
