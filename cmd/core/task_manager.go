package main

import (
	"errors"
	"sync"
)

type Task struct {
	ID        string
	AgentID   AgentID
	Type      string // e.g. "GO_TO", "GO_HOME"
	Payload   []byte
	Status    TaskStatus
	Result    []byte
	MissionID MissionID
}

type TaskStatus int

const (
	TaskStatusUnspecified TaskStatus = iota // 0
	TaskStatusDraft                         // 1
	TaskStatusStarting                      // 2
	TaskStatusCannotStart                   // 3
	TaskStatusStarted                       // 4
	TaskStatusExpiring                      // 5
	TaskStatusExpired                       // 6
	TaskStatusAborting                      // 7
	TaskStatusAborted                       // 8
	TaskStatusFailed                        // 9
	TaskStatusFinishing                     // 10
	TaskStatusFinished                      // 11
)

// Transient returns true if the task is in a transient state (not yet getting confirmation from the agentsdk).
func (t *Task) Transient() bool {
	switch t.Status {
	case TaskStatusUnspecified, TaskStatusDraft, TaskStatusStarting, TaskStatusExpiring, TaskStatusAborting, TaskStatusFinishing:
		return true
	default:
		return false
	}
}

type TaskSender interface {
	IsOnline(id AgentID) bool
	SendTask(task *Task) error
}

type TaskManager struct {
	mu           sync.Mutex
	sender       TaskSender
	currentTasks map[AgentID]*Task
}

// StartTask dispatches a task to the agentsdk via its live gRPC stream.
// Returns an error if the agentsdk is offline or already has a running task.
func (t *TaskManager) StartTask(task *Task) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.sender.IsOnline(task.AgentID) {
		return errors.New("cannot start task: agentsdk is offline")
	}
	if _, ok := t.currentTasks[task.AgentID]; ok {
		return errors.New("cannot start task: agentsdk is busy")
	}
	t.currentTasks[task.AgentID] = task
	return t.sender.SendTask(task)
}
