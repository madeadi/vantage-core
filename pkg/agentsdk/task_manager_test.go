package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mqttbroker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// newTaskTestBroker starts a real mochi-mqtt broker (allow-all) on an
// ephemeral port. A separate helper from agent_integration_test.go's
// newTestBroker because that one is only built under !race (see its own
// doc comment); these tests don't touch the reconnect path that triggers
// that race, so they run under -race too and need their own,
// unconditionally-built helper.
func newTaskTestBroker(t *testing.T) string {
	t.Helper()

	server := mqttbroker.New(&mqttbroker.Options{InlineClient: true})
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("AddHook: %v", err)
	}
	tcp := listeners.NewTCP(listeners.Config{ID: fmt.Sprintf("task-test-%d", time.Now().UnixNano()), Address: "127.0.0.1:0"})
	if err := server.AddListener(tcp); err != nil {
		t.Fatalf("AddListener: %v", err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })

	return tcp.Address()
}

// statusCollector records every message published to a subscribed topic
// filter, safe for concurrent use (the broker/client invoke the handler
// from their own goroutines).
type statusCollector struct {
	mu   sync.Mutex
	msgs []TaskStatusPayload
}

func (c *statusCollector) handle(_ mqtt.Client, msg mqtt.Message) {
	var p TaskStatusPayload
	if err := json.Unmarshal(msg.Payload(), &p); err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, p)
}

func (c *statusCollector) snapshot() []TaskStatusPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]TaskStatusPayload, len(c.msgs))
	copy(out, c.msgs)
	return out
}

func (c *statusCollector) waitForStatus(t *testing.T, taskID string, status TaskAckStatus, timeout time.Duration) TaskStatusPayload {
	t.Helper()
	deadline := time.After(timeout)
	for {
		for _, p := range c.snapshot() {
			if p.TaskID == taskID && p.Status == status {
				return p
			}
		}
		select {
		case <-deadline:
			t.Fatalf("never saw status %q for task %q within %v (got: %+v)", status, taskID, timeout, c.snapshot())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// countingHandler counts how many times Execute actually ran -- the crux
// of the idempotency test: a redelivered task/new message must not
// increment this a second time.
type countingHandler struct {
	taskType string
	execs    atomic.Int32
	fail     bool
}

func (h *countingHandler) GetTaskType() string      { return h.taskType }
func (h *countingHandler) GetPayloadSchema() string { return "" }
func (h *countingHandler) Execute(_ context.Context, _ any) error {
	h.execs.Add(1)
	if h.fail {
		return fmt.Errorf("intentional failure")
	}
	return nil
}

// newConnectedTaskManager builds a Topic + TaskManager wired to a real,
// already-connected mqtt.Client against brokerAddr, with its new-task
// listener already running.
func newConnectedTaskManager(t *testing.T, brokerAddr, agentID string) (*TaskManager, mqtt.Client, Topic) {
	t.Helper()
	topic, err := NewTopic("vantageos", agentID)
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}

	opts := mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID(agentID)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("connect: %v", token.Error())
	}
	t.Cleanup(func() { client.Disconnect(250) })

	tm := NewTaskManager(context.Background(), topic)
	tm.SetClient(client)
	if err := tm.RunNewTaskListener(); err != nil {
		t.Fatalf("RunNewTaskListener: %v", err)
	}
	return tm, client, topic
}

// newStatusSubscriber connects a second client subscribing to every
// task-status topic for topic's agent, to observe what TaskManager
// publishes without needing to know task ids ahead of time.
func newStatusSubscriber(t *testing.T, brokerAddr string, topic Topic, agentID string) *statusCollector {
	t.Helper()
	opts := mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID(agentID + "-observer")
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("observer connect: %v", token.Error())
	}
	t.Cleanup(func() { client.Disconnect(250) })

	collector := &statusCollector{}
	filter := "vantageos/agent/" + agentID + "/task/+/status"
	if token := client.Subscribe(filter, 1, collector.handle); token.Wait() && token.Error() != nil {
		t.Fatalf("observer subscribe: %v", token.Error())
	}
	return collector
}

func publishNewTaskDirect(t *testing.T, client mqtt.Client, topic Topic, task Task) {
	t.Helper()
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal task: %v", err)
	}
	if token := client.Publish(topic.NewTask(), 1, false, b); token.Wait() && token.Error() != nil {
		t.Fatalf("publish task: %v", token.Error())
	}
}

