package service

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"vantageos-core/cmd/core/model"
	"vantageos-core/cmd/core/repository"
	"vantageos-core/pkg/agentsdk"
	agentv1 "vantageos-core/proto/agent/v1"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// TaskDispatcher dispatches tasks to agents -- over a live gRPC stream when
// one exists, or by publishing to the agent's MQTT task/new topic
// otherwise (spec Step 16) -- enforcing the single-active-task-per-agent
// rule.
type TaskDispatcher struct {
	registry *AgentRegistry
	taskRepo repository.TaskRepo

	// mu serializes SendTask's busy-check + save so two concurrent
	// dispatches for the same agent cannot both observe "not busy" and
	// both proceed -- see root CLAUDE.md's task dispatch invariants. One
	// mutex for the whole dispatcher, not per-agent: this project's scale
	// target (10 agents) makes that contention immaterial, and a single
	// mutex is much easier to reason about as actually satisfying the
	// invariant than a per-agent map of mutexes would be.
	mu sync.Mutex

	// mqttClient/mqttTopicPrefix are set once core's MQTT connection comes
	// up (see cmd/core/telemetry.go); nil/empty when MQTT is disabled, in
	// which case SendTask falls back to gRPC-only behavior exactly as
	// before this feature existed.
	mqttMu          sync.RWMutex
	mqttClient      mqtt.Client
	mqttTopicPrefix string
}

func NewTaskDispatcher(registry *AgentRegistry, taskRepo repository.TaskRepo) *TaskDispatcher {
	return &TaskDispatcher{registry: registry, taskRepo: taskRepo}
}

// SetMQTTClient attaches core's MQTT client, enabling task/new publish for
// agents with no live gRPC stream. Safe to call once the client connects;
// call again on every reconnect (idempotent -- just replaces the client).
func (d *TaskDispatcher) SetMQTTClient(client mqtt.Client, topicPrefix string) {
	d.mqttMu.Lock()
	defer d.mqttMu.Unlock()
	d.mqttClient = client
	d.mqttTopicPrefix = topicPrefix
}

func (d *TaskDispatcher) mqtt() (mqtt.Client, string) {
	d.mqttMu.RLock()
	defer d.mqttMu.RUnlock()
	return d.mqttClient, d.mqttTopicPrefix
}

// SendTask persists the task and dispatches it: over the agent's live gRPC
// stream if it has one, else by publishing to its MQTT task/new topic
// (QoS 1 -- redelivery is possible, per-task idempotency is the agent
// SDK's job, see pkg/agentsdk/task_manager.go) if MQTT is configured.
// Persistent MQTT sessions (CleanSession=false) mean the broker itself
// queues the publish for a currently-disconnected-but-subscribed agent, so
// unlike the gRPC path this never needs an explicit reconnect-triggered
// resend for that case (see OnReconnect's own doc comment) -- the publish
// either succeeds now or the agent was never provisioned for MQTT at all,
// in which case it fails the same way an offline gRPC-only agent already
// did.
func (d *TaskDispatcher) SendTask(task *model.Task) error {
	d.mu.Lock()
	if active := d.taskRepo.GetActiveTasksByAgent(task.AgentID); len(active) > 0 {
		d.mu.Unlock()
		return errors.New("agent is busy")
	}
	if err := d.taskRepo.SaveTask(task); err != nil {
		d.mu.Unlock()
		slog.Error("Failed to save task to task repository", "err", err)
		return err
	}
	d.mu.Unlock()

	online, err := d.registry.SendToAgent(task.AgentID, grpcTaskMessage(task))
	if err != nil {
		slog.Error("Failed to send task to agent over gRPC", "err", err)
		return err
	}
	if online {
		return nil
	}

	client, topicPrefix := d.mqtt()
	if client == nil {
		return errors.New("no active stream for agent")
	}
	if err := publishNewTask(client, topicPrefix, task); err != nil {
		slog.Error("Failed to publish task to agent over mqtt", "agent_id", task.AgentID, "err", err)
		return errors.New("no active stream for agent")
	}
	return nil
}

func grpcTaskMessage(task *model.Task) *agentv1.ServerMessage {
	return &agentv1.ServerMessage{
		Payload: &agentv1.ServerMessage_Task{Task: &agentv1.Task{
			Id:      task.ID,
			Type:    task.Type,
			Payload: task.Payload,
		}},
	}
}

// publishNewTask publishes task to its agent's Topic.NewTask(), QoS 1, not
// retained (matching topic.go's retain/QoS table -- a new task must not
// replay to an agent that subscribes later than the dispatch, unlike
// telemetry-schema/event).
func publishNewTask(client mqtt.Client, topicPrefix string, task *model.Task) error {
	topic, err := agentsdk.NewTopic(topicPrefix, string(task.AgentID))
	if err != nil {
		return err
	}

	b, err := json.Marshal(agentsdk.Task{ID: task.ID, Type: task.Type, Payload: task.Payload})
	if err != nil {
		return err
	}

	token := client.Publish(topic.NewTask(), 1, false, b)
	token.Wait()
	return token.Error()
}

// OnReconnect re-dispatches an agent's active task after it reconnects --
// the gRPC path's own mechanism, since a gRPC stream carries no queued
// state across a drop the way a persistent MQTT session does. Only ever
// invoked by the gRPC reconnect handler (see cmd/core/grpc/agent_grpc.go),
// so an MQTT-only agent never reaches this; it does not need to, per
// SendTask's own doc comment.
func (d *TaskDispatcher) OnReconnect(agentID model.AgentID) {
	activeTasks := d.taskRepo.GetActiveTasksByAgent(agentID)
	if len(activeTasks) == 0 {
		return
	}

	err := d.SendTask(activeTasks[0]) // send the first active task to the agent
	slog.Info("Reconnected agent", "agentID", agentID, "taskID", activeTasks[0].ID)
	if err != nil {
		slog.Error("Failed to send active task to agent", "err", err)
	}
}

func (d *TaskDispatcher) FindTask(taskID string) (*model.Task, error) {
	return d.taskRepo.GetTaskByID(taskID)
}

func (d *TaskDispatcher) ListTasks(agentID model.AgentID) []*model.Task {
	return d.taskRepo.ListTasks(agentID)
}
