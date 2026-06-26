package main

import (
	"context"
	"log/slog"
	"time"
	agentv1 "vantageos-core/proto/agent/v1"

	"google.golang.org/grpc/metadata"
)

func runTelemetry(ctx context.Context, client agentv1.AgentServiceClient, agentID string) {
	md := metadata.Pairs("agent_id", agentID)
	streamCtx := metadata.NewOutgoingContext(ctx, md)
	stream, err := client.ReportTelemetry(streamCtx)
	if err != nil {
		slog.Error("failed to open ReportTelemetry", "err", err)
		return
	}
	slog.Info("ReportTelemetry stream open")

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = stream.CloseSend()
			return
		case <-ticker.C:
			if err := stream.Send(&agentv1.TelemetryEvent{
				AgentId: agentID,
				Payload: []byte(`{"status":"ok"}`),
			}); err != nil {
				slog.Error("telemetry send error", "err", err)
				return
			}
		}
	}
}
