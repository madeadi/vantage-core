package main

import (
	"log/slog"

	agentv1 "vantageos-core/proto/agent/v1"
)

type TaskUpdatedHandlerMemory struct {
	TaskRepo TaskRepo
}

func NewTaskUpdatedHandlerMemory(taskRepo TaskRepo) *TaskUpdatedHandlerMemory {
	return &TaskUpdatedHandlerMemory{
		TaskRepo: taskRepo,
	}
}

func (t TaskUpdatedHandlerMemory) OnTaskUpdated(ack *agentv1.TaskAck) {
	found, err := t.TaskRepo.GetTaskByID(ack.TaskId)
	if err != nil {
		slog.Error("OnTaskUpdated: GetTaskByID failed", "task_id", ack.TaskId, "err", err)
		return
	}
	if found == nil {
		slog.Warn("OnTaskUpdated: task not found", "task_id", ack.TaskId)
		return
	}

	found.Status = TaskStatus(ack.Status)
	found.Result = ack.Result

	if err := t.TaskRepo.SaveTask(found); err != nil {
		slog.Error("OnTaskUpdated: SaveTask failed", "task_id", ack.TaskId, "err", err)
	}
}
