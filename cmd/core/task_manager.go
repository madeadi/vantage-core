package main

import "errors"

type Task struct {
	ID      string
	AgentID AgentID
	Type    string // e.g. "GO_TO", "GO_HOME"
	Payload []byte
	Status  TaskStatus
	Result  []byte
}

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "PENDING"
	TaskStatusRunning   TaskStatus = "RUNNING"
	TaskStatusCompleted TaskStatus = "COMPLETED"
	TaskStatusFailed    TaskStatus = "FAILED"
)

type TaskSender interface {
	IsOnline(id AgentID) bool
	SendTask(task *Task) error
}

type TaskManager struct {
	sender       TaskSender
	currentTasks map[AgentID]*Task
}

// StartTask dispatches a task to the agent via its live gRPC stream.
// Returns an error if the agent is offline or already has a running task.
func (t *TaskManager) StartTask(task *Task) error {
	if !t.sender.IsOnline(task.AgentID) {
		return errors.New("cannot start task: agent is offline")
	}
	if _, ok := t.currentTasks[task.AgentID]; ok {
		return errors.New("cannot start task: agent is busy")
	}
	t.currentTasks[task.AgentID] = task
	return t.sender.SendTask(task)
}
