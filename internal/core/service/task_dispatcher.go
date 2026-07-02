package service

import (
	"errors"
	"log/slog"
	"vantageos-core/internal/core/model"
	"vantageos-core/internal/core/repository"
	agentv1 "vantageos-core/proto/agent/v1"
)

// TaskDispatcher dispatches tasks to agents over their live gRPC stream,
// enforcing the single-active-task-per-agent rule.
type TaskDispatcher struct {
	registry *AgentRegistry
	taskRepo repository.TaskRepo
}

func NewTaskDispatcher(registry *AgentRegistry, taskRepo repository.TaskRepo) *TaskDispatcher {
	return &TaskDispatcher{registry: registry, taskRepo: taskRepo}
}

// SendTask persists the task and pushes it down the agent's live gRPC stream.
func (d *TaskDispatcher) SendTask(task *model.Task) error {
	if active := d.taskRepo.GetActiveTasksByAgent(task.AgentID); len(active) > 0 {
		return errors.New("agent is busy")
	}
	if err := d.taskRepo.SaveTask(task); err != nil {
		slog.Error("Failed to save task to task repository", "err", err)
		return err
	}

	msg := &agentv1.ServerMessage{
		Payload: &agentv1.ServerMessage_Task{Task: &agentv1.Task{
			Id:      task.ID,
			Type:    task.Type,
			Payload: task.Payload,
		}},
	}
	online, err := d.registry.SendToAgent(task.AgentID, msg)
	if !online {
		return errors.New("no active stream for agent")
	}
	if err != nil {
		slog.Error("Failed to send task to agent", "err", err)
		return err
	}
	return nil
}

// OnReconnect re-dispatches an agent's active task after it reconnects.
func (d *TaskDispatcher) OnReconnect(agentID model.AgentID) {
	activeTasks := d.taskRepo.GetActiveTasksByAgent(agentID)
	if len(activeTasks) == 0 {
		return
	}

	err := d.SendTask(activeTasks[0]) // send the first active task to the agent
	slog.Info("Reconnected agent", "agentID", agentID, "taskID", activeTasks[0].ID)
	if err != nil {
		slog.Error("Failed to send active task to agent", "err", err)
	}
}

func (d *TaskDispatcher) ListTasks(agentID model.AgentID) []*model.Task {
	return d.taskRepo.ListTasks(agentID)
}
