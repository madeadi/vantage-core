package agentsdk

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// fakeToken is a completed mqtt.Token carrying a fixed error, for a fake
// client that never actually touches a broker.
type fakeToken struct{ err error }

func (f *fakeToken) Wait() bool                     { return true }
func (f *fakeToken) WaitTimeout(time.Duration) bool { return true }
func (f *fakeToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (f *fakeToken) Error() error { return f.err }

// publishedMessage records one call to fakeClient.Publish.
type publishedMessage struct {
	topic    string
	qos      byte
	retained bool
	payload  []byte
}

// fakeClient is a minimal mqtt.Client — the interface's own doc comment says
// it exists "primarily to allow mocking tests" — that records every Publish
// call instead of touching a broker. Every other method panics if called,
// since Telemetry only ever calls Publish.
type fakeClient struct {
	mu         sync.Mutex
	published  []publishedMessage
	publishErr error
}

func (f *fakeClient) Publish(topic string, qos byte, retained bool, payload any) mqtt.Token {
	f.mu.Lock()
	defer f.mu.Unlock()

	var body []byte
	switch p := payload.(type) {
	case []byte:
		body = p
	case string:
		body = []byte(p)
	default:
		panic("fakeClient.Publish: unsupported payload type")
	}

	f.published = append(f.published, publishedMessage{topic: topic, qos: qos, retained: retained, payload: body})
	return &fakeToken{err: f.publishErr}
}

func (f *fakeClient) messages() []publishedMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]publishedMessage, len(f.published))
	copy(out, f.published)
	return out
}

func (f *fakeClient) IsConnected() bool       { return true }
func (f *fakeClient) IsConnectionOpen() bool  { return true }
func (f *fakeClient) Connect() mqtt.Token     { panic("not implemented") }
func (f *fakeClient) Disconnect(quiesce uint) { panic("not implemented") }
func (f *fakeClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	panic("not implemented")
}
func (f *fakeClient) SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
	panic("not implemented")
}
func (f *fakeClient) Unsubscribe(topics ...string) mqtt.Token             { panic("not implemented") }
func (f *fakeClient) AddRoute(topic string, callback mqtt.MessageHandler) { panic("not implemented") }
func (f *fakeClient) OptionsReader() mqtt.ClientOptionsReader             { panic("not implemented") }

type testTelemetry struct {
	BatteryPercent float64 `json:"battery_percent"`
	Status         string  `json:"status"`
}

func newTestAgent(t *testing.T, client mqtt.Client) *Agent {
	t.Helper()
	topic, err := NewTopic("vantageos", "test-agent")
	if err != nil {
		t.Fatalf("NewTopic: %v", err)
	}
	return &Agent{ID: "test-agent", Topic: topic, Client: client}
}

func TestNewTelemetryRejectsNilAgent(t *testing.T) {
	if _, err := NewTelemetry[testTelemetry](nil); err == nil {
		t.Fatal("NewTelemetry(nil): want error, got nil")
	}
}

func TestTelemetryPublishSchema(t *testing.T) {
	fc := &fakeClient{}
	agent := newTestAgent(t, fc)

	tel, err := NewTelemetry[testTelemetry](agent)
	if err != nil {
		t.Fatalf("NewTelemetry: %v", err)
	}

	if err := tel.PublishSchema(); err != nil {
		t.Fatalf("PublishSchema: %v", err)
	}

	msgs := fc.messages()
	if len(msgs) != 1 {
		t.Fatalf("got %d published messages, want 1", len(msgs))
	}
	msg := msgs[0]

	if want := "vantageos/agent/test-agent/telemetry-schema"; msg.topic != want {
		t.Errorf("topic = %q, want %q", msg.topic, want)
	}
	if msg.qos != 1 {
		t.Errorf("qos = %d, want 1 (retained advisory announcement)", msg.qos)
	}
	if !msg.retained {
		t.Error("retained = false, want true (a late subscriber must still see the schema)")
	}

	var got TelemetrySchemaAnnouncement
	if err := json.Unmarshal(msg.payload, &got); err != nil {
		t.Fatalf("unmarshal published payload: %v", err)
	}
	if got.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "test-agent")
	}
	if got.SchemaHash != tel.SchemaHash() {
		t.Errorf("SchemaHash = %q, want %q", got.SchemaHash, tel.SchemaHash())
	}
	if string(got.Schema) != string(tel.SchemaJSON()) {
		t.Errorf("Schema = %s, want %s", got.Schema, tel.SchemaJSON())
	}
}

