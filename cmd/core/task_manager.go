package main

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

// taskReconnectGracePeriod is how long an in-flight task is held after an agent
// disconnects before being abandoned. Sized to outlast a transient network blip.
const taskReconnectGracePeriod = 30 * time.Second

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

// AgentLifecycleHook is notified when agents connect and disconnect so that
// in-flight tasks can be held and re-delivered across transient reconnects.
type AgentLifecycleHook interface {
	OnAgentDisconnect(agentID AgentID)
	OnAgentReconnect(agentID AgentID)
}

type TaskManager struct {
	mu              sync.Mutex
	sender          TaskSender
	currentTasks    map[AgentID]*Task
	reconnectTimers map[AgentID]*time.Timer
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

// OnAgentDisconnect holds any in-flight task for taskReconnectGracePeriod.
// If the agent reconnects within that window, the task is re-delivered.
// Otherwise, the task is abandoned when the timer fires.
func (t *TaskManager) OnAgentDisconnect(agentID AgentID) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.currentTasks[agentID]; !ok {
		return // no in-flight task; nothing to hold
	}

	// Stop any existing timer before starting a new one.
	if prev, ok := t.reconnectTimers[agentID]; ok {
		prev.Stop()
	}

	t.reconnectTimers[agentID] = time.AfterFunc(taskReconnectGracePeriod, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if task, ok := t.currentTasks[agentID]; ok {
			slog.Warn("task abandoned: agent did not reconnect within grace period",
				"agent_id", agentID, "task_id", task.ID)
			delete(t.currentTasks, agentID)
		}
		delete(t.reconnectTimers, agentID)
	})
}

// OnAgentReconnect cancels the grace-period timer and re-delivers any held task.
func (t *TaskManager) OnAgentReconnect(agentID AgentID) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if timer, ok := t.reconnectTimers[agentID]; ok {
		timer.Stop()
		delete(t.reconnectTimers, agentID)
	}

	task, ok := t.currentTasks[agentID]
	if !ok {
		return // no held task to re-deliver
	}

	if err := t.sender.SendTask(task); err != nil {
		slog.Error("failed to re-deliver task after agent reconnect",
			"agent_id", agentID, "task_id", task.ID, "err", err)
		delete(t.currentTasks, agentID)
		return
	}
	slog.Info("task re-delivered after agent reconnect",
		"agent_id", agentID, "task_id", task.ID)
}
