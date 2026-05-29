package message

import (
	"encoding/json"
	"fmt"
)

// Role discriminates a stored message.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Model is the provider/model selection on a user message.
type Model struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	Variant    string `json:"variant,omitempty"`
}

// OutputFormat is the requested response format (text | json_schema).
type OutputFormat struct {
	Type       string         `json:"type"`
	Schema     map[string]any `json:"schema,omitempty"`
	RetryCount *int           `json:"retryCount,omitempty"`
}

// UserMessage is a user turn (message-v2.ts:327-349).
type UserMessage struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Role      string `json:"role"` // "user"
	Time      struct {
		Created int64 `json:"created"`
	} `json:"time"`
	Format *OutputFormat   `json:"format,omitempty"`
	Agent  string          `json:"agent"`
	Model  Model           `json:"model"`
	System string          `json:"system,omitempty"`
	Tools  map[string]bool `json:"tools,omitempty"`
}

// Path is the assistant message's working/root directory pair.
type Path struct {
	CWD  string `json:"cwd"`
	Root string `json:"root"`
}

// Error is the assistant error envelope (NamedError shape: {name, data}).
type Error struct {
	Name string         `json:"name"`
	Data map[string]any `json:"data,omitempty"`
}

// AssistantMessage is an assistant turn (message-v2.ts:452-490).
type AssistantMessage struct {
	ID         string      `json:"id"`
	SessionID  string      `json:"sessionID"`
	Role       string      `json:"role"` // "assistant"
	ParentID   string      `json:"parentID"`
	ModelID    string      `json:"modelID"`
	ProviderID string      `json:"providerID"`
	Mode       string      `json:"mode"`
	Agent      string      `json:"agent"`
	Path       Path        `json:"path"`
	Summary    bool        `json:"summary,omitempty"`
	Cost       float64     `json:"cost"`
	Tokens     TokenCounts `json:"tokens"`
	Structured any         `json:"structured,omitempty"`
	Variant    string      `json:"variant,omitempty"`
	Finish     string      `json:"finish,omitempty"`
	Error      *Error      `json:"error,omitempty"`
	Time       struct {
		Created   int64  `json:"created"`
		Completed *int64 `json:"completed,omitempty"`
	} `json:"time"`
}

// Info is a stored message: exactly one of User/Assistant is set, mirroring
// opencode's discriminated Message union (message-v2.ts:492-493). It marshals
// to the bare message object (not a wrapper) for wire compatibility.
type Info struct {
	User      *UserMessage
	Assistant *AssistantMessage
}

// IsUser reports whether this is a user message.
func (i Info) IsUser() bool { return i.User != nil }

// ID returns the message id regardless of role.
func (i Info) ID() string {
	if i.User != nil {
		return i.User.ID
	}
	if i.Assistant != nil {
		return i.Assistant.ID
	}
	return ""
}

// Role returns the message role discriminator.
func (i Info) Role() string {
	if i.User != nil {
		return RoleUser
	}
	return RoleAssistant
}

// MarshalJSON emits the active variant directly (no envelope).
func (i Info) MarshalJSON() ([]byte, error) {
	switch {
	case i.User != nil:
		return json.Marshal(i.User)
	case i.Assistant != nil:
		return json.Marshal(i.Assistant)
	default:
		return nil, fmt.Errorf("message.Info: neither user nor assistant set")
	}
}

// UnmarshalInfo decodes a stored message object by its "role" discriminator.
func UnmarshalInfo(data []byte) (Info, error) {
	var probe struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return Info{}, fmt.Errorf("message: read role: %w", err)
	}
	switch probe.Role {
	case RoleUser:
		m := new(UserMessage)
		if err := json.Unmarshal(data, m); err != nil {
			return Info{}, err
		}
		return Info{User: m}, nil
	case RoleAssistant:
		m := new(AssistantMessage)
		if err := json.Unmarshal(data, m); err != nil {
			return Info{}, err
		}
		return Info{Assistant: m}, nil
	default:
		return Info{}, fmt.Errorf("message: unknown role %q", probe.Role)
	}
}

// WithParts couples a message with its ordered parts (message-v2.ts:554-561).
type WithParts struct {
	Info  Info   `json:"info"`
	Parts []Part `json:"parts"`
}
