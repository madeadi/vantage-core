package agentsdk

import "fmt"

type TopicResp string

const (
	// shall append agent ID to the topic
	TopicTelemetry       TopicResp = "agent/telemetry"
	TopicTelemetrySchema TopicResp = "agent/telemetry/schema"
	TopicNewTaskRequest  TopicResp = "agent/new-task/request"
	TopicNewTask         TopicResp = "agent/new-task"
	TopicTaskStatus      TopicResp = "agent/task-status"
	TopicEvent           TopicResp = "agent/event"
)

type Topic struct {
	prefix  string
	agentID string
}

func NewTopic(prefix string, agentID string) Topic {
	return Topic{prefix: prefix, agentID: agentID}
}

func (t Topic) Telemetry() string {
	return fmt.Sprintf("%s/%s/%s", t.prefix, TopicTelemetry, t.agentID)
}

func (t Topic) TelemetrySchema() string {
	return fmt.Sprintf("%s/%s/%s", t.prefix, TopicTelemetrySchema, t.agentID)
}

func (t Topic) NewTaskRequest() string {
	return fmt.Sprintf("%s/%s/%s", t.prefix, TopicNewTaskRequest, t.agentID)
}

func (t Topic) NewTask() string {
	return fmt.Sprintf("%s/%s/%s", t.prefix, TopicNewTask, t.agentID)
}

func (t Topic) TaskStatus(taskID string) string {
	return fmt.Sprintf("%s/%s/%s", t.prefix, TopicTaskStatus, taskID)
}

func (t Topic) Event() string {
	return fmt.Sprintf("%s/%s/%s", t.prefix, TopicEvent, t.agentID)
}
