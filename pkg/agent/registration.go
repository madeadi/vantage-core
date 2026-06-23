package agent

type RegisterResponse struct {
	AgentID string `json:"agent_id"`
	Token   string `json:"token"`
	WSURL   string `json:"ws_url"`

	TopicTelemetry string `json:"topic_telemetry"`
	TopicTasks     string `json:"topic_tasks"`
}
