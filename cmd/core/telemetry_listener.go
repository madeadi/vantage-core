package main

import (
	"log/slog"
	"vantageos-core/pkg/pubsub"
)

type TelemetryListener struct {
	subscriber pubsub.Subscriber
}

func NewTelemetryListener(subscriber pubsub.Subscriber) *TelemetryListener {
	return &TelemetryListener{subscriber: subscriber}
}

func (t *TelemetryListener) Watch(agentID AgentID) {
	topic := "agents/" + string(agentID) + "/telemetry"
	t.subscriber.Subscribe(topic, func(_ string, payload []byte) {
		slog.Info("telemetry received", "agent", agentID, "payload", string(payload))
	})
}

func (t *TelemetryListener) Unwatch(agentID AgentID) {
	t.subscriber.Unsubscribe("agents/" + string(agentID) + "/telemetry")
}
