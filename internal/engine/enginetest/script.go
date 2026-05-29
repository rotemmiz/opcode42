package enginetest

import "github.com/rotemmiz/forge/internal/engine/llm"

// Script builds a scripted provider stream fluently. It is the deterministic
// stand-in for a real provider's event sequence in integration tests.
type Script struct{ events []llm.Event }

// NewScript starts an empty script.
func NewScript() *Script { return &Script{} }

// Events returns the accumulated events.
func (s *Script) Events() []llm.Event { return s.events }

// StepStart emits a step-start boundary.
func (s *Script) StepStart() *Script {
	s.events = append(s.events, llm.Event{Type: llm.EventStepStart})
	return s
}

// Text emits a text span as start/delta(s)/end for the given id.
func (s *Script) Text(id string, chunks ...string) *Script {
	s.events = append(s.events, llm.Event{Type: llm.EventTextStart, ID: id})
	for _, c := range chunks {
		s.events = append(s.events, llm.Event{Type: llm.EventTextDelta, ID: id, Text: c})
	}
	s.events = append(s.events, llm.Event{Type: llm.EventTextEnd, ID: id})
	return s
}

// ToolCall emits a complete tool-call (input-start/end + call) for the given id.
func (s *Script) ToolCall(id, name string, input map[string]any) *Script {
	s.events = append(s.events,
		llm.Event{Type: llm.EventToolInputStart, ID: id, Name: name},
		llm.Event{Type: llm.EventToolInputEnd, ID: id},
		llm.Event{Type: llm.EventToolCall, ID: id, Name: name, Input: input},
	)
	return s
}

// StepFinish emits a step-finish with the given reason and usage.
func (s *Script) StepFinish(reason string, usage llm.TokenUsage) *Script {
	u := usage
	s.events = append(s.events, llm.Event{Type: llm.EventStepFinish, Reason: reason, Usage: &u})
	return s
}

// Finish emits the terminal finish event.
func (s *Script) Finish() *Script {
	s.events = append(s.events, llm.Event{Type: llm.EventFinish})
	return s
}
