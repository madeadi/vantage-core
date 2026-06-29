package main

import (
	"sync"
	"time"
)

type TaskRepoMemory struct {
	mu sync.RWMutex
	ts map[string]*Task
}

func (t *TaskRepoMemory) GetTaskByID(taskID string) (*Task, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	task := t.ts[taskID]
	if task == nil {
		return nil, nil
	}
	cp := *task
	return &cp, nil
}

func NewTaskRepoMemory() *TaskRepoMemory {
	return &TaskRepoMemory{ts: make(map[string]*Task)}
}

func (t *TaskRepoMemory) SaveTask(task *Task) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	found := t.ts[task.ID]
	if found != nil {
		found.CopyMetadata(task)
	} else {
		task.ReceivedAt = time.Now()
	}

	t.ts[task.ID] = task
	return nil
}

func (t *TaskRepoMemory) GetActiveTasksByAgent(agentID AgentID) []*Task {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var tasks []*Task
	for _, task := range t.ts {
		if task.AgentID != agentID {
			continue
		}
		if task.Final() {
			continue
		}
		cp := *task
		tasks = append(tasks, &cp)
	}

	return tasks
}

func (t *TaskRepoMemory) ListTasks(agentID AgentID) []*Task {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var tasks []*Task
	for _, task := range t.ts {
		if agentID != "" && task.AgentID != agentID {
			continue
		}
		cp := *task
		tasks = append(tasks, &cp)
	}
	return tasks
}
