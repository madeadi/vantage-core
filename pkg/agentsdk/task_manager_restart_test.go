package agentsdk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// blockingHandler never returns from Execute until unblock is closed --
// standing in for a task whose agent process disappears mid-execution.
type blockingHandler struct {
	taskType string
	execs    atomic.Int32
	unblock  chan struct{}
}

func (h *blockingHandler) GetTaskType() string      { return h.taskType }
func (h *blockingHandler) GetPayloadSchema() string { return "" }
func (h *blockingHandler) Execute(_ context.Context, _ any) error {
	h.execs.Add(1)
	<-h.unblock
	return nil
}

// TestTaskSurvivesAgentRestartMidDispatch is Step 16's other literal "Done
// when": a task dispatched while an agent is executing it, followed by
// that agent process disappearing before finishing, must not be lost --
// the broker must still hold and redeliver it once a fresh agent process
// (same agent id, same persistent session) reconnects, since the original
// message was never acked (see NewAgent/listenNewTask's doc comments on
// why acking is deferred until a task's outcome is actually settled).
//
// A clean client.Disconnect() is used to end the first connection rather
// than severing the TCP connection at a lower level: per the MQTT spec, a
// persistent session (CleanSession=false) retains its unacked QoS 1 state
// regardless of *how* the previous connection ended, not only on an
// unclean one -- so this is a faithful, simpler stand-in for a crash, not
// a shortcut that dodges the interesting part of the scenario.
func TestTaskSurvivesAgentRestartMidDispatch(t *testing.T) {
	brokerAddr := newTaskTestBroker(t)
	topic, err := NewTopic("vantageos", "bot-1")
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}

	// "Process 1": receives the task and gets stuck mid-execution. The
	// handler is registered before Connect (not just SetDefaultPublishHandler
	// before Connect) -- listenNewTask looks up t.handlers synchronously off
	// the same message that could arrive the instant the connection is
	// live, so registering it any later leaves a window where an
	// already-registered default publish handler still finds no handler for
	// the task's type. This mirrors the real requirement documented in
	// cmd/mqtt-agent-example/main.go.
	firstTM := NewTaskManager(context.Background(), topic)
	blocked := &blockingHandler{taskType: "LONG", unblock: make(chan struct{})}
	t.Cleanup(func() { close(blocked.unblock) }) // let the leaked goroutine finish, if it's still running
	firstTM.RegisterHandler(blocked)

	firstOpts := mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).
		SetClientID("bot-1").SetCleanSession(false).SetAutoAckDisabled(true).
		SetDefaultPublishHandler(firstTM.handleAnyIncoming) // see NewAgent's doc comment on why this must be set before Connect
	firstClient := mqtt.NewClient(firstOpts)
	if token := firstClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("first connect: %v", token.Error())
	}

	firstTM.SetClient(firstClient)
	if err := firstTM.RunNewTaskListener(); err != nil {
		t.Fatalf("first RunNewTaskListener: %v", err)
	}

	statuses := newStatusSubscriber(t, brokerAddr, topic, "bot-1")

	pubClient := mqtt.NewClient(mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).SetClientID("dispatcher"))
	if token := pubClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("dispatcher connect: %v", token.Error())
	}
	t.Cleanup(func() { pubClient.Disconnect(250) })

	task := Task{ID: "long-task", Type: "LONG"}
	publishNewTaskDirect(t, pubClient, topic, task)

	statuses.waitForStatus(t, task.ID, TaskAckStarted, 2*time.Second)
	if got := blocked.execs.Load(); got != 1 {
		t.Fatalf("Execute ran %d times before the simulated crash, want 1", got)
	}

	// "Process 1" disappears mid-execution -- the task/new message it
	// received is still unacked (Execute never returned, so
	// listenNewTask's goroutine never reached msg.Ack()).
	firstClient.Disconnect(250)

	// "Process 2": a fresh TaskManager -- no memory of "long-task" at all,
	// exactly like a real restarted process -- reconnecting with the same
	// client id and a persistent session.
	secondTM := NewTaskManager(context.Background(), topic)
	rerun := &countingHandler{taskType: "LONG"} // this one actually finishes
	secondTM.RegisterHandler(rerun)

	secondOpts := mqtt.NewClientOptions().AddBroker("tcp://" + brokerAddr).
		SetClientID("bot-1").SetCleanSession(false).SetAutoAckDisabled(true).
		SetDefaultPublishHandler(secondTM.handleAnyIncoming)
	secondClient := mqtt.NewClient(secondOpts)
	if token := secondClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("second connect: %v", token.Error())
	}
	t.Cleanup(func() { secondClient.Disconnect(250) })

	secondTM.SetClient(secondClient)
	if err := secondTM.RunNewTaskListener(); err != nil {
		t.Fatalf("second RunNewTaskListener: %v", err)
	}

	// The broker must redeliver the still-unacked "long-task" publish to
	// this new session -- proving the task was never lost.
	deadline := time.After(5 * time.Second)
	for rerun.execs.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the restarted agent never received the redelivered task -- it was lost")
		case <-time.After(50 * time.Millisecond):
		}
	}

	statuses.waitForStatus(t, task.ID, TaskAckFinished, 2*time.Second)
}
