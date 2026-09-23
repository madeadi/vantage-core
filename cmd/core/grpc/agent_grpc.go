package grpc

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"vantageos-core/cmd/core/config"
	"vantageos-core/cmd/core/model"
	"vantageos-core/cmd/core/service"
	agentv1 "vantageos-core/proto/agent/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type LayoutPoseListener interface {
	OnPoseUpdate(agentID model.AgentID, pose *agentv1.PoseTelemetryEvent)
}

// AgentServer is the gRPC AgentService implementation plus the hooks core uses
// to push live config changes into it.
type AgentServer interface {
	agentv1.AgentServiceServer
	// SetLayouts atomically replaces the agent↔layout transformation configs
	// served by GetTransformationMatrices.
	SetLayouts(layouts []config.AgentLayoutConfig)
}

type agentGRPCServer struct {
	agentv1.UnimplementedAgentServiceServer
	registry *service.AgentRegistry
	pose     LayoutPoseListener
	layouts  atomic.Pointer[[]config.AgentLayoutConfig]

	authService service.AuthService
}

func NewAgentGRPCServer(
	registry *service.AgentRegistry,
	pose LayoutPoseListener,
	layouts []config.AgentLayoutConfig,
) AgentServer {
	s := &agentGRPCServer{
		registry: registry,
		pose:     pose,
	}
	s.SetLayouts(layouts)
	return s
}

func (s *agentGRPCServer) SetLayouts(layouts []config.AgentLayoutConfig) {
	next := append([]config.AgentLayoutConfig(nil), layouts...)
	s.layouts.Store(&next)
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

func (s *agentGRPCServer) ReportSkills(ctx context.Context, req *agentv1.SkillRegistration) (*agentv1.SkillRegistrationAck, error) {
	agentID := agentIDFromContext(ctx)

	skills := make([]model.AgentSkill, 0, len(req.GetSkills()))
	for _, sk := range req.GetSkills() {
		if err := service.ValidateSchema(sk.GetPayloadSchema()); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "skill %q has invalid payload schema: %v", sk.GetType(), err)
		}
		skills = append(skills, model.AgentSkill{
			Name:    sk.GetType(),
			Payload: json.RawMessage(sk.GetPayloadSchema()),
		})
	}
	s.registry.SetSkills(agentID, skills)
	slog.Info("ReportSkills: skills registered", "agent_id", agentID, "count", len(skills))

	return &agentv1.SkillRegistrationAck{}, nil
}

func (s *agentGRPCServer) GetTransformationMatrices(ctx context.Context, req *agentv1.TransformationMatrixRequest) (*agentv1.TransformationMatrixResponse, error) {
	agentID := model.AgentID(req.AgentId)
	if agentID == "" {
		agentID = agentIDFromContext(ctx)
	}

	var matrices []*agentv1.TransformationMatrix
	for _, l := range *s.layouts.Load() {
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

func authenticateMetadata(authService *service.AuthService, ctx context.Context) error {
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
	agentID := model.AgentID(agentIDVals[0])

	if !authService.Authenticate(string(agentID), token) {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
}

// AuthUnaryInterceptor validates the bearer token from gRPC metadata on every unary call.
func AuthUnaryInterceptor(authService *service.AuthService) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := authenticateMetadata(authService, ctx); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// AuthStreamInterceptor validates the bearer token from gRPC metadata on every stream open.
func AuthStreamInterceptor(authService *service.AuthService) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authenticateMetadata(authService, ss.Context()); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

// agentIDFromContext extracts the agent_id from incoming gRPC metadata.
// Safe to call after AuthStreamInterceptor has already validated it.
func agentIDFromContext(ctx context.Context) model.AgentID {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("agent_id"); len(vals) > 0 {
			return model.AgentID(vals[0])
		}
	}
	return ""
}
