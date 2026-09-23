package agentsdk

import "encoding/json"

// Task is what core publishes to Topic.NewTask() (spec Step 16). JSON tags
// are explicit and lower_snake_case, matching this package's other wire
// payloads (EventPayload, TaskStatusPayload) -- Go's default field-name
// keys would otherwise silently differ from what core actually sends.
type Task struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