func TestTelemetryPublish(t *testing.T) {
	fc := &fakeClient{}
	agent := newTestAgent(t, fc)

	tel, err := NewTelemetry[testTelemetry](agent)
	if err != nil {
		t.Fatalf("NewTelemetry: %v", err)
	}

	if err := tel.Publish(testTelemetry{BatteryPercent: 87.5, Status: "idle"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	msgs := fc.messages()
	if len(msgs) != 1 {
		t.Fatalf("got %d published messages, want 1", len(msgs))
	}
	msg := msgs[0]

	if want := "vantageos/agent/test-agent/telemetry"; msg.topic != want {
		t.Errorf("topic = %q, want %q", msg.topic, want)
	}
	if msg.qos != 0 {
		t.Errorf("qos = %d, want 0", msg.qos)
	}
	if msg.retained {
		t.Error("retained = true, want false")
	}

	// The wire payload must be T marshaled as-is, with no envelope wrapping
	// it (agent_id, timestamp, seq, ...) -- see specs/mqtt_telemetry.specs.md
	// D3: agents publish raw, self-shaped payloads.
	var got testTelemetry
	if err := json.Unmarshal(msg.payload, &got); err != nil {
		t.Fatalf("unmarshal published payload: %v", err)
	}
	if got.BatteryPercent != 87.5 || got.Status != "idle" {
		t.Errorf("got %+v, want {87.5 idle}", got)
	}

	var raw map[string]any
	if err := json.Unmarshal(msg.payload, &raw); err != nil {
		t.Fatalf("unmarshal as map: %v", err)
	}
	for _, unexpected := range []string{"agent_id", "timestamp", "seq"} {
		if _, present := raw[unexpected]; present {
			t.Errorf("published payload unexpectedly contains envelope field %q: %s", unexpected, msg.payload)
		}
	}
}

func TestTelemetryPublishWithoutConnection(t *testing.T) {
	agent := newTestAgent(t, nil) // Client is nil: never registered/connected

	tel, err := NewTelemetry[testTelemetry](agent)
	if err != nil {
		t.Fatalf("NewTelemetry: %v", err)
	}

	if err := tel.Publish(testTelemetry{Status: "idle"}); err == nil {
		t.Fatal("Publish on a disconnected agent: want error, got nil")
	}
	if err := tel.PublishSchema(); err == nil {
		t.Fatal("PublishSchema on a disconnected agent: want error, got nil")
	}
}

func TestTelemetryPublishPropagatesBrokerError(t *testing.T) {
	fc := &fakeClient{publishErr: context.DeadlineExceeded}
	agent := newTestAgent(t, fc)

	tel, err := NewTelemetry[testTelemetry](agent)
	if err != nil {
		t.Fatalf("NewTelemetry: %v", err)
	}

	if err := tel.Publish(testTelemetry{Status: "idle"}); err == nil {
		t.Fatal("Publish with a failing token: want error, got nil")
	}
}

func TestTelemetryStartLoopPublishesOnTickAndStopsOnCancel(t *testing.T) {
	fc := &fakeClient{}
	agent := newTestAgent(t, fc)

	tel, err := NewTelemetry[testTelemetry](agent)
	if err != nil {
		t.Fatalf("NewTelemetry: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	src := func(context.Context) (testTelemetry, error) {
		return testTelemetry{BatteryPercent: 42, Status: "moving"}, nil
	}

	before := time.Now()
	tel.StartLoop(ctx, 5*time.Millisecond, src)
	if elapsed := time.Since(before); elapsed > 50*time.Millisecond {
		t.Fatalf("StartLoop blocked its caller for %v, want it to return immediately", elapsed)
	}

	deadline := time.After(500 * time.Millisecond)
	for {
		if len(fc.messages()) >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("got %d published messages after 500ms, want at least 3", len(fc.messages()))
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	time.Sleep(20 * time.Millisecond) // let the loop goroutine observe cancellation
	countAtCancel := len(fc.messages())
	time.Sleep(50 * time.Millisecond)
	if got := len(fc.messages()); got != countAtCancel {
		t.Errorf("published %d more messages after ctx was cancelled, want 0", got-countAtCancel)
	}
}

func TestTelemetryStartLoopSkipsPublishOnSourceError(t *testing.T) {
	fc := &fakeClient{}
	agent := newTestAgent(t, fc)

	tel, err := NewTelemetry[testTelemetry](agent)
	if err != nil {
		t.Fatalf("NewTelemetry: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := func(context.Context) (testTelemetry, error) {
		return testTelemetry{}, context.DeadlineExceeded
	}

	tel.StartLoop(ctx, 5*time.Millisecond, src)
	time.Sleep(30 * time.Millisecond)

	if got := len(fc.messages()); got != 0 {
		t.Errorf("published %d messages despite every source call failing, want 0", got)
	}
}
