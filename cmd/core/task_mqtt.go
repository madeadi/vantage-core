package main

import (
	"encoding/json"
	"log/slog"

	"vantageos-core/cmd/core/model"
	"vantageos-core/cmd/core/service"
	"vantageos-core/pkg/agentsdk"
	agentv1 "vantageos-core/proto/agent/v1"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// taskUpdatedHandler is the minimal interface both the gRPC and MQTT
// inbound paths need. Satisfied by *service.MissionTaskManager, the one
// place a task ack is actually applied: it looks up the task by id and
// sets its status/result, so handling the same ack twice has no
// additional effect -- exactly the idempotency spec Step 16 asks for on
// the receiving end (QoS 1 can redeliver a status update same as it can a
// new task).
type taskUpdatedHandler interface {
	OnTaskUpdated(ack *agentv1.TaskAck)
}

// subscribeTaskControlPlane wires the two MQTT topics Step 16 adds beyond
// telemetry: task/status (agent -> core acks) and event (agent presence,
// via the LWT + birth message pkg/agentsdk.NewAgent already publishes).
// Called from startTelemetryIngest's OnConnect so both share core's one
// MQTT connection rather than opening a second one to the same broker.
func subscribeTaskControlPlane(client mqtt.Client, topicPrefix string, ar *service.AgentRegistry, tuHandler taskUpdatedHandler) {
	statusFilter := agentsdk.TaskStatusFilter(topicPrefix)
	if token := client.Subscribe(statusFilter, 1, taskStatusHandler(topicPrefix, tuHandler)); token.Wait() && token.Error() != nil {
		slog.Error("task control plane: subscribe to task/status failed", "filter", statusFilter, "error", token.Error())
	}

	eventFilter := agentsdk.EventFilter(topicPrefix)
	if token := client.Subscribe(eventFilter, 1, presenceHandler(topicPrefix, ar)); token.Wait() && token.Error() != nil {
		slog.Error("task control plane: subscribe to event failed", "filter", eventFilter, "error", token.Error())
	}
}

// taskStatusHandler decodes an agentsdk.TaskStatusPayload off a
// task/<id>/status message and applies it through tuHandler, having built
// an agentv1.TaskAck from it -- the same status-application code path the
// gRPC StreamTasks handler already uses (cmd/core/grpc/agent_grpc.go),
// so both transports converge on one place that decides what a status
// update means.
func taskStatusHandler(topicPrefix string, tuHandler taskUpdatedHandler) mqtt.MessageHandler {
	return func(_ mqtt.Client, msg mqtt.Message) {
		agentID, ok := agentsdk.AgentIDFromTopic(topicPrefix, msg.Topic())
		if !ok {
			slog.Warn("task control plane: task/status topic did not match the agent-id-first layout", "topic", msg.Topic())
			return
		}
		taskID, ok := agentsdk.TaskIDFromStatusTopic(topicPrefix, msg.Topic())
		if !ok {
			slog.Warn("task control plane: task/status topic missing a task id", "topic", msg.Topic())
			return
		}

		var status agentsdk.TaskStatusPayload
		if err := json.Unmarshal(msg.Payload(), &status); err != nil {
			slog.Error("task control plane: bad task/status payload", "agent_id", agentID, "task_id", taskID, "error", err)
			return
		}

		tuHandler.OnTaskUpdated(&agentv1.TaskAck{
			TaskId:       taskID,
			Status:       taskAckStatusToProto(status.Status),
			ErrorMessage: status.ErrorMessage,
		})
	}
}

func taskAckStatusToProto(s agentsdk.TaskAckStatus) agentv1.TaskStatus {
	switch s {
	case agentsdk.TaskAckCannotStart:
		return agentv1.TaskStatus_TASK_STATUS_CANNOT_START
	case agentsdk.TaskAckStarted:
		return agentv1.TaskStatus_TASK_STATUS_STARTED
	case agentsdk.TaskAckAborted:
		return agentv1.TaskStatus_TASK_STATUS_ABORTED
	case agentsdk.TaskAckFailed:
		return agentv1.TaskStatus_TASK_STATUS_FAILED
	case agentsdk.TaskAckFinished:
		return agentv1.TaskStatus_TASK_STATUS_FINISHED
	default:
		return agentv1.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

// presenceHandler updates ar's MQTT presence tracking (spec Step 16) from
// an agent's retained lifecycle event -- online/registered mark it alive,
// offline (graceful disconnect or the broker delivering the LWT) marks it
// gone immediately, ahead of the staleness timeout.
func presenceHandler(topicPrefix string, ar *service.AgentRegistry) mqtt.MessageHandler {
	return func(_ mqtt.Client, msg mqtt.Message) {
		agentID, ok := agentsdk.AgentIDFromTopic(topicPrefix, msg.Topic())
		if !ok {
			return
		}

		var event agentsdk.EventPayload
		if err := json.Unmarshal(msg.Payload(), &event); err != nil {
			slog.Error("task control plane: bad event payload", "agent_id", agentID, "error", err)
			return
		}

		switch event.Type {
		case agentsdk.EventOffline:
			ar.MarkMQTTOffline(model.AgentID(agentID))
		case agentsdk.EventOnline, agentsdk.EventRegistered:
			ar.MarkMQTTOnline(model.AgentID(agentID))
		}
	}
}
