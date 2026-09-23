package service

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
	"vantageos-core/cmd/core/model"
	"vantageos-core/cmd/core/repository"
	"vantageos-core/pkg/agentsdk"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mqttbroker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// TestSendTaskConcurrentDispatchesOnlyOneSucceeds is root CLAUDE.md's task
// dispatch invariant, made deterministic rather than merely probable: N
// concurrent SendTask calls for the same agent must result in exactly one
// saved (active) task, whatever happens to delivery. Before the fix
// bundled with this test (a single mutex around the busy-check + save),
// this was a real time-of-check-to-time-of-use race -- not one -race would
// catch, since GetActiveTasksByAgent and SaveTask each lock and unlock
// independently; only holding one lock across both closes the window.
func TestSendTaskConcurrentDispatchesOnlyOneSucceeds(t *testing.T) {
	ar := NewAgentRegistry(nil, "localhost:9090")
	repo := repository.NewTaskRepoMemory()
	d := NewTaskDispatcher(ar, repo)

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := &model.Task{
				ID: fmt.Sprintf("task-%d", i), AgentID: "bot-1", Type: "PING",
				Status: model.TaskStatusDraft,
			}
			_ = d.SendTask(task) // every call fails to deliver (no transport) -- only the save matters here
		}(i)
	}
	wg.Wait()

	saved := repo.ListTasks("bot-1")
	if len(saved) != 1 {
		ids := make([]string, len(saved))
		for i, task := range saved {
			ids[i] = task.ID
		}
		t.Errorf("repo has %d tasks for bot-1 after %d concurrent SendTask calls, want exactly 1 (got: %v)", len(saved), n, ids)
	}
}

func TestSendTaskRejectsWhenAlreadyBusy(t *testing.T) {
	ar := NewAgentRegistry(nil, "localhost:9090")
	repo := repository.NewTaskRepoMemory()
	d := NewTaskDispatcher(ar, repo)

	first := &model.Task{ID: "task-1", AgentID: "bot-1", Type: "PING", Status: model.TaskStatusDraft}
	_ = d.SendTask(first) // saved regardless of delivery outcome -- see doc comment on SendTask

	second := &model.Task{ID: "task-2", AgentID: "bot-1", Type: "PING", Status: model.TaskStatusDraft}
	err := d.SendTask(second)
	if err == nil || err.Error() != "agent is busy" {
		t.Errorf("SendTask while busy = %v, want \"agent is busy\"", err)
	}
}

func TestSendTaskFallsBackToMQTTWhenNoGRPCStream(t *testing.T) {
	brokerAddr := newDispatcherTestBroker(t)

	ar := NewAgentRegistry(nil, "localhost:9090")
	repo := repository.NewTaskRepoMemory()
	d := NewTaskDispatcher(ar, repo)

	pubOpts := mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID("core-under-test")
	pubClient := mqtt.NewClient(pubOpts)
	if token := pubClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("core client connect: %v", token.Error())
	}
	t.Cleanup(func() { pubClient.Disconnect(250) })
	d.SetMQTTClient(pubClient, "vantageos")

	received := make(chan agentsdk.Task, 1)
	subOpts := mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID("agent-under-test")
	subClient := mqtt.NewClient(subOpts)
	if token := subClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("agent client connect: %v", token.Error())
	}
	t.Cleanup(func() { subClient.Disconnect(250) })
	if token := subClient.Subscribe("vantageos/agent/bot-1/task/new", 1, func(_ mqtt.Client, msg mqtt.Message) {
		var task agentsdk.Task
		if err := json.Unmarshal(msg.Payload(), &task); err != nil {
			t.Errorf("unmarshal published task: %v", err)
			return
		}
		received <- task
	}); token.Wait() && token.Error() != nil {
		t.Fatalf("agent subscribe: %v", token.Error())
	}

	task := &model.Task{ID: "task-mqtt-1", AgentID: "bot-1", Type: "GO_TO", Payload: []byte(`{"x":1,"y":2}`), Status: model.TaskStatusDraft}
	if err := d.SendTask(task); err != nil {
		t.Fatalf("SendTask: %v", err)
	}

	select {
	case got := <-received:
		if got.ID != "task-mqtt-1" || got.Type != "GO_TO" {
			t.Errorf("received task = %+v, want id=task-mqtt-1 type=GO_TO", got)
		}
		if string(got.Payload) != `{"x":1,"y":2}` {
			t.Errorf("received payload = %s, want {\"x\":1,\"y\":2}", got.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task was never published to the agent's task/new topic")
	}
}

func TestSendTaskFailsWhenNeitherGRPCNorMQTTAvailable(t *testing.T) {
	ar := NewAgentRegistry(nil, "localhost:9090")
	repo := repository.NewTaskRepoMemory()
	d := NewTaskDispatcher(ar, repo)

	task := &model.Task{ID: "task-1", AgentID: "bot-1", Type: "PING", Status: model.TaskStatusDraft}
	err := d.SendTask(task)
	if err == nil || err.Error() != "no active stream for agent" {
		t.Errorf("SendTask with no transport = %v, want \"no active stream for agent\"", err)
	}
}

func newDispatcherTestBroker(t *testing.T) string {
	t.Helper()
	server := mqttbroker.New(&mqttbroker.Options{InlineClient: true})
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("AddHook: %v", err)
	}
	tcp := listeners.NewTCP(listeners.Config{ID: fmt.Sprintf("dispatcher-test-%d", time.Now().UnixNano()), Address: "127.0.0.1:0"})
	if err := server.AddListener(tcp); err != nil {
		t.Fatalf("AddListener: %v", err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	return tcp.Address()
}
