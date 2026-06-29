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
	// matrix is the 3×3 affine transformation matrix (row-major, 9 values)
	// fetched from core for this agent+layout pair. Nil means no transform.
	matrix      []float64
	northOffset float64
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

	tCtx := metadata.NewOutgoingContext(ctx, md)
	resp, err := client.GetTransformationMatrices(tCtx, &agentv1.TransformationMatrixRequest{})
	if err != nil {
		slog.Warn("could not fetch transformation matrices, poses will be sent in robot frame", "err", err)
	} else {
		for _, m := range resp.GetMatrices() {
			if m.LayoutId == p.layoutID && len(m.Matrix) == 9 {
				p.matrix = m.Matrix
				p.northOffset = m.NorthOffset
				slog.Info("transformation matrix loaded", "layout_id", p.layoutID, "north_offset", p.northOffset)
				break
			}
		}
		if p.matrix == nil {
			slog.Warn("no transformation matrix found for layout", "layout_id", p.layoutID)
		}
	}

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
	x, y, yaw := rp.X, rp.Y, rp.Yaw
	if p.matrix != nil {
		x, y = applyAffine(p.matrix, x, y)
		yaw += p.northOffset
	}
	return x, y, yaw, p.layoutID
}

// applyAffine applies a 3×3 row-major affine matrix to a 2D point (x, y).
//
//	[x']   [m0 m1 m2] [x]
//	[y'] = [m3 m4 m5] [y]
//	[1 ]   [m6 m7 m8] [1]
func applyAffine(m []float64, x, y float64) (float64, float64) {
	return m[0]*x + m[1]*y + m[2], m[3]*x + m[4]*y + m[5]
}
