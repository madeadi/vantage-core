package agentsdk

import (
	"fmt"
	"strings"
)

// Topic layout is agent-id-first: <prefix>/agent/<id>/<topic>. The agent ID
// sits at a fixed depth so a broker ACL can substitute it with one static
// pattern per direction (e.g. Mosquitto's %u) instead of a rule per agent —
// see specs/mqtt_telemetry.specs.md.
//
// Retain/QoS per topic (Mosquitto semantics):
//
//	telemetry          not retained  QoS 0  published by the agent
//	telemetry-schema   retained      QoS 1  published by the agent (advisory —
//	                                        the validation authority is
//	                                        agent_groups.telemetry_schema)
//	event              retained      QoS 1  published by the agent; also the LWT
//	task/new           not retained  QoS 1  published by core
//	task/<id>/status   not retained  QoS 1  published by the agent
//	pose               reserved — not built in this scope
const (
	topicSegTelemetry       = "telemetry"
	topicSegTelemetrySchema = "telemetry-schema"
	topicSegEvent           = "event"
	topicSegTaskNew         = "task/new"
	topicSegTask            = "task"
)

// Topic builds the MQTT topic strings for one agent under a shared prefix.
type Topic struct {
	prefix  string
	agentID string
}

// NewTopic validates prefix and agentID and returns a Topic for building this
// agent's topic strings.
//
// Both reject the MQTT wildcards ('+', '#') and the path separator ('/'):
// either would let the value occupy more than one topic level, colliding with
// another agent's identity or with the wildcard subscriptions core uses to
// fan in across agents (see TelemetryFilter et al.). prefix additionally
// rejects a leading or trailing slash, which would otherwise produce a
// doubled or stray separator in every topic built from it.
func NewTopic(prefix, agentID string) (Topic, error) {
	if strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/") {
		return Topic{}, fmt.Errorf("agentsdk: topic prefix %q must not have a leading or trailing slash", prefix)
	}
	if err := validTopicSegment("topic prefix", prefix); err != nil {
		return Topic{}, err
	}
	if err := validTopicSegment("agent id", agentID); err != nil {
		return Topic{}, err
	}
	return Topic{prefix: prefix, agentID: agentID}, nil
}

// validTopicSegment rejects values that would corrupt the fixed-depth layout
// the ACL patterns below depend on: empty, containing '/', or containing an
// MQTT wildcard ('+', '#').
func validTopicSegment(what, v string) error {
	if v == "" {
		return fmt.Errorf("agentsdk: %s must not be empty", what)
	}
	if strings.ContainsAny(v, "+#") {
		return fmt.Errorf("agentsdk: %s %q must not contain the MQTT wildcard characters '+' or '#'", what, v)
	}
	if strings.Contains(v, "/") {
		return fmt.Errorf("agentsdk: %s %q must not contain '/'", what, v)
	}
	return nil
}

func (t Topic) base() string {
	return t.prefix + "/agent/" + t.agentID
}

// Telemetry is where the agent publishes telemetry payloads.
func (t Topic) Telemetry() string { return t.base() + "/" + topicSegTelemetry }

// TelemetrySchema is where the agent publishes its advisory declared schema.
func (t Topic) TelemetrySchema() string { return t.base() + "/" + topicSegTelemetrySchema }

// Event is where the agent publishes lifecycle events (online, offline,
// registered, invalid_data) and is also the LWT topic.
func (t Topic) Event() string { return t.base() + "/" + topicSegEvent }

// NewTask is where core publishes new tasks for this agent to run.
func (t Topic) NewTask() string { return t.base() + "/" + topicSegTaskNew }

// TaskStatus is where the agent publishes ack/status updates for one task.
func (t Topic) TaskStatus(taskID string) (string, error) {
	if err := validTopicSegment("task id", taskID); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%s/status", t.base(), topicSegTask, taskID), nil
}

// -- core-side subscriber filters --
//
// core does not build these from a Topic, since it fans in across every
// agent under prefix rather than addressing one. Kept in this file, next to
// the builders above, so the publish and subscribe sides of the layout
// cannot drift apart.

// TelemetryFilter subscribes to every agent's telemetry topic.
func TelemetryFilter(prefix string) string { return prefix + "/agent/+/" + topicSegTelemetry }

// TelemetrySchemaFilter subscribes to every agent's declared-schema topic.
func TelemetrySchemaFilter(prefix string) string {
	return prefix + "/agent/+/" + topicSegTelemetrySchema
}

// EventFilter subscribes to every agent's lifecycle-event topic.
func EventFilter(prefix string) string { return prefix + "/agent/+/" + topicSegEvent }

// TaskStatusFilter subscribes to every agent's task-status topics.
func TaskStatusFilter(prefix string) string {
	return prefix + "/agent/+/" + topicSegTask + "/+/status"
}

// AgentIDFromTopic extracts the <id> segment from a topic published under
// prefix's agent-id-first layout (<prefix>/agent/<id>/...). It reports false
// if topic does not have that shape.
func AgentIDFromTopic(prefix, topic string) (agentID string, ok bool) {
	head := prefix + "/agent/"
	if !strings.HasPrefix(topic, head) {
		return "", false
	}
	rest := topic[len(head):]
	i := strings.IndexByte(rest, '/')
	if i <= 0 {
		return "", false
	}
	return rest[:i], true
}
