package message

import (
	"encoding/json"
	"fmt"
)

// TransformList is the mutable output of the
// experimental.chat.messages.transform hook (plugin/src/index.ts:284-292): the
// full message list — each entry an { info, parts } pair — that a plugin may
// rewrite just before it is serialized for the model
// (session/prompt.ts:1433, session/compaction.ts:405).
//
// It owns custom JSON so the heterogeneous Part interface round-trips through
// UnmarshalPart: the bridge marshals the current list out, the host hands it to
// plugins, and the (possibly mutated) list is unmarshalled back over this value.
// On any decode failure the caller keeps the original, untransformed list.
type TransformList struct {
	Messages []WithParts `json:"messages"`
}

// transformEntry mirrors one { info, parts } element on the wire. info is
// decoded by its role discriminator and parts by their type discriminator so
// the typed message graph survives the round-trip.
type transformEntry struct {
	Info  json.RawMessage   `json:"info"`
	Parts []json.RawMessage `json:"parts"`
}

// MarshalJSON emits { messages: [{ info, parts }] } using the existing
// Info/Part marshallers. The alias type sheds the custom marshaller to avoid
// recursion while reusing the same field/tag layout.
func (t TransformList) MarshalJSON() ([]byte, error) {
	type wire TransformList
	w := wire(t)
	if w.Messages == nil {
		w.Messages = []WithParts{}
	}
	return json.Marshal(w)
}

// UnmarshalJSON decodes a transformed message list, reconstructing each Info by
// role and each Part by type. A malformed entry fails the whole decode so the
// bridge falls back to the unmodified list (no partial corruption).
func (t *TransformList) UnmarshalJSON(data []byte) error {
	var wire struct {
		Messages []transformEntry `json:"messages"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	out := make([]WithParts, 0, len(wire.Messages))
	for i, e := range wire.Messages {
		info, err := UnmarshalInfo(e.Info)
		if err != nil {
			return fmt.Errorf("transform message %d: %w", i, err)
		}
		parts := make([]Part, 0, len(e.Parts))
		for j, raw := range e.Parts {
			p, err := UnmarshalPart(raw)
			if err != nil {
				return fmt.Errorf("transform message %d part %d: %w", i, j, err)
			}
			parts = append(parts, p)
		}
		out = append(out, WithParts{Info: info, Parts: parts})
	}
	t.Messages = out
	return nil
}
