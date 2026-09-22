package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mqttbroker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"vantageos-core/pkg/agentsdk"
)

// newTestBroker starts an embedded, allow-all MQTT broker for the duration
// of t and returns its bound "host:port" address. A real broker, not a fake
// client — this test's job is proving the whole path (agent SDK publish →
// wire → ingest.Daemon) actually works, which a fake can't demonstrate.
func newTestBroker(t *testing.T) string {
	t.Helper()

	server := mqttbroker.New(nil)
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("AddHook: %v", err)
	}

	tcp := listeners.NewTCP(listeners.Config{ID: fmt.Sprintf("test-%d", time.Now().UnixNano()), Address: "127.0.0.1:0"})
	if err := server.AddListener(tcp); err != nil {
		t.Fatalf("AddListener: %v", err)
	}

	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })

	return tcp.Address()
}

type demoTelemetry struct {
	BatteryPercent float64 `json:"battery_percent"`
	Status         string  `json:"status"`
}

// TestIngestReceivesFromRealAgent is the end-to-end proof for Step 6: a real
// pkg/agentsdk agent, publishing over a real broker, actually reaches the
// ingest daemon's listeners with the right agent ID and kind. Everything
// else in this package's tests calls Daemon internals directly; this is the
// one that proves the pieces fit together over the wire.
func TestIngestReceivesFromRealAgent(t *testing.T) {
	brokerAddr := newTestBroker(t)
	brokerURL := "tcp://" + brokerAddr

	// The ingest daemon needs its own connected mqtt.Client to subscribe
	// with -- Step 15 designs how core gets broker credentials; for this
	// test a bare paho client against the allow-all broker stands in for it.
	coreOpts := mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID("core-ingest-test")
	coreClient := mqtt.NewClient(coreOpts)
	if token := coreClient.Connect(); token.Wait() && token.Error() != nil {
		t.Fatalf("core client connect: %v", token.Error())
	}
	t.Cleanup(func() { coreClient.Disconnect(250) })

	d := New("vantageos", 64)
	if err := d.Subscribe(coreClient); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	received := make(chan Message, 8)
	d.AddListener(func(m Message) { received <- m })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	agent := agentsdk.NewAgent("real-agent", "vantageos", brokerURL, "real-agent", "")
	if agent == nil {
		t.Fatal("NewAgent returned nil")
	}
	tel, err := agentsdk.NewTelemetry[demoTelemetry](agent)
	if err != nil {
		t.Fatalf("NewTelemetry: %v", err)
	}
	agent.Register()
	t.Cleanup(agent.Stop)

	waitForConnected(t, agent, 2*time.Second)

	if err := tel.Publish(demoTelemetry{BatteryPercent: 71, Status: "moving"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var gotTelemetry, gotSchema bool
	deadline := time.After(2 * time.Second)
	for !gotTelemetry || !gotSchema {
		select {
		case m := <-received:
			if m.AgentID != "real-agent" {
				t.Errorf("message AgentID = %q, want %q", m.AgentID, "real-agent")
			}
			switch m.Kind {
			case KindTelemetry:
				gotTelemetry = true
				var payload demoTelemetry
				if err := json.Unmarshal(m.Payload, &payload); err != nil {
					t.Fatalf("unmarshal telemetry payload: %v", err)
				}
				if payload.Status != "moving" || payload.BatteryPercent != 71 {
					t.Errorf("telemetry payload = %+v, want {71 moving}", payload)
				}
			case KindTelemetrySchema:
				gotSchema = true
				var announcement agentsdk.TelemetrySchemaAnnouncement
				if err := json.Unmarshal(m.Payload, &announcement); err != nil {
					t.Fatalf("unmarshal schema payload: %v", err)
				}
				if announcement.SchemaHash != tel.SchemaHash() {
					t.Errorf("schema hash = %q, want %q", announcement.SchemaHash, tel.SchemaHash())
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for both telemetry and schema (got telemetry=%v schema=%v)", gotTelemetry, gotSchema)
		}
	}

	stats := d.Stats()
	if stats.Ingested < 2 {
		t.Errorf("Stats().Ingested = %d, want at least 2", stats.Ingested)
	}
	if stats.Dropped != 0 {
		t.Errorf("Stats().Dropped = %d, want 0", stats.Dropped)
	}
}

// waitForConnected polls until agent's underlying mqtt client reports
// connected, or fails the test after timeout. It reaches into the package's
// own exported surface only (Register is async by design — see agent.go).
func waitForConnected(t *testing.T, agent *agentsdk.Agent, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if agent.Client != nil && agent.Client.IsConnected() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("agent did not connect within timeout")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
