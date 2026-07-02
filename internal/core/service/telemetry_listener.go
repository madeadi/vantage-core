package service

import (
	"log/slog"
	"vantageos-core/internal/core/model"
	agentv1 "vantageos-core/proto/agent/v1"
)

type TelemetryListener struct{}

func NewTelemetryListener() *TelemetryListener {
	return &TelemetryListener{}
}

func (t *TelemetryListener) Handle(agentID model.AgentID, event *agentv1.TelemetryEvent) {
	slog.Info("telemetry received", "agentsdk", agentID, "payload", string(event.Payload))
}
