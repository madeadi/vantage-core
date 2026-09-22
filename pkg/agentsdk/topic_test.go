package agentsdk

import (
	"strings"
	"testing"
)

func TestNewTopic(t *testing.T) {
	cases := []struct {
		name    string
		prefix  string
		agentID string
		wantErr bool
	}{
		{name: "valid", prefix: "vantageos", agentID: "smallbot", wantErr: false},
		{name: "empty prefix", prefix: "", agentID: "smallbot", wantErr: true},
		{name: "empty agent id", prefix: "vantageos", agentID: "", wantErr: true},
		{name: "prefix leading slash", prefix: "/vantageos", agentID: "smallbot", wantErr: true},
		{name: "prefix trailing slash", prefix: "vantageos/", agentID: "smallbot", wantErr: true},
		{name: "prefix contains slash", prefix: "vantage/os", agentID: "smallbot", wantErr: true},
		{name: "agent id contains slash", prefix: "vantageos", agentID: "small/bot", wantErr: true},
		{name: "agent id contains plus wildcard", prefix: "vantageos", agentID: "small+bot", wantErr: true},
		{name: "agent id contains hash wildcard", prefix: "vantageos", agentID: "small#bot", wantErr: true},
		{name: "prefix contains plus wildcard", prefix: "vantage+os", agentID: "smallbot", wantErr: true},
		{name: "agent id literally schema", prefix: "vantageos", agentID: "schema", wantErr: false}, // regression: old layout collided here
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTopic(tc.prefix, tc.agentID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewTopic(%q, %q) error = %v, wantErr %v", tc.prefix, tc.agentID, err, tc.wantErr)
			}
		})
	}
}

func TestTopicBuilders(t *testing.T) {
	topic, err := NewTopic("vantageos", "smallbot")
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Telemetry", topic.Telemetry(), "vantageos/agent/smallbot/telemetry"},
		{"TelemetrySchema", topic.TelemetrySchema(), "vantageos/agent/smallbot/telemetry-schema"},
		{"Event", topic.Event(), "vantageos/agent/smallbot/event"},
		{"NewTask", topic.NewTask(), "vantageos/agent/smallbot/task/new"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}

	status, err := topic.TaskStatus("task-123")
	if err != nil {
		t.Fatalf("TaskStatus: %v", err)
	}
	if want := "vantageos/agent/smallbot/task/task-123/status"; status != want {
		t.Errorf("TaskStatus = %q, want %q", status, want)
	}

	if _, err := topic.TaskStatus("task/123"); err == nil {
		t.Error("TaskStatus with '/' in task id: want error, got nil")
	}
	if _, err := topic.TaskStatus(""); err == nil {
		t.Error("TaskStatus with empty task id: want error, got nil")
	}
}

// mqttNestedUnder reports whether a subscription to route+"/#" would also
// match topic — i.e. topic's segments have route's segments as a strict
// prefix. This mirrors MQTT's actual per-level matching (unlike a plain Go
// string-prefix check, which is the wrong model: MQTT never matches a
// segment against part of another segment, only whole segments against
// each other or against '+'/'#').
func mqttNestedUnder(topic, route string) bool {
	t := strings.Split(topic, "/")
	r := strings.Split(route, "/")
	if len(t) <= len(r) {
		return false
	}
	for i, seg := range r {
		if t[i] != seg {
			return false
		}
	}
	return true
}

// Regression: the pre-agent-id-first layout nested schema under telemetry
// (agent/telemetry/schema/<id>), so a multi-level wildcard subscription on
// the telemetry topic (agent/telemetry/#) also matched schema announcements.
// The new layout puts telemetry and telemetry-schema at the same depth under
// distinct segment names, so neither topic's segments may be a strict prefix
// of the other's.
func TestTelemetryAndSchemaTopicsDoNotOverlap(t *testing.T) {
	topic, err := NewTopic("vantageos", "smallbot")
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}

	telemetry := topic.Telemetry()
	schema := topic.TelemetrySchema()

	if mqttNestedUnder(schema, telemetry) {
		t.Errorf("a wildcard subscription to %q would also match %q", telemetry+"/#", schema)
	}
	if mqttNestedUnder(telemetry, schema) {
		t.Errorf("a wildcard subscription to %q would also match %q", schema+"/#", telemetry)
	}
}

func TestFilters(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"TelemetryFilter", TelemetryFilter("vantageos"), "vantageos/agent/+/telemetry"},
		{"TelemetrySchemaFilter", TelemetrySchemaFilter("vantageos"), "vantageos/agent/+/telemetry-schema"},
		{"EventFilter", EventFilter("vantageos"), "vantageos/agent/+/event"},
		{"TaskStatusFilter", TaskStatusFilter("vantageos"), "vantageos/agent/+/task/+/status"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestAgentIDFromTopic(t *testing.T) {
	topic, err := NewTopic("vantageos", "smallbot")
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}

	cases := []struct {
		name     string
		topicStr string
		wantID   string
		wantOK   bool
	}{
		{"telemetry topic", topic.Telemetry(), "smallbot", true},
		{"schema topic", topic.TelemetrySchema(), "smallbot", true},
		{"event topic", topic.Event(), "smallbot", true},
		{"wrong prefix", "other/agent/smallbot/telemetry", "", false},
		{"missing agent segment", "vantageos/agent/", "", false},
		{"not under agent/", "vantageos/mission/foo/status", "", false},
		{"unrelated topic", "totally/unrelated", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotOK := AgentIDFromTopic("vantageos", tc.topicStr)
			if gotID != tc.wantID || gotOK != tc.wantOK {
				t.Errorf("AgentIDFromTopic(%q) = (%q, %v), want (%q, %v)", tc.topicStr, gotID, gotOK, tc.wantID, tc.wantOK)
			}
		})
	}
}

func TestTwoAgentsDoNotShareTopics(t *testing.T) {
	a, err := NewTopic("vantageos", "robot-a")
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}
	b, err := NewTopic("vantageos", "robot-b")
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}

	if a.Telemetry() == b.Telemetry() {
		t.Error("two different agent IDs produced the same telemetry topic")
	}
	if a.Event() == b.Event() {
		t.Error("two different agent IDs produced the same event topic")
	}
}
