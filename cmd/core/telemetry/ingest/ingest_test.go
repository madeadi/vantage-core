package ingest

import (
	"context"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// fakeMessage is a minimal mqtt.Message for calling a Daemon's handler
// directly, without a real broker.
type fakeMessage struct {
	topic   string
	payload []byte
}

func (m fakeMessage) Duplicate() bool   { return false }
func (m fakeMessage) Qos() byte         { return 0 }
func (m fakeMessage) Retained() bool    { return false }
func (m fakeMessage) Topic() string     { return m.topic }
func (m fakeMessage) MessageID() uint16 { return 0 }
func (m fakeMessage) Payload() []byte   { return m.payload }
func (m fakeMessage) Ack()              {}

func TestHandleExtractsAgentIDAndKind(t *testing.T) {
	d := New("vantageos", 8)

	d.handle(KindTelemetry)(nil, fakeMessage{
		topic:   "vantageos/agent/smallbot/telemetry",
		payload: []byte(`{"status":"idle"}`),
	})
	d.handle(KindTelemetrySchema)(nil, fakeMessage{
		topic:   "vantageos/agent/smallbot/telemetry-schema",
		payload: []byte(`{"type":"object"}`),
	})

	if got := d.Stats().QueueDepth; got != 2 {
		t.Fatalf("QueueDepth = %d, want 2", got)
	}

	first := <-d.queue
	if first.AgentID != "smallbot" || first.Kind != KindTelemetry {
		t.Errorf("first message = %+v, want AgentID=smallbot Kind=KindTelemetry", first)
	}
	if string(first.Payload) != `{"status":"idle"}` {
		t.Errorf("first payload = %s", first.Payload)
	}

	second := <-d.queue
	if second.AgentID != "smallbot" || second.Kind != KindTelemetrySchema {
		t.Errorf("second message = %+v, want AgentID=smallbot Kind=KindTelemetrySchema", second)
	}
}

func TestHandleIgnoresMalformedTopic(t *testing.T) {
	d := New("vantageos", 8)

	d.handle(KindTelemetry)(nil, fakeMessage{
		topic:   "not/the/right/shape",
		payload: []byte("x"),
	})

	stats := d.Stats()
	if stats.QueueDepth != 0 || stats.Dropped != 0 {
		t.Errorf("got %+v, want a malformed topic to be silently discarded (not queued, not counted as dropped)", stats)
	}
}

func TestHandleDropsAndCountsOnFullQueueWithoutBlocking(t *testing.T) {
	d := New("vantageos", 2) // small buffer, and Run is never started -- nothing drains it

	done := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			d.handle(KindTelemetry)(nil, fakeMessage{
				topic:   "vantageos/agent/smallbot/telemetry",
				payload: []byte("x"),
			})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handle blocked instead of dropping once the queue filled — this must never block the mqtt dispatch goroutine")
	}

	stats := d.Stats()
	if stats.QueueDepth != 2 {
		t.Errorf("QueueDepth = %d, want 2 (buffer size)", stats.QueueDepth)
	}
	if stats.Dropped != 3 {
		t.Errorf("Dropped = %d, want 3 (5 sent - 2 buffer capacity)", stats.Dropped)
	}
}

func TestRunFansOutToAllListeners(t *testing.T) {
	d := New("vantageos", 8)

	var mu sync.Mutex
	var gotA, gotB []Message
	d.AddListener(func(m Message) {
		mu.Lock()
		gotA = append(gotA, m)
		mu.Unlock()
	})
	d.AddListener(func(m Message) {
		mu.Lock()
		gotB = append(gotB, m)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	go d.Run(ctx)
	defer cancel()

	d.handle(KindTelemetry)(nil, fakeMessage{
		topic:   "vantageos/agent/smallbot/telemetry",
		payload: []byte(`{"status":"idle"}`),
	})

	deadline := time.After(time.Second)
	for {
		mu.Lock()
		done := len(gotA) == 1 && len(gotB) == 1
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("listeners were not both called within 1s")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if gotA[0].AgentID != "smallbot" || gotB[0].AgentID != "smallbot" {
		t.Errorf("gotA=%+v gotB=%+v, want both to see AgentID=smallbot", gotA, gotB)
	}

	stats := d.Stats()
	if stats.Ingested != 1 {
		t.Errorf("Ingested = %d, want 1", stats.Ingested)
	}
	if stats.QueueDepth != 0 {
		t.Errorf("QueueDepth = %d, want 0 (drained by Run)", stats.QueueDepth)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	d := New("vantageos", 8)

	ctx, cancel := context.WithCancel(context.Background())
	runReturned := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(runReturned)
	}()

	cancel()

	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of context cancellation")
	}
}

var _ mqtt.Message = fakeMessage{} // compile-time interface check
