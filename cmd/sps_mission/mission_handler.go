package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"vantageos-core/pkg/missionsdk"
	missionv1 "vantageos-core/proto/mission/v1"
)

var errNoStream = errors.New("mission stream not bound yet")

// DeliveryMissionHandler drives a single delivery type's task-creation state
// machine over its bound mission stream.
type DeliveryMissionHandler interface {
	Type() string
	Start(delivery Delivery) error
	Bind(stream *missionsdk.MissionStream)
	HandleTaskUpdate(update *missionv1.TaskStatusUpdate)
}

// MissionBase holds the shared agent composition, stream management, and
// delivery-persistence helpers used by both MissionFromKitchen and
// MissionToKitchen. Concrete handlers embed it and implement Type, Start,
// and onCompleted (the flow-specific phase state machine).
type MissionBase struct {
	vehicle VehicleAgent
	gate    GateAgent
	lift    LiftAgent
	kRobot  RobotAgent
	iRobot  RobotAgent
	dr      DeliveryRepository

	mu     sync.Mutex
	stream *missionsdk.MissionStream
}

// Bind attaches the (re)connected mission stream this handler should send
// tasks on. Called by app.go every time the underlying gRPC stream reconnects.
func (b *MissionBase) Bind(stream *missionsdk.MissionStream) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stream = stream
}

func (b *MissionBase) send(ts *missionv1.CreateTask) error {
	b.mu.Lock()
	stream := b.stream
	b.mu.Unlock()

	if stream == nil {
		return errNoStream
	}
	return stream.CreateTask(ts)
}

func (b *MissionBase) sendOrLog(ts *missionv1.CreateTask) {
	if err := b.send(ts); err != nil {
		slog.Error("failed to send task", "mission_context", ts.MissionContext, "err", err)
	}
}

// taskFactory builds the shared envelope for a task belonging to delivery,
// tagged with the step it represents. The caller fills in Type/Payload/
// Requirement via one of the agent builders.
func (b *MissionBase) taskFactory(delivery Delivery, phase string) *missionv1.CreateTask {
	payload := map[string]interface{}{
		"phase": phase,
	}
	ctx, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal task context", "delivery_id", delivery.ID, "phase", phase, "err", err)
		return nil
	}

	return &missionv1.CreateTask{
		MissionContext: &missionv1.MissionContext{
			Id:      delivery.ID,
			Context: ctx,
		},
	}
}

func (b *MissionBase) updateDeliveryStatus(delivery Delivery, status string) Delivery {
	delivery.Status = status
	delivery.UpdatedAt = time.Now()
	if err := b.dr.UpdateDelivery(delivery); err != nil {
		slog.Error("failed to persist delivery status", "delivery_id", delivery.ID, "status", status, "err", err)
	}
	return delivery
}

func (b *MissionBase) fail(delivery Delivery, status string, reason string) {
	slog.Info("failing delivery", "delivery_id", delivery.ID, "reason", reason)
	delivery.Status = status
	delivery.FailureReason = reason
	delivery.UpdatedAt = time.Now()
	if err := b.dr.UpdateDelivery(delivery); err != nil {
		slog.Error("failed to persist delivery failure", "delivery_id", delivery.ID, "err", err)
	}
}

// handleTaskUpdate is the shared status-update logic. It loads the delivery,
// skips terminal ones, routes FAILED to fail(), and delegates COMPLETED to
// the provided onCompleted callback (the flow-specific phase state machine).
func (b *MissionBase) handleTaskUpdate(update *missionv1.TaskStatusUpdate, failedStatus string, onCompleted func(Delivery, string)) {
	delivery, err := b.dr.FindDeliveryByID(update.GetMissionContext().GetId())
	if err != nil {
		slog.Error("handleTaskUpdate: delivery not found", "mission_id", update.GetMissionContext().GetId(), "err", err)
		return
	}

	switch delivery.Status {
	case "CANCELLED", "DONE", "FAILED":
		slog.Info("ignoring task update for terminal delivery", "delivery_id", delivery.ID, "status", delivery.Status)
		return
	}

	switch update.Status {
	case missionv1.MissionTaskStatus_MISSION_TASK_STATUS_FAILED:
		b.fail(delivery, failedStatus, "task "+update.GetTaskContext().GetId()+" ended with status FAILED")
	case missionv1.MissionTaskStatus_MISSION_TASK_STATUS_COMPLETED:
		var payload map[string]interface{}
		if err := json.Unmarshal(update.MissionContext.Context, &payload); err != nil {
			slog.Error("failed to unmarshal task context", "task_id", update.TaskContext.Id, "err", err)
			return
		}
		phase, _ := payload["phase"].(string)
		onCompleted(delivery, phase)
	default:
		slog.Info("task status not handled", "task_id", update.TaskContext.Id, "status", update.Status)
	}
}
