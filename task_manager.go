package main

import (
	"errors"
)

type Task struct {
	ID      string
	AgentID AgentID
	Type    string // e.g. "GO_TO", "GO_HOME"
	Payload []byte
	Status  TaskStatus
}

type TaskStatus string

type Onliner interface {
	IsOnline(id AgentID) bool
}

type Publisher interface {
	Publish(topic string, data []byte)
}

type TaskManager struct {
	onliner      Onliner
	publisher    Publisher
	currentTasks map[AgentID]*Task

	newTaskTopic string
}

// StartTask starts the task and send it to the appropriate agent.
// Cannot send the task when the agent is: offline or busy
func (t *TaskManager) StartTask(task *Task) error {
	if !t.onliner.IsOnline(task.AgentID) {
		return errors.New("cannot start task: agent is offline")
	}

	if _, ok := t.currentTasks[task.AgentID]; ok {
		return errors.New("cannot start task: agent is busy")
	}

	t.currentTasks[task.AgentID] = task
	t.publisher.Publish(t.newTaskTopic+"/agentId", task.Payload)

	return nil
}
