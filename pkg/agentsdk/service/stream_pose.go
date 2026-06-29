package service

import (
	"context"
	"log/slog"
	"time"
	skill "vantageos-core/pkg/agentsdk/agent_skill"

	agentv1 "vantageos-core/proto/agent/v1"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// StreamPose manages the ReportPoseTelemetry client-streaming gRPC call.
// It samples the robot's current pose at a fixed interval and streams
// [agentv1.PoseTelemetryEvent] messages to the core server.
type StreamPose struct {
	duration  time.Duration
	agentID   string
	layoutID  string
	poseSkill skill.RobotPoseSkill
}

// NewStreamPose returns a StreamPose that reports poses for agentID, sampling
// rps at the given duration interval and tagging events with layoutID.
func NewStreamPose(agentID string, rps skill.RobotPoseSkill, duration time.Duration, layoutID string) *StreamPose {
	return &StreamPose{
		duration:  duration,
		agentID:   agentID,
		layoutID:  layoutID,
		poseSkill: rps,
	}
}

// Run opens the ReportPoseTelemetry client stream and sends a
// [agentv1.PoseTelemetryEvent] on each tick of the configured interval.
// It closes the stream cleanly when ctx is cancelled.
func (p *StreamPose) Run(ctx context.Context, client agentv1.AgentServiceClient) {
	md := metadata.Pairs("agent_id", p.agentID)
	streamCtx := metadata.NewOutgoingContext(ctx, md)
	stream, err := client.ReportPoseTelemetry(streamCtx)
	if err != nil {
		slog.Error("failed to open ReportPoseTelemetry", "err", err)
		return
	}

	slog.Info("ReportPoseTelemetry stream open")

	ticker := time.NewTicker(p.duration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = stream.CloseSend()
			return
		case <-ticker.C:
			x, y, yaw, layoutID := p.getLayoutPose()
			if err := stream.Send(&agentv1.PoseTelemetryEvent{
				AgentId:   p.agentID,
				LayoutId:  layoutID,
				X:         x,
				Y:         y,
				Yaw:       yaw,
				Timestamp: timestamppb.Now(),
			}); err != nil {
				slog.Error("pose telemetry send error", "err", err)
				return
			}
		}
	}
}

func (p *StreamPose) getLayoutPose() (float64, float64, float64, string) {
	rp := p.poseSkill.GetRobotPose()
	return rp.X, rp.Y, rp.Yaw, p.layoutID
}
