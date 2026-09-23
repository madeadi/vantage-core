// Command mqtt-agent-example is a minimal, runnable agent proving the MQTT
// SDK (pkg/agentsdk) end-to-end: it registers with a broker, derives and
// announces its telemetry schema, publishes telemetry on a ticker, and
// handles one task type.
//
// This is what an agent developer copies as a starting point for a real
// agent: replace AgentTelemetry with your own agent's shape, replace
// pingHandler with your own task handlers, and replace readTelemetry with
// whatever actually reads your robot's sensors.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vantageos-core/pkg/agentsdk"
)

// AgentTelemetry is the customization point described above. Its derived
// JSON Schema is announced (advisory) on connect and every reconnect; the
// authoritative contract an agent's payloads are actually validated against
// lives on its agent_groups.telemetry_schema record in PocketBase — see
// specs/mqtt_telemetry.specs.md.
type AgentTelemetry struct {
	BatteryPercent float64 `json:"battery_percent"`
	PoseX          float64 `json:"x"`
	PoseY          float64 `json:"y"`
	Status         string  `json:"status"`
}

func main() {
	id := flag.String("id", "mqtt-agent-example", "agent id -- also the mqtt client id (ignored with -core-url, see below)")
	prefix := flag.String("prefix", "vantageos", "mqtt topic prefix (ignored with -core-url)")
	broker := flag.String("broker", "tcp://127.0.0.1:1883", "mqtt broker URL (ignored with -core-url)")
	username := flag.String("username", "", "mqtt username (defaults to -id; ignored with -core-url)")
	password := flag.String("password", "", "mqtt password (ignored with -core-url)")
	interval := flag.Duration("interval", 5*time.Second, "telemetry publish interval")

	// -core-url + -key bootstrap from a device key via POST /agents/register
	// (spec Step 15) instead of the four flags above -- the broker URL,
	// username, password, topic prefix, and even the agent's own id all come
	// from core's response, matching how a real deployed agent would be
	// provisioned (one device key, nothing else to configure by hand).
	coreURL := flag.String("core-url", "", "core's HTTP base URL, e.g. http://127.0.0.1:8080 -- when set, bootstraps via POST /agents/register instead of -broker/-username/-password/-id/-prefix")
	deviceKey := flag.String("key", "", "device key (agents.key) for -core-url registration")
	flag.Parse()

	if *username == "" {
		*username = *id
	}

	if *coreURL != "" {
		resp, err := agentsdk.Register(*coreURL, *deviceKey)
		if err != nil {
			slog.Error("registration failed", "core_url", *coreURL, "error", err)
			os.Exit(1)
		}
		if resp.BrokerURL == "" {
			slog.Error("registration succeeded but returned no broker credentials -- is mqtt.enabled true on this core instance?", "core_url", *coreURL)
			os.Exit(1)
		}
		*id, *prefix, *broker, *username, *password = resp.AgentID, resp.TopicPrefix, resp.BrokerURL, resp.Username, resp.Password
		slog.Info("registered via core", "core_url", *coreURL, "agent_id", *id, "broker", *broker)
	}

	agent := agentsdk.NewAgent(*id, *prefix, *broker, *username, *password)
	if agent == nil {
		slog.Error("failed to build agent -- check -id and -prefix")
		os.Exit(1)
	}

	// NewTelemetry must run before Register: it registers a hook (via
	// agent.OnConnect) that re-announces the schema on every connect and
	// reconnect, and that hook needs to be in place before the first
	// connect happens for it to catch that one too.
	tel, err := agentsdk.NewTelemetry[AgentTelemetry](agent)
	if err != nil {
		slog.Error("failed to derive telemetry schema", "error", err)
		os.Exit(1)
	}
	slog.Info("derived telemetry schema", "schema_hash", tel.SchemaHash())

	// Handlers must be registered before Register() too, for the same
	// reason -- the task listener subscribes as part of handling the first
	// connect, which can fire very soon after Register() returns.
	agent.TaskManager.RegisterHandler(pingHandler{})

	agent.Register()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tel.StartLoop(ctx, *interval, readTelemetry)

	slog.Info("mqtt-agent-example running", "id", *id, "broker", *broker, "interval", *interval)
	<-ctx.Done()

	slog.Info("shutting down")
	agent.Stop()
}

// readTelemetry stands in for reading a real robot's sensors. A real agent
// replaces this with whatever actually produces its telemetry values.
func readTelemetry(context.Context) (AgentTelemetry, error) {
	return AgentTelemetry{
		BatteryPercent: 40 + rand.Float64()*60,
		PoseX:          rand.Float64() * 10,
		PoseY:          rand.Float64() * 10,
		Status:         "idle",
	}, nil
}

// pingHandler proves task dispatch end-to-end by handling "PING" tasks --
// nothing more than logging that one arrived.
type pingHandler struct{}

func (pingHandler) GetTaskType() string      { return "PING" }
func (pingHandler) GetPayloadSchema() string { return "" }

func (pingHandler) Execute(ctx context.Context, rawPayload any) error {
	body, ok := rawPayload.([]byte)
	if !ok {
		body, _ = json.Marshal(rawPayload)
	}
	slog.Info("received PING task", "payload", string(body))
	return nil
}
