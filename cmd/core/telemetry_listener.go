package main

import (
	"log/slog"

	agentv1 "vantageos-core/proto/agent/v1"
)

type TelemetryListener struct{}

func NewTelemetryListener() *TelemetryListener {
	return &TelemetryListener{}
}

func (t *TelemetryListener) Handle(agentID AgentID, event *agentv1.TelemetryEvent) {
	slog.Info("telemetry received", "agent", agentID, "payload", string(event.Payload))
}
