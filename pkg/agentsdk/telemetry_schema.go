package agentsdk

import "time"

type TelemetrySchema struct {
	ID        string    `json:"agent_id"`
	Timestamp time.Time `json:"timestamp"`
}
