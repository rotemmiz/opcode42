package push

import (
	"strings"

	"github.com/rotemmiz/opcode42/internal/bus"
)

// Notification is the platform-neutral push payload the dispatcher renders into
// an FCM (or, later, APNs) message. Data carries the event_type + session_id so
// the client can deep-link to the relevant session on tap.
type Notification struct {
	Title     string
	Body      string
	SessionID string
	EventType string
}

// data returns the FCM `data` map for this notification.
func (n Notification) data() map[string]string {
	return map[string]string{
		"event_type": n.EventType,
		"session_id": n.SessionID,
	}
}

// maxBody bounds a notification body so a runaway message/question text does not
// blow up the FCM payload (FCM caps messages at 4KB).
const maxBody = 240

// FromEvent maps a daemon bus event to a Notification, or returns ok=false when
// the event type is not push-worthy. This is the event→notification mapping of
// plan 13 §13.8:
//
//	session.idle       -> "Agent finished"
//	permission.asked   -> "Permission needed"
//	question.asked     -> "Agent has a question"
//
// session.status is deliberately NOT mapped: the engine emits the deprecated
// session.idle alongside every idle session.status transition
// (engine.go:276-280), so keying on session.idle gives exactly one push per
// idle transition without double-firing.
func FromEvent(e bus.Event) (Notification, bool) {
	props := asMap(e.Properties)
	switch e.Type {
	case "session.idle":
		return Notification{
			Title:     "Agent finished",
			Body:      "Your agent finished its task.",
			SessionID: stringField(props, "sessionID"),
			EventType: e.Type,
		}, true
	case "permission.asked":
		tool := stringField(props, "tool")
		body := "The agent needs your approval to continue."
		if tool != "" {
			body = "The agent wants to run " + tool + "."
		}
		return Notification{
			Title:     "Permission needed",
			Body:      truncate(body),
			SessionID: stringField(props, "sessionID"),
			EventType: e.Type,
		}, true
	case "question.asked":
		body := stringField(props, "text")
		if body == "" {
			body = stringField(props, "question")
		}
		if body == "" {
			body = "The agent has a question for you."
		}
		return Notification{
			Title:     "Agent has a question",
			Body:      truncate(body),
			SessionID: stringField(props, "sessionID"),
			EventType: e.Type,
		}, true
	default:
		return Notification{}, false
	}
}

// asMap coerces an event's Properties to a map for field extraction. Events
// published via bus.NewEvent carry a map[string]any; typed structs are coerced
// best-effort and yield empty fields (still a valid, if generic, notification).
func asMap(props any) map[string]any {
	if m, ok := props.(map[string]any); ok {
		return m
	}
	return nil
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxBody {
		return s
	}
	return strings.TrimSpace(s[:maxBody]) + "…"
}
