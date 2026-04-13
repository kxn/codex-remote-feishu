package conversationtrace

import "time"

type EventKind string

const (
	EventUserMessage   EventKind = "user_message"
	EventSteerMessage  EventKind = "steer_message"
	EventAssistantText EventKind = "assistant_text"
	EventTurnStarted   EventKind = "turn_started"
	EventTurnCompleted EventKind = "turn_completed"
)

type Entry struct {
	Timestamp        time.Time `json:"ts"`
	Event            EventKind `json:"event"`
	Actor            string    `json:"actor,omitempty"`
	SurfaceSessionID string    `json:"surfaceSessionId,omitempty"`
	ChatID           string    `json:"chatId,omitempty"`
	MessageID        string    `json:"messageId,omitempty"`
	InstanceID       string    `json:"instanceId,omitempty"`
	ThreadID         string    `json:"threadId,omitempty"`
	TurnID           string    `json:"turnId,omitempty"`
	Text             string    `json:"text,omitempty"`
	Status           string    `json:"status,omitempty"`
	Final            bool      `json:"final,omitempty"`
}
