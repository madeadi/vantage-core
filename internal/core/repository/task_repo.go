package repository

import "vantageos-core/internal/core/model"

type TaskRepo interface {
	SaveTask(task *model.Task) error
	GetActiveTasksByAgent(agentID model.AgentID) []*model.Task
	ListTasks(agentID model.AgentID) []*model.Task
	GetTaskByID(taskID string) (*model.Task, error)
}
