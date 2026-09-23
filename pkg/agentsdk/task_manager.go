package agentsdk

import (
	"container/list"
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

// TaskAckStatus is the subset of core's model.TaskStatus/agentv1.TaskStatus
// an agent itself ever sends -- core manages the rest (draft, starting,
// expiring, expired, aborting, finishing) on its own. Values match
// proto/agent/v1/agent.proto's TaskAck.status comment ("robot sends:
// CANNOT_START, STARTED, ABORTED, FAILED, FINISHED") in lower_snake_case,
// since this package deliberately has no dependency on that gRPC-specific
// proto package (see this file's own doc comment on TaskStatusPayload).
type TaskAckStatus string

const (
	TaskAckCannotStart TaskAckStatus = "cannot_start"
	TaskAckStarted     TaskAckStatus = "started"
	TaskAckAborted     TaskAckStatus = "aborted"
	TaskAckFailed      TaskAckStatus = "failed"
	TaskAckFinished    TaskAckStatus = "finished"
)

// TaskStatusPayload is what TaskManager publishes to Topic.TaskStatus(taskID)
// -- deliberately a small local JSON struct rather than agentv1.TaskAck:
// this package has no dependency on the gRPC-specific proto package (see
// topic.go's own agent-id-first layout, built to be transport-agnostic).
// Core's MQTT status subscriber (cmd/core) decodes this and constructs an
// agentv1.TaskAck from it before handing off to the same handler the gRPC
// path already uses, so both transports converge on one status-application
// code path.
type TaskStatusPayload struct {
	TaskID       string        `json:"task_id"`
	Status       TaskAckStatus `json:"status"`
	ErrorMessage string        `json:"error_message,omitempty"`
}

// maxTrackedTasks bounds TaskManager's seen-task cache (see seenTasks
// below) -- an agent runs indefinitely, so this must not grow without
// bound. 500 matches this project's other rolling-buffer sizes (e.g. the UI
// live view) as a reasonable "recent history" magnitude for a single
// agent's own task stream, not a precisely tuned figure.
const maxTrackedTasks = 500

// seenTask is one entry in TaskManager's dedup cache: the last status
// published for a task ID, so a redelivered task/new message (QoS 1 can
// redeliver -- see specs/mqtt_telemetry.specs.md Step 16) republishes that
// status instead of calling Execute a second time.
type seenTask struct {
	taskID string
	status TaskStatusPayload
}

type TaskManager struct {
	handlers map[string]*TaskHandler
	topic    Topic

	mu   sync.Mutex // guards mqtt
	mqtt mqtt.Client

	ctx context.Context

	seenMu    sync.Mutex
	seen      map[string]*list.Element // task id -> its node in seenOrder
	seenOrder *list.List               // front = most recently touched
}

// NewTaskManager returns a TaskManager with no mqtt client attached yet —
// call SetClient once the agent has connected, before RunNewTaskListener.
func NewTaskManager(ctx context.Context, topic Topic) *TaskManager {
	return &TaskManager{
		handlers:  make(map[string]*TaskHandler),
		topic:     topic,
		ctx:       ctx,
		seen:      make(map[string]*list.Element),
		seenOrder: list.New(),
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
// NewTaskTopic. It dispatches to the handler registered for the task's
// type -- unless task.ID was already seen, in which case this is a QoS 1
// redelivery (a dropped PUBACK, a reconnect mid-handshake, ...) and the
// handler must not run a second time; the last status this agent published
// for that task is republished instead, so core still gets an ack even if
// the original one was lost.
//
// msg.Ack() is called explicitly (the agent's mqtt client is built with
// SetAutoAckDisabled(true) -- see NewAgent's doc comment on why) only once
// a task's outcome is actually settled: immediately for a permanent
// failure (bad payload, unknown type) or a redelivery of an
// already-finished task, but only after Execute itself returns for a task
// actually being run. Acking any earlier would tell the broker this
// message is fully delivered while the real work is still in flight, which
// is exactly the case a crash needs the broker to still be holding this
// message for -- see specs/mqtt_telemetry.specs.md Step 16's "a task
// survives an agent restart mid-dispatch".
func (t *TaskManager) listenNewTask(_ mqtt.Client, msg mqtt.Message) {
	var task Task
	if err := json.Unmarshal(msg.Payload(), &task); err != nil {
		slog.Error("failed to unmarshal new task payload", "topic", msg.Topic(), "error", err)
		msg.Ack() // permanent failure -- redelivering the same bad payload would never succeed
		return
	}

	if last, redelivered := t.recall(task.ID); redelivered {
		slog.Info("redelivered task, republishing last status instead of re-executing", "task_id", task.ID, "status", last.Status)
		t.publishStatus(last)
		msg.Ack()
		return
	}

	handler, ok := t.handlers[task.Type]
	if !ok {
		slog.Error("no handler registered for task type", "taskType", task.Type, "taskID", task.ID)
		msg.Ack() // permanent failure for this build of the agent
		return
	}

	t.publishStatus(TaskStatusPayload{TaskID: task.ID, Status: TaskAckStarted})

	go func() {
		var final TaskStatusPayload
		if err := (*handler).Execute(t.ctx, msg.Payload()); err != nil {
			slog.Error("task execution failed", "taskType", task.Type, "taskID", task.ID, "error", err)
			final = TaskStatusPayload{TaskID: task.ID, Status: TaskAckFailed, ErrorMessage: err.Error()}
		} else {
			final = TaskStatusPayload{TaskID: task.ID, Status: TaskAckFinished}
		}
		t.remember(final)
		t.publishStatus(final)
		msg.Ack() // only now is the outcome settled -- see this method's own doc comment
	}()
}

// recall reports whether taskID has been seen before and, if so, the last
// status recorded for it -- which may still be zero-valued (TaskAckStarted
// was remembered) if a redelivery races Execute still running from the
// first delivery; publishing "started" again in that case is harmless.
func (t *TaskManager) recall(taskID string) (TaskStatusPayload, bool) {
	t.seenMu.Lock()
	defer t.seenMu.Unlock()

	elem, ok := t.seen[taskID]
	if !ok {
		// First sight: record it now (status filled in by remember once
		// Execute finishes, or immediately below for "started").
		elem = t.seenOrder.PushFront(&seenTask{taskID: taskID, status: TaskStatusPayload{TaskID: taskID, Status: TaskAckStarted}})
		t.seen[taskID] = elem
		t.evictOldestLocked()
		return TaskStatusPayload{}, false
	}

	t.seenOrder.MoveToFront(elem)
	return elem.Value.(*seenTask).status, true
}

// remember updates the cached status for an already-tracked task id (set by
// recall's first-sight branch).
func (t *TaskManager) remember(status TaskStatusPayload) {
	t.seenMu.Lock()
	defer t.seenMu.Unlock()

	elem, ok := t.seen[status.TaskID]
	if !ok {
		return // evicted already, or never tracked -- nothing to update
	}
	elem.Value.(*seenTask).status = status
	t.seenOrder.MoveToFront(elem)
}

// evictOldestLocked drops the least-recently-touched entry once the cache
// is over maxTrackedTasks. Caller must hold seenMu.
func (t *TaskManager) evictOldestLocked() {
	for t.seenOrder.Len() > maxTrackedTasks {
		oldest := t.seenOrder.Back()
		if oldest == nil {
			return
		}
		t.seenOrder.Remove(oldest)
		delete(t.seen, oldest.Value.(*seenTask).taskID)
	}
}

// publishStatus publishes status to Topic.TaskStatus(status.TaskID), QoS 1
// (matching the retain/QoS table in topic.go -- task/<id>/status is not
// retained, only telemetry-schema/event are).
func (t *TaskManager) publishStatus(status TaskStatusPayload) {
	client := t.client()
	if client == nil {
		slog.Error("publishStatus: no mqtt client attached", "task_id", status.TaskID)
		return
	}

	topic, err := t.topic.TaskStatus(status.TaskID)
	if err != nil {
		slog.Error("publishStatus: invalid task id for topic", "task_id", status.TaskID, "error", err)
		return
	}

	b, err := json.Marshal(status)
	if err != nil {
		slog.Error("publishStatus: marshal failed", "task_id", status.TaskID, "error", err)
		return
	}

	token := client.Publish(topic, 1, false, b)
	token.Wait()
	if err := token.Error(); err != nil {
		slog.Error("publishStatus: publish failed", "task_id", status.TaskID, "topic", topic, "error", err)
	}
}

// handleAnyIncoming routes msg to listenNewTask if its topic is
// Topic.NewTask(), ignoring everything else. Registered as the mqtt
// client's *default* publish handler (see NewAgent's doc comment on why
// this matters, alongside the topic-specific Subscribe in
// RunNewTaskListener rather than instead of it) -- SetDefaultPublishHandler
// is configured on ClientOptions before Connect, so it is active from the
// very first byte the connection ever reads, closing the race where a
// broker's resend-on-session-resume arrives before RunNewTaskListener's
// own Subscribe (necessarily issued from OnConnect, i.e. after Connect
// returns) has registered paho's topic-specific local route.
func (t *TaskManager) handleAnyIncoming(c mqtt.Client, msg mqtt.Message) {
	if msg.Topic() == t.topic.NewTask() {
		t.listenNewTask(c, msg)
	}
}

// RunNewTaskListener subscribes to the agent's task/new topic and routes
// incoming tasks to registered handlers via listenNewTask. Call SetClient
// first.
func (t *TaskManager) RunNewTaskListener() error {
	client := t.client()
	if client == nil {
		return errNoClient
	}

	newTaskTopic := t.topic.NewTask()
	token := client.Subscribe(newTaskTopic, 1, t.listenNewTask)
	token.Wait()
	if err := token.Error(); err != nil {
		slog.Error("failed to subscribe to new task topic", "topic", newTaskTopic, "error", err)
		return err
	}

	slog.Info("subscribed to new task topic", "topic", newTaskTopic)
	return nil
}
