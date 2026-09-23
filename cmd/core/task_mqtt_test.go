package main

import (
	"encoding/json"
	"testing"
	"time"

	"vantageos-core/cmd/core/model"
	"vantageos-core/cmd/core/service"
	"vantageos-core/pkg/agentsdk"
	agentv1 "vantageos-core/proto/agent/v1"
)

// fakeMQTTMessage is a minimal mqtt.Message for exercising taskStatusHandler
// and presenceHandler directly, without a real broker -- their logic is
// pure topic/payload parsing plus a call to a handler, no benefit to
// proving it over an actual network round trip.
type fakeMQTTMessage struct {
	topic   string
	payload []byte
}

func (f fakeMQTTMessage) Duplicate() bool   { return false }
func (f fakeMQTTMessage) Qos() byte         { return 1 }
func (f fakeMQTTMessage) Retained() bool    { return false }
func (f fakeMQTTMessage) Topic() string     { return f.topic }
func (f fakeMQTTMessage) MessageID() uint16 { return 0 }
func (f fakeMQTTMessage) Payload() []byte   { return f.payload }
func (f fakeMQTTMessage) Ack()              {}

type fakeTaskUpdatedHandler struct {
	acks []*agentv1.TaskAck
}

func (f *fakeTaskUpdatedHandler) OnTaskUpdated(ack *agentv1.TaskAck) {
	f.acks = append(f.acks, ack)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestTaskStatusHandlerAppliesAckViaHandler(t *testing.T) {
	handler := &fakeTaskUpdatedHandler{}
	msgHandler := taskStatusHandler("vantageos", handler)

	payload := mustMarshal(t, agentsdk.TaskStatusPayload{Status: agentsdk.TaskAckFinished})
	msg := fakeMQTTMessage{topic: "vantageos/agent/bot-1/task/task-42/status", payload: payload}

	msgHandler(nil, msg)

	if len(handler.acks) != 1 {
		t.Fatalf("got %d acks, want 1", len(handler.acks))
	}
	ack := handler.acks[0]
	if ack.TaskId != "task-42" {
		t.Errorf("TaskId = %q, want task-42 (from the topic, not the payload)", ack.TaskId)
	}
	if ack.Status != agentv1.TaskStatus_TASK_STATUS_FINISHED {
		t.Errorf("Status = %v, want TASK_STATUS_FINISHED", ack.Status)
	}
}

func TestTaskStatusHandlerCarriesErrorMessage(t *testing.T) {
	handler := &fakeTaskUpdatedHandler{}
	msgHandler := taskStatusHandler("vantageos", handler)

	payload := mustMarshal(t, agentsdk.TaskStatusPayload{Status: agentsdk.TaskAckFailed, ErrorMessage: "battery critical"})
	msg := fakeMQTTMessage{topic: "vantageos/agent/bot-1/task/task-1/status", payload: payload}

	msgHandler(nil, msg)

	if len(handler.acks) != 1 {
		t.Fatalf("got %d acks, want 1", len(handler.acks))
	}
	if handler.acks[0].ErrorMessage != "battery critical" {
		t.Errorf("ErrorMessage = %q, want battery critical", handler.acks[0].ErrorMessage)
	}
}

func TestTaskStatusHandlerIgnoresMalformedTopic(t *testing.T) {
	handler := &fakeTaskUpdatedHandler{}
	msgHandler := taskStatusHandler("vantageos", handler)

	// Missing the task id segment entirely.
	msg := fakeMQTTMessage{topic: "vantageos/agent/bot-1/task/status", payload: []byte(`{}`)}
	msgHandler(nil, msg)

	if len(handler.acks) != 0 {
		t.Errorf("got %d acks for a malformed topic, want 0", len(handler.acks))
	}
}

func TestTaskStatusHandlerIgnoresMalformedPayload(t *testing.T) {
	handler := &fakeTaskUpdatedHandler{}
	msgHandler := taskStatusHandler("vantageos", handler)

	msg := fakeMQTTMessage{topic: "vantageos/agent/bot-1/task/task-1/status", payload: []byte("not json")}
	msgHandler(nil, msg)

	if len(handler.acks) != 0 {
		t.Errorf("got %d acks for a malformed payload, want 0", len(handler.acks))
	}
}

func TestTaskAckStatusToProtoMapping(t *testing.T) {
	cases := map[agentsdk.TaskAckStatus]agentv1.TaskStatus{
		agentsdk.TaskAckCannotStart:       agentv1.TaskStatus_TASK_STATUS_CANNOT_START,
		agentsdk.TaskAckStarted:           agentv1.TaskStatus_TASK_STATUS_STARTED,
		agentsdk.TaskAckAborted:           agentv1.TaskStatus_TASK_STATUS_ABORTED,
		agentsdk.TaskAckFailed:            agentv1.TaskStatus_TASK_STATUS_FAILED,
		agentsdk.TaskAckFinished:          agentv1.TaskStatus_TASK_STATUS_FINISHED,
		agentsdk.TaskAckStatus("garbage"): agentv1.TaskStatus_TASK_STATUS_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := taskAckStatusToProto(in); got != want {
			t.Errorf("taskAckStatusToProto(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPresenceHandlerOnlineAndRegisteredMarkOnline(t *testing.T) {
	for _, eventType := range []agentsdk.EventType{agentsdk.EventOnline, agentsdk.EventRegistered} {
		ar := service.NewAgentRegistry(nil, "localhost:9090")
		msgHandler := presenceHandler("vantageos", ar)

		payload := mustMarshal(t, agentsdk.EventPayload{AgentID: "bot-1", Type: eventType, Timestamp: time.Now()})
		msg := fakeMQTTMessage{topic: "vantageos/agent/bot-1/event", payload: payload}
		msgHandler(nil, msg)

		if _, ok := ar.OnlineAgents()[model.AgentID("bot-1")]; !ok {
			t.Errorf("event type %q did not mark bot-1 online", eventType)
		}
	}
}

func TestPresenceHandlerOfflineMarksOffline(t *testing.T) {
	ar := service.NewAgentRegistry(nil, "localhost:9090")
	ar.MarkMQTTOnline("bot-1")
	msgHandler := presenceHandler("vantageos", ar)

	payload := mustMarshal(t, agentsdk.EventPayload{AgentID: "bot-1", Type: agentsdk.EventOffline, Timestamp: time.Now()})
	msg := fakeMQTTMessage{topic: "vantageos/agent/bot-1/event", payload: payload}
	msgHandler(nil, msg)

	if _, ok := ar.OnlineAgents()[model.AgentID("bot-1")]; ok {
		t.Error("bot-1 still online after an offline event")
	}
}
