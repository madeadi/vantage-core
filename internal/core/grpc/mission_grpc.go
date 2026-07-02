package grpc

import (
	"context"
	"io"
	"log/slog"
	"strings"
	model2 "vantageos-core/internal/core/model"
	"vantageos-core/internal/core/service"
	missionv1 "vantageos-core/proto/mission/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TaskCreator handles a CreateTask request received on a mission's stream.
type TaskCreator interface {
	OnCreateTask(missionID string, ct *missionv1.CreateTask) error
}

type MissionGrpc struct {
	missionv1.UnimplementedMissionServiceServer
	registry    *service.MissionRegistry
	taskCreator TaskCreator
}

func NewMissionGrpc(
	registry *service.MissionRegistry,
	taskCreator TaskCreator,
) missionv1.MissionServiceServer {
	return &MissionGrpc{
		registry:    registry,
		taskCreator: taskCreator,
	}
}

func (m *MissionGrpc) StreamMission(stream missionv1.MissionService_StreamMissionServer) error {
	missionID := missionIDFromContext(stream.Context())
	slog.Info("StreamMission: mission connected", "mission_id", missionID)

	m.registry.AttachStream(missionID, stream)

	defer func() {
		m.registry.DetachStream(missionID)
		slog.Info("StreamMission: mission disconnected", "mission_id", missionID)
	}()

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			slog.Error("StreamMission: recv error", "mission_id", missionID, "err", err)
			return err
		}

		switch payload := msg.Payload.(type) {
		case *missionv1.MissionClientMessage_CreateTask:
			slog.Info("create task received", "mission_id", missionID, "task_type", payload.CreateTask.Type, "requirements", payload.CreateTask.Requirement)
			if err := m.taskCreator.OnCreateTask(missionID, payload.CreateTask); err != nil {
				slog.Error("StreamMission: OnCreateTask failed", "mission_id", missionID, "err", err)
			}
		}
	}
}

func authenticateMissionMetadata(registry *service.MissionRegistry, ctx context.Context) error {
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

	missionIDVals := md.Get("mission_id")
	if len(missionIDVals) == 0 {
		return status.Error(codes.Unauthenticated, "missing mission_id")
	}
	missionID := model2.MissionID(missionIDVals[0])

	if !registry.Authenticate(missionID, token) {
		return status.Error(codes.Unauthenticated, "invalid token")
	}
	return nil
}

// AuthMissionStreamInterceptor validates the bearer token from gRPC metadata on every stream open.
func AuthMissionStreamInterceptor(registry *service.MissionRegistry) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := authenticateMissionMetadata(registry, ss.Context()); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

// missionIDFromContext extracts the mission_id from incoming gRPC metadata.
// Safe to call after AuthMissionStreamInterceptor has already validated it.
func missionIDFromContext(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("mission_id"); len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}
