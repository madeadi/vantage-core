package agentsdk

import (
	"context"
	"encoding/json"
	"log/slog"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type TaskHandler interface {
	GetTaskType() string
	Execute(ctx context.Context, rawPayload any) error
	GetPayloadSchema() string
}

type TaskManager struct {
	handlers     map[string]*TaskHandler
	mqtt         mqtt.Client
	NewTaskTopic string

	ctx context.Context
}

func NewTaskManager(ctx context.Context, mqttClient mqtt.Client, newTaskTopic string) *TaskManager {
	return &TaskManager{
		handlers:     make(map[string]*TaskHandler),
		mqtt:         mqttClient,
		NewTaskTopic: newTaskTopic,
		ctx:          ctx,
	}
}

func (t *TaskManager) RegisterHandler(handler TaskHandler) *TaskManager {
	slog.Info("Registering task handler", "taskType", handler.GetTaskType())
	t.handlers[handler.GetTaskType()] = &handler
	return t
}

// listenNewTask is the mqtt.MessageHandler invoked for every message on
// NewTaskTopic. It dispatches to the handler registered for the task's type.
func (t *TaskManager) listenNewTask(_ mqtt.Client, msg mqtt.Message) {
	var task Task
	if err := json.Unmarshal(msg.Payload(), &task); err != nil {
		slog.Error("failed to unmarshal new task payload", "topic", msg.Topic(), "error", err)
		return
	}

	handler, ok := t.handlers[task.Type]
	if !ok {
		slog.Error("no handler registered for task type", "taskType", task.Type, "taskID", task.ID)
		return
	}

	go func() {
		if err := (*handler).Execute(t.ctx, msg.Payload()); err != nil {
			slog.Error("task execution failed", "taskType", task.Type, "taskID", task.ID, "error", err)
		}
	}()
}

// RunNewTaskListener subscribes to NewTaskTopic and routes incoming tasks to
// registered handlers via listenNewTask.
func (t *TaskManager) RunNewTaskListener() error {
	token := t.mqtt.Subscribe(t.NewTaskTopic, 0, t.listenNewTask)
	token.Wait()
	if err := token.Error(); err != nil {
		slog.Error("failed to subscribe to new task topic", "topic", t.NewTaskTopic, "error", err)
		return err
	}

	slog.Info("subscribed to new task topic", "topic", t.NewTaskTopic)
	return nil
}