func TestTaskManagerExecutesNewTaskAndPublishesStartedThenFinished(t *testing.T) {
	brokerAddr := newTaskTestBroker(t)
	tm, _, topic := newConnectedTaskManager(t, brokerAddr, "bot-1")
	statuses := newStatusSubscriber(t, brokerAddr, topic, "bot-1")

	handler := &countingHandler{taskType: "PING"}
	tm.RegisterHandler(handler)

	pubClient := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID("dispatcher"))
	if token := pubClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("dispatcher connect: %v", token.Error())
	}
	t.Cleanup(func() { pubClient.Disconnect(250) })

	publishNewTaskDirect(t, pubClient, topic, Task{ID: "task-1", Type: "PING"})

	statuses.waitForStatus(t, "task-1", TaskAckStarted, 2*time.Second)
	statuses.waitForStatus(t, "task-1", TaskAckFinished, 2*time.Second)

	if got := handler.execs.Load(); got != 1 {
		t.Errorf("Execute ran %d times, want 1", got)
	}
}

func TestTaskManagerFailedExecutionPublishesFailedStatusWithError(t *testing.T) {
	brokerAddr := newTaskTestBroker(t)
	tm, _, topic := newConnectedTaskManager(t, brokerAddr, "bot-1")
	statuses := newStatusSubscriber(t, brokerAddr, topic, "bot-1")

	handler := &countingHandler{taskType: "PING", fail: true}
	tm.RegisterHandler(handler)

	pubClient := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID("dispatcher"))
	if token := pubClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("dispatcher connect: %v", token.Error())
	}
	t.Cleanup(func() { pubClient.Disconnect(250) })

	publishNewTaskDirect(t, pubClient, topic, Task{ID: "task-fail", Type: "PING"})

	failed := statuses.waitForStatus(t, "task-fail", TaskAckFailed, 2*time.Second)
	if failed.ErrorMessage == "" {
		t.Error("failed status has no error_message")
	}
}

// TestTaskManagerRedeliveryDoesNotReExecute is Step 16's literal "duplicate
// delivery executes once": the same task/new payload arriving twice (a
// stand-in for a genuine QoS 1 redelivery, indistinguishable to
// listenNewTask from its perspective -- see this test's own reasoning in
// task_manager.go's doc comment on recall/remember) must not run Execute a
// second time, and must republish the task's current/last status so core
// still gets an ack even if the first one was lost.
func TestTaskManagerRedeliveryDoesNotReExecute(t *testing.T) {
	brokerAddr := newTaskTestBroker(t)
	tm, _, topic := newConnectedTaskManager(t, brokerAddr, "bot-1")
	statuses := newStatusSubscriber(t, brokerAddr, topic, "bot-1")

	handler := &countingHandler{taskType: "PING"}
	tm.RegisterHandler(handler)

	pubClient := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID("dispatcher"))
	if token := pubClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("dispatcher connect: %v", token.Error())
	}
	t.Cleanup(func() { pubClient.Disconnect(250) })

	task := Task{ID: "task-redelivered", Type: "PING"}
	publishNewTaskDirect(t, pubClient, topic, task)
	statuses.waitForStatus(t, task.ID, TaskAckFinished, 2*time.Second)

	if got := handler.execs.Load(); got != 1 {
		t.Fatalf("Execute ran %d times after first delivery, want 1", got)
	}

	// The redelivery: same task id, same payload, published again.
	publishNewTaskDirect(t, pubClient, topic, task)

	// Give it time to land and be (mis)handled if it were going to be.
	deadline := time.After(1 * time.Second)
	<-deadline

	if got := handler.execs.Load(); got != 1 {
		t.Errorf("Execute ran %d times after redelivery, want still 1 (must not re-execute)", got)
	}

	// The redelivery must still republish a status -- count how many
	// "finished" statuses landed in total (>=2: one per delivery).
	finishedCount := 0
	for _, p := range statuses.snapshot() {
		if p.TaskID == task.ID && p.Status == TaskAckFinished {
			finishedCount++
		}
	}
	if finishedCount < 2 {
		t.Errorf("saw %d finished statuses for the redelivered task, want at least 2 (one per delivery)", finishedCount)
	}
}

func TestTaskManagerDistinctTaskTypesEachRunOnce(t *testing.T) {
	brokerAddr := newTaskTestBroker(t)
	tm, _, topic := newConnectedTaskManager(t, brokerAddr, "bot-1")
	statuses := newStatusSubscriber(t, brokerAddr, topic, "bot-1")

	handler := &countingHandler{taskType: "PING"}
	tm.RegisterHandler(handler)

	pubClient := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID("dispatcher"))
	if token := pubClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("dispatcher connect: %v", token.Error())
	}
	t.Cleanup(func() { pubClient.Disconnect(250) })

	for i := 0; i < 3; i++ {
		publishNewTaskDirect(t, pubClient, topic, Task{ID: fmt.Sprintf("task-%d", i), Type: "PING"})
	}
	for i := 0; i < 3; i++ {
		statuses.waitForStatus(t, fmt.Sprintf("task-%d", i), TaskAckFinished, 2*time.Second)
	}

	if got := handler.execs.Load(); got != 3 {
		t.Errorf("Execute ran %d times for 3 distinct tasks, want 3", got)
	}
}
