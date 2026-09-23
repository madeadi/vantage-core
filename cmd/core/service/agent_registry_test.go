package service

import (
	"testing"
	"time"
	"vantageos-core/cmd/core/model"
)

func TestOnlineAgentsEmptyWhenNothingConnected(t *testing.T) {
	ar := NewAgentRegistry(nil, "localhost:9090")
	if got := ar.OnlineAgents(); len(got) != 0 {
		t.Errorf("OnlineAgents() = %v, want empty", got)
	}
}

func TestMarkMQTTOnlineMakesAgentAppearOnline(t *testing.T) {
	ar := NewAgentRegistry([]AllowedAgent{{AgentID: "bot-1", Name: "Bot One"}}, "localhost:9090")

	ar.MarkMQTTOnline("bot-1")

	online := ar.OnlineAgents()
	agent, ok := online["bot-1"]
	if !ok {
		t.Fatal("bot-1 not reported online after MarkMQTTOnline")
	}
	if agent.Name != "Bot One" {
		t.Errorf("Name = %q, want Bot One (from AllowedAgent)", agent.Name)
	}
}

func TestMarkMQTTOfflineRemovesAgentImmediately(t *testing.T) {
	ar := NewAgentRegistry(nil, "localhost:9090")
	ar.MarkMQTTOnline("bot-1")

	if _, ok := ar.OnlineAgents()["bot-1"]; !ok {
		t.Fatal("bot-1 not online after MarkMQTTOnline")
	}

	ar.MarkMQTTOffline("bot-1")

	if _, ok := ar.OnlineAgents()["bot-1"]; ok {
		t.Error("bot-1 still reported online after MarkMQTTOffline")
	}
}

// TestMQTTPresenceGoesStaleWithoutAnyOfflineEvent proves the staleness
// safety net (spec Step 16): an agent whose last signal is older than
// mqttPresenceStaleAfter is no longer reported online, even though no
// explicit offline event ever arrived (the scenario an LWT/network
// partition can produce).
func TestMQTTPresenceGoesStaleWithoutAnyOfflineEvent(t *testing.T) {
	ar := NewAgentRegistry(nil, "localhost:9090")

	ar.mu.Lock()
	ar.mqttLastSeen["bot-1"] = time.Now().Add(-mqttPresenceStaleAfter - time.Second)
	ar.mu.Unlock()

	if _, ok := ar.OnlineAgents()["bot-1"]; ok {
		t.Error("bot-1 reported online despite its last signal being older than mqttPresenceStaleAfter")
	}
}

func TestOnlineAgentsUnionsGRPCStreamsAndMQTTPresence(t *testing.T) {
	ar := NewAgentRegistry(nil, "localhost:9090")
	ar.AttachStream("grpc-bot", nil) // nil stream is fine -- only presence is under test here
	ar.MarkMQTTOnline("mqtt-bot")

	online := ar.OnlineAgents()
	if _, ok := online["grpc-bot"]; !ok {
		t.Error("grpc-bot (gRPC stream) not reported online")
	}
	if _, ok := online["mqtt-bot"]; !ok {
		t.Error("mqtt-bot (MQTT presence) not reported online")
	}
	if len(online) != 2 {
		t.Errorf("OnlineAgents() has %d entries, want 2", len(online))
	}
}

// TestOnlineAgentsGRPCStreamTakesPrecedenceOverAbsentMQTTEntry is really
// just confirming the union doesn't panic or duplicate when only one
// signal exists for an agent that has both a name from AllowedAgents and a
// gRPC stream -- a basic sanity check on the merge logic in OnlineAgents.
func TestOnlineAgentsGRPCStreamTakesPrecedenceOverAbsentMQTTEntry(t *testing.T) {
	ar := NewAgentRegistry([]AllowedAgent{{AgentID: "bot-1", Name: "Bot One"}}, "localhost:9090")
	ar.AttachStream("bot-1", nil)

	online := ar.OnlineAgents()
	agent, ok := online["bot-1"]
	if !ok {
		t.Fatal("bot-1 not reported online")
	}
	if agent.ID != model.AgentID("bot-1") {
		t.Errorf("ID = %q, want bot-1", agent.ID)
	}
}
