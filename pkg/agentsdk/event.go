package agentsdk

import "time"

type EventType string

const (
	EventOffline    EventType = "offline"
	EventOnline     EventType = "online"
	EventRegistered EventType = "registered"
	EventInvalidData EventType = "invalid_data"
)

type EventPayload struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
	Type      EventType `json:"type"`
	Reason    string    `json:"reason"`
	Payload   []byte    `json:"payload" omitempty:"true"`
}
