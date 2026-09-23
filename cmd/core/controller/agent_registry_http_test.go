package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vantageos-core/cmd/core/config"
	"vantageos-core/cmd/core/service"
	"vantageos-core/pkg/agentsdk"
)

func newTestAgentController(t *testing.T, mqttCfg config.MQTTConfig, allowed ...service.AllowedAgent) (*AgentController, *httptest.Server) {
	t.Helper()
	ar := service.NewAgentRegistry(allowed, "localhost:19090")
	ac := NewAgentController(ar, mqttCfg)
	mux := http.NewServeMux()
	ac.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return ac, server
}

func doRegister(t *testing.T, serverURL, deviceKey string) (*http.Response, agentsdk.RegisterResponse) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, serverURL+"/agents/register", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+deviceKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	defer resp.Body.Close()
	var body agentsdk.RegisterResponse
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp, body
}

func TestHandleRegisterMQTTDisabledOmitsBrokerFields(t *testing.T) {
	_, server := newTestAgentController(t, config.MQTTConfig{}, service.AllowedAgent{AgentID: "bot-1", Key: "key-1"})

	resp, body := doRegister(t, server.URL, "key-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body.Token == "" || body.GRPCAddr == "" {
		t.Errorf("gRPC fields missing: %+v", body)
	}
	if body.AgentID != "bot-1" {
		t.Errorf("agent_id = %q, want bot-1", body.AgentID)
	}
	if body.BrokerURL != "" || body.Username != "" || body.Password != "" || body.TopicPrefix != "" {
		t.Errorf("broker fields set with mqtt disabled: %+v", body)
	}
}

func TestHandleRegisterMQTTEnabledIssuesCredentials(t *testing.T) {
	mqttCfg := config.MQTTConfig{Enabled: true, Broker: "tcp://broker:1883", TopicPrefix: "vantageos"}
	_, server := newTestAgentController(t, mqttCfg, service.AllowedAgent{AgentID: "bot-1", Key: "key-1"})

	_, first := doRegister(t, server.URL, "key-1")
	if first.BrokerURL != "tcp://broker:1883" {
		t.Errorf("broker_url = %q, want tcp://broker:1883", first.BrokerURL)
	}
	if first.Username != "bot-1" {
		t.Errorf("username = %q, want bot-1 (must equal the agent id -- see IssueMQTTCredentials)", first.Username)
	}
	if first.Password == "" {
		t.Error("password is empty, want a minted credential")
	}
	if first.TopicPrefix != "vantageos" {
		t.Errorf("topic_prefix = %q, want vantageos", first.TopicPrefix)
	}

	_, second := doRegister(t, server.URL, "key-1")
	if second.Password == first.Password {
		t.Error("password did not change across registrations -- want a fresh one every time")
	}
}

func TestHandleRegisterBrokerAdvertiseURLOverridesBroker(t *testing.T) {
	mqttCfg := config.MQTTConfig{
		Enabled: true, Broker: "tcp://internal:1883", BrokerAdvertiseURL: "tcp://public.example.com:1883",
		TopicPrefix: "vantageos",
	}
	_, server := newTestAgentController(t, mqttCfg, service.AllowedAgent{AgentID: "bot-1", Key: "key-1"})

	_, body := doRegister(t, server.URL, "key-1")
	if body.BrokerURL != "tcp://public.example.com:1883" {
		t.Errorf("broker_url = %q, want the advertise URL, not core's own connection address", body.BrokerURL)
	}
}

func TestHandleRegisterRejectsAgentIDInvalidForMQTTTopic(t *testing.T) {
	// An id that would corrupt the fixed-depth topic layout -- see
	// agentsdk.NewTopic. Not something normal provisioning produces (Step
	// 1's own validation runs earlier in the chain), but the "agents"
	// PocketBase collection's agent_id field has no format constraint at
	// the database level, so this must be re-checked here too.
	mqttCfg := config.MQTTConfig{Enabled: true, Broker: "tcp://broker:1883", TopicPrefix: "vantageos"}
	_, server := newTestAgentController(t, mqttCfg, service.AllowedAgent{AgentID: "bad/id", Key: "key-1"})

	resp, _ := doRegister(t, server.URL, "key-1")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for an agent id that is not a valid mqtt topic segment", resp.StatusCode)
	}
}
