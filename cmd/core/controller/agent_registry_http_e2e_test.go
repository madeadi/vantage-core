//go:build !race

// This file's own logic is race-clean; it is excluded from -race runs for
// the same reason pkg/agentsdk/agent_integration_test.go is -- calling
// agent.Register() exercises the real MQTT connect path (CleanSession=false,
// reconnect-capable), which reliably triggers a genuine, already-documented
// data race inside the pinned github.com/eclipse/paho.mqtt.golang v1.5.1
// dependency itself ((*client).resume() vs. the outgoing-write goroutine's
// packets.(*FixedHeader).pack(), both entirely inside paho). See that file's
// doc comment for the full writeup and the accepted-risk decision -- this is
// the same risk, not a new one, just reached via a different entrypoint
// (registration + NewAgent rather than NewAgent directly).
package controller

import (
	"fmt"
	"testing"
	"time"

	mqttbroker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"

	"vantageos-core/cmd/core/config"
	"vantageos-core/cmd/core/service"
	"vantageos-core/pkg/agentsdk"
)

// TestRegisterAndConnectEndToEnd is Step 15's literal "Done when": an agent
// bootstraps from nothing but a device key to a working scoped broker
// session. Real HTTP registration endpoint, real embedded MQTT broker
// (mochi-mqtt, allow-all -- this sandbox has no Mosquitto ACL-file
// enforcement to test against, see the comment above agent.Register()
// below), real agentsdk.Register + agentsdk.NewAgent using exactly the
// credentials the registration response returned.
func TestRegisterAndConnectEndToEnd(t *testing.T) {
	broker, brokerAddr := newTestBrokerAllowAll(t)

	mqttCfg := config.MQTTConfig{
		Enabled: true, Broker: "tcp://" + brokerAddr, TopicPrefix: "vantageos",
	}
	_, server := newTestAgentController(t, mqttCfg, service.AllowedAgent{AgentID: "e2e-agent", Key: "e2e-key"})

	regResp, err := agentsdk.Register(server.URL, "e2e-key")
	if err != nil {
		t.Fatalf("agentsdk.Register: %v", err)
	}
	if regResp.AgentID != "e2e-agent" {
		t.Fatalf("agent_id = %q, want e2e-agent", regResp.AgentID)
	}
	if regResp.BrokerURL == "" || regResp.Username == "" || regResp.Password == "" || regResp.TopicPrefix == "" {
		t.Fatalf("incomplete broker credentials: %+v", regResp)
	}

	// The broker in this test allows any credentials (see
	// newTestBrokerAllowAll) -- what this proves is that the SDK, given
	// exactly what registration returned and nothing else, successfully
	// opens an MQTT session and the broker sees the right client. Whether a
	// *real* deployment's broker actually enforces username==agent_id and
	// the six ACL patterns is a broker-configuration concern (Mosquitto's
	// password_file + acl_file, per the Ops section), not something this
	// package's Go code can drive -- that seam is deliberate, matching
	// this step's own file list (cmd/core/controller/agent_registry_http.go,
	// pkg/agentsdk/registration.go; no broker-provisioning integration).
	agent := agentsdk.NewAgent(regResp.AgentID, regResp.TopicPrefix, regResp.BrokerURL, regResp.Username, regResp.Password)
	if agent == nil {
		t.Fatal("agentsdk.NewAgent returned nil")
	}
	t.Cleanup(agent.Stop)

	agent.Register()

	deadline := time.After(5 * time.Second)
	for {
		clients := broker.Clients.GetAll()
		found := false
		for _, cl := range clients {
			if cl.ID == "e2e-agent" {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-deadline:
			t.Fatal("agent never showed up as a connected client on the broker within 5s")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// newTestBrokerAllowAll starts a real mochi-mqtt broker (allow-all hook,
// same as pkg/agentsdk's own agent_integration_test.go) bound to an
// ephemeral port, returning it and its "host:port" address.
func newTestBrokerAllowAll(t *testing.T) (*mqttbroker.Server, string) {
	t.Helper()

	server := mqttbroker.New(&mqttbroker.Options{InlineClient: true})
	if err := server.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("AddHook: %v", err)
	}

	tcp := listeners.NewTCP(listeners.Config{ID: fmt.Sprintf("test-%d", time.Now().UnixNano()), Address: "127.0.0.1:0"})
	if err := server.AddListener(tcp); err != nil {
		t.Fatalf("AddListener: %v", err)
	}

	go func() {
		_ = server.Serve()
	}()
	t.Cleanup(func() { _ = server.Close() })

	return server, tcp.Address()
}
