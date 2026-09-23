package service

import (
	"testing"
	"time"
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

