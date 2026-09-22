package agentsdk

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type TaskHandler interface {
	GetTaskType() string
	Execute(ctx context.Context, rawPayload any) error
	GetPayloadSchema() string
}

type TaskManager struct {
	handlers     map[string]*TaskHandler
	NewTaskTopic string

	mu   sync.Mutex // guards mqtt
	mqtt mqtt.Client

	ctx context.Context
}

// NewTaskManager returns a TaskManager with no mqtt client attached yet —
// call SetClient once the agent has connected, before RunNewTaskListener.
func NewTaskManager(ctx context.Context, newTaskTopic string) *TaskManager {
	return &TaskManager{
		handlers:     make(map[string]*TaskHandler),
		NewTaskTopic: newTaskTopic,
		ctx:          ctx,
	}
}

// SetClient attaches the mqtt client RunNewTaskListener and any future
// resubscription will use. Safe to call concurrently with client(), but not
// intended to be called concurrently with itself from multiple goroutines
// for the same TaskManager.
func (t *TaskManager) SetClient(c mqtt.Client) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mqtt = c
}

func (t *TaskManager) client() mqtt.Client {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mqtt
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
// registered handlers via listenNewTask. Call SetClient first.
func (t *TaskManager) RunNewTaskListener() error {
	client := t.client()
	if client == nil {
		return errNoClient
	}

	token := client.Subscribe(t.NewTaskTopic, 1, t.listenNewTask)
	token.Wait()
	if err := token.Error(); err != nil {
		slog.Error("failed to subscribe to new task topic", "topic", t.NewTaskTopic, "error", err)
		return err
	}

	slog.Info("subscribed to new task topic", "topic", t.NewTaskTopic)
	return nil
}
