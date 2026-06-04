package message

import (
	"encoding/json"
	"testing"
)

// TestTransformListRoundTrip asserts the experimental.chat.messages.transform
// payload survives a marshal→unmarshal round-trip: the heterogeneous Part
// interface is reconstructed by type and the Info by role, so a plugin's
// mutated list comes back as a typed message graph.
func TestTransformListRoundTrip(t *testing.T) {
	in := TransformList{Messages: []WithParts{
		userMsg("msg_user", &TextPart{PartBase: base("msg_user", "prt_1"), Type: "text", Text: "hello"}),
		assistantMsg("msg_asst", "msg_user",
			&TextPart{PartBase: base("msg_asst", "prt_2"), Type: "text", Text: "hi back"},
			&ToolPart{PartBase: base("msg_asst", "prt_3"), Type: "tool", CallID: "c1", Tool: "bash",
				State: rawState(t, ToolStateCompleted{Status: ToolCompleted, Output: "ok"})}),
	}}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out TransformList
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(out.Messages))
	}
	if out.Messages[0].Info.User == nil || out.Messages[0].Info.User.ID != "msg_user" {
		t.Fatalf("first message not decoded as user: %+v", out.Messages[0].Info)
	}
	if out.Messages[1].Info.Assistant == nil {
		t.Fatalf("second message not decoded as assistant: %+v", out.Messages[1].Info)
	}
	tp, ok := out.Messages[1].Parts[1].(*ToolPart)
	if !ok {
		t.Fatalf("tool part not decoded by type: %T", out.Messages[1].Parts[1])
	}
	if tp.Tool != "bash" || tp.CallID != "c1" {
		t.Fatalf("tool part fields lost: %+v", tp)
	}
}

// TestTransformListMutationApplied asserts a mutation made on the decoded list
// (as a plugin would) is observable after a second round-trip — i.e. the list
// the loop serializes reflects the plugin's edit.
func TestTransformListMutationApplied(t *testing.T) {
	in := TransformList{Messages: []WithParts{
		userMsg("msg_user", &TextPart{PartBase: base("msg_user", "prt_1"), Type: "text", Text: "before"}),
	}}
	data, _ := json.Marshal(in)
	var out TransformList
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Plugin mutates the text part.
	out.Messages[0].Parts[0].(*TextPart).Text = "after"

	models := ToModelMessages(out.Messages, testModel(), SerializeOptions{})
	if len(models) != 1 {
		t.Fatalf("want 1 model message, got %d", len(models))
	}
	if models[0].Content[0].Text != "after" {
		t.Fatalf("mutation not reflected: %q", models[0].Content[0].Text)
	}
}

// TestTransformListEmpty asserts an empty list marshals to a stable shape and
// decodes back to an empty (non-nil-driven) list.
func TestTransformListEmpty(t *testing.T) {
	data, err := json.Marshal(TransformList{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"messages":[]}` {
		t.Fatalf("empty transform list shape = %s", data)
	}
	var out TransformList
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Messages) != 0 {
		t.Fatalf("want empty messages, got %d", len(out.Messages))
	}
}
