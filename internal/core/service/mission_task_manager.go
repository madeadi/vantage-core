package service

import (
	"log/slog"
	"vantageos-core/internal/core/model"
	"vantageos-core/internal/core/repository"
	agentPb "vantageos-core/proto/agent/v1"
	missionPb "vantageos-core/proto/mission/v1"

	"github.com/google/uuid"
)

// MissionTaskManager handles the communication of task between mission and agent.
type MissionTaskManager struct {
	mr         *MissionRegistry
	dispatcher *TaskDispatcher
	taskRepo   repository.TaskRepo
}

func NewMissionTaskManager(dispatcher *TaskDispatcher, mr *MissionRegistry, taskRepo repository.TaskRepo) *MissionTaskManager {
	return &MissionTaskManager{dispatcher: dispatcher, mr: mr, taskRepo: taskRepo}
}

func (c *MissionTaskManager) OnCreateTask(missionID string, ct *missionPb.CreateTask) error {
	taskID := uuid.New().String()

	// build the task and send it to the agent
	task := &model.Task{
		ID:             taskID,
		AgentID:        model.AgentID(ct.Requirement.AgentId),
		Type:           ct.Type,
		Payload:        ct.Payload,
		Status:         model.TaskStatusDraft,
		MissionID:      missionID,
		MissionContext: ct.MissionContext.Context,
	}

	if err := c.dispatcher.SendTask(task); err != nil {
		slog.Error("Failed to send task to agent", "err", err)
		return err
	}

	return nil
}

// OnTaskUpdated receives a task update from the agent and then notify the mission
// wrt. the status of the task.
func (c *MissionTaskManager) OnTaskUpdated(ack *agentPb.TaskAck) {
	found, err := c.taskRepo.GetTaskByID(ack.TaskId)
	if err != nil {
		slog.Error("OnTaskUpdated: GetTaskByID failed", "task_id", ack.TaskId, "err", err)
		return
	}

	if found == nil {
		slog.Warn("OnTaskUpdated: task not found", "task_id", ack.TaskId)
		return
	}

	found.Status = model.TaskStatus(ack.Status)
	found.Result = ack.Result

	if err := c.taskRepo.SaveTask(found); err != nil {
		slog.Error("OnTaskUpdated: SaveTask failed", "task_id", ack.TaskId, "err", err)
		return
	}

	if found.MissionID != "" {
		slog.Info("Sending task update to mission", "mission_id", found.MissionID, "task_id", found.ID, "status", found.Status)
		if found.Transient() {
			slog.Info("Transient status, ignoring.")
			return
		}

		stream := c.mr.GetStream(found.MissionID)
		if stream == nil {
			slog.Warn("Mission stream not found", "mission_id", found.MissionID)
			return
		}

		err := stream.Send(&missionPb.MissionServerMessage{
			Payload: &missionPb.MissionServerMessage_TaskStatusUpdate{
				TaskStatusUpdate: &missionPb.TaskStatusUpdate{
					MissionContext: &missionPb.MissionContext{
						Context: found.MissionContext,
					},
					Status: found.MissionTaskStatus(),
				},
			},
		})
		if err != nil {
			slog.Error("Failed to send task update to mission", "mission_id", found.MissionID, "err", err)
			return
		}
	}
}
