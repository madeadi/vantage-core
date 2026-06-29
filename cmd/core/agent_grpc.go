package main

import (
	"context"
	"io"
	"log/slog"
	"strings"

	agentv1 "vantageos-core/proto/agent/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type LayoutPoseListener interface {
	OnPoseUpdate(agentID AgentID, pose *agentv1.PoseTelemetryEvent)
}

type agentGRPCServer struct {
	agentv1.UnimplementedAgentServiceServer
	registry  *AgentRegistry
	telemetry *TelemetryListener
	pose      LayoutPoseListener
	layouts   []AgentLayoutConfig
}

func (s *agentGRPCServer) StreamTasks(stream agentv1.AgentService_StreamTasksServer) error {
	agentID := agentIDFromContext(stream.Context())
	slog.Info("StreamTasks: agentsdk connected", "agent_id", agentID)

	s.registry.attachStream(agentID, stream)
	defer func() {
		s.registry.detachStream(agentID)
		slog.Info("StreamTasks: agentsdk disconnected", "agent_id", agentID)
	}()

	for {
		ack, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			slog.Error("StreamTasks: recv error", "agent_id", agentID, "err", err)
			return err
		}
		slog.Info("task ack received", "agent_id", agentID, "task_id", ack.TaskId, "status", ack.Status)
	}
}

func (s *agentGRPCServer) ReportTelemetry(stream agentv1.AgentService_ReportTelemetryServer) error {
	agentID := agentIDFromContext(stream.Context())
	slog.Info("ReportTelemetry: agentsdk connected", "agent_id", agentID)

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			_ = stream.SendAndClose(&agentv1.TelemetryAck{})
			return nil
		}
		if err != nil {
			slog.Error("ReportTelemetry: recv error", "agent_id", agentID, "err", err)
			return err
		}
		s.telemetry.Handle(agentID, event)
	}
}

func (s *agentGRPCServer) ReportPoseTelemetry(stream agentv1.AgentService_ReportPoseTelemetryServer) error {
	agentID := agentIDFromContext(stream.Context())
	slog.Info("ReportPoseTelemetry: agentsdk connected", "agent_id", agentID)
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			_ = stream.SendAndClose(&agentv1.PoseTelemetryAck{})
			return nil
		}
		if err != nil {
			slog.Error("ReportPoseTelemetry: recv error", "agent_id", agentID, "err", err)
			return err
		}
		s.pose.OnPoseUpdate(agentID, event)
	}
}

func (s *agentGRPCServer) GetTransformationMatrices(ctx context.Context, req *agentv1.TransformationMatrixRequest) (*agentv1.TransformationMatrixResponse, error) {
	agentID := AgentID(req.AgentId)
	if agentID == "" {
		agentID = agentIDFromContext(ctx)
	}

	var matrices []*agentv1.TransformationMatrix
	for _, l := range s.layouts {
		if l.AgentID != agentID {
			continue
		}
		flat := make([]float64, 0, 9)
		for _, row := range l.TransformationMatrix {
			flat = append(flat, row...)
		}
		matrices = append(matrices, &agentv1.TransformationMatrix{
			LayoutId:    l.LayoutID,
			Matrix:      flat,
			NorthOffset: l.NorthOffset,
		})
	}
	return &agentv1.TransformationMatrixResponse{Matrices: matrices}, nil
}

func authenticateMetadata(registry *AgentRegistry, ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}

	authVals := md.Get("authorization")
	if len(authVals) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization header")
	}
	token, ok := strings.CutPrefix(authVals[0], "Bearer ")
	if !ok || token == "" {
		return status.Error(codes.Unauthenticated, "invalid authorization format")
	}

	agentIDVals := md.Get("agent_id")
	if len(agentIDVals) == 0 {
		return status.Error(codes.Unauthenticated, "missing agent_id")
	}
	agentID := AgentID(agentIDVals[0])

	if !registry.Authenticate(agentID, token) {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
}

// authUnaryInterceptor validates the bearer token from gRPC metadata on every unary call.
func authUnaryInterceptor(registry *AgentRegistry) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authenticateMetadata(registry, ctx); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// authStreamInterceptor validates the bearer token from gRPC metadata on every stream open.
func authStreamInterceptor(registry *AgentRegistry) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authenticateMetadata(registry, ss.Context()); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

// agentIDFromContext extracts the agent_id from incoming gRPC metadata.
// Safe to call after authStreamInterceptor has already validated it.
func agentIDFromContext(ctx context.Context) AgentID {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("agent_id"); len(vals) > 0 {
			return AgentID(vals[0])
		}
	}
	return ""
}
