package service

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"vantageos-core/pkg/agentsdk/server"
	agentv1 "vantageos-core/proto/agent/v1"
)

// TaskAckFn delivers a TaskAck to the stream layer. Must be safe for concurrent calls.
type TaskAckFn func(ack *agentv1.TaskAck)

// TaskHandler handles a specific task type.
type TaskHandler interface {
	GetTaskType() string
	Execute(ctx context.Context, task *agentv1.Task) (result []byte, err error)
}

type runningTask struct {
	task   *agentv1.Task
	cancel context.CancelFunc
}

// AgentTaskManager routes incoming tasks to registered handlers and manages
// the lifecycle of the currently running task.
type AgentTaskManager struct {
	handlers map[string]TaskHandler
	mu       sync.Mutex
	current  *runningTask
}

// NewAgentTaskManager returns an AgentTaskManager with the given handlers.
func NewAgentTaskManager(handlers ...TaskHandler) *AgentTaskManager {
	m := make(map[string]TaskHandler, len(handlers))
	for _, h := range handlers {
		m[h.GetTaskType()] = h
	}
	return &AgentTaskManager{handlers: m}
}

// HandleTask starts task in a goroutine if the manager is idle and the task
// type is registered. onAck is called with STARTED immediately, then with
// FINISHED or FAILED when the handler returns. Returns an error (busy or
// unknown type) that the caller should surface as a CANNOT_START ack.
func (tm *AgentTaskManager) HandleTask(parentCtx context.Context, task *agentv1.Task, onAck TaskAckFn) error {
	tm.mu.Lock()

	if tm.current != nil {
		running := tm.current.task
		tm.mu.Unlock()
		slog.Error("task manager busy", "running_id", running.Id, "running_type", running.Type)
		return fmt.Errorf("task manager is busy with task %s (%s)", running.Id, running.Type)
	}

	handler, ok := tm.handlers[task.Type]
	if !ok {
		tm.mu.Unlock()
		return fmt.Errorf("task type %s is not registered", task.Type)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	tm.current = &runningTask{task: task, cancel: cancel}
	tm.mu.Unlock()

	go func() {
		onAck(&agentv1.TaskAck{
			TaskId: task.Id,
			Status: agentv1.TaskStatus_TASK_STATUS_STARTED,
		})

		result, err := handler.Execute(ctx, task)

		// clearTask checks ownership before clearing state. If AbortCurrentTask
		// already ran, it returns false — abort path already sent ABORTED.
		if tm.clearTask(task.Id, cancel) {
			if err != nil {
				onAck(&agentv1.TaskAck{
					TaskId:       task.Id,
					Status:       agentv1.TaskStatus_TASK_STATUS_FAILED,
					ErrorMessage: err.Error(),
				})
			} else {
				onAck(&agentv1.TaskAck{
					TaskId: task.Id,
					Status: agentv1.TaskStatus_TASK_STATUS_FINISHED,
					Result: result,
				})
			}
		}
	}()

	return nil
}

// AbortCurrentTask cancels the task with taskID if it is currently running.
// Returns true when cancelled; the caller should then send TASK_STATUS_ABORTED.
// Returns false if no matching task is running (stale abort or already done).
func (tm *AgentTaskManager) AbortCurrentTask(taskID string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.current == nil || tm.current.task.Id != taskID {
		return false
	}

	tm.current.cancel()
	tm.current = nil
	return true
}

// Skills returns the list of skills this manager can handle, ready for registration.
func (tm *AgentTaskManager) Skills() []server.Skill {
	skills := make([]server.Skill, 0, len(tm.handlers))
	for t := range tm.handlers {
		skills = append(skills, server.Skill{Name: t, Payload: server.SkillPayload{Name: t}})
	}
	return skills
}

// IsTaskRunning reports whether a task is currently executing.
func (tm *AgentTaskManager) IsTaskRunning() bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.current != nil
}

// clearTask atomically verifies taskID still owns the active slot, cancels it,
// and clears state. Returns false if AbortCurrentTask already cleared state.
func (tm *AgentTaskManager) clearTask(taskID string, cancel context.CancelFunc) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.current == nil || tm.current.task.Id != taskID {
		return false
	}

	cancel()
	tm.current = nil
	return true
}
