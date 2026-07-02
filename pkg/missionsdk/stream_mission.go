package missionsdk

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	missionv1 "vantageos-core/proto/mission/v1"

	"google.golang.org/grpc/metadata"
)

// MissionStream manages the bidirectional StreamMission gRPC stream.
// It sends CreateTask requests to the server and receives TaskStatusUpdate
// events for tasks the mission has created.
type MissionStream struct {
	missionID string
	sendMu    sync.Mutex
	stream    missionv1.MissionService_StreamMissionClient

	onStatusUpdate func(*missionv1.TaskStatusUpdate)
}

// NewMissionStream returns a MissionStream that identifies itself to the
// server with the given missionID. onStatusUpdate is invoked for every
// TaskStatusUpdate received on the stream.
func NewMissionStream(missionID string, onStatusUpdate func(*missionv1.TaskStatusUpdate)) *MissionStream {
	return &MissionStream{missionID: missionID, onStatusUpdate: onStatusUpdate}
}

// Run opens the StreamMission bidirectional stream and processes messages
// until ctx is cancelled or the stream closes.
func (m *MissionStream) Run(ctx context.Context, client missionv1.MissionServiceClient) error {
	md := metadata.Pairs("mission_id", m.missionID)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.StreamMission(streamCtx)
	if err != nil {
		slog.Error("failed to open StreamMission", "err", err)
		return err
	}
	slog.Info("StreamMission stream open")

	m.sendMu.Lock()
	m.stream = stream
	m.sendMu.Unlock()

	defer func() {
		m.sendMu.Lock()
		m.stream = nil
		m.sendMu.Unlock()
	}()

	for {
		msg, err := stream.Recv()
		if err == io.EOF || ctx.Err() != nil {
			slog.Info("StreamMission stream closed")
			return nil
		}
		if err != nil {
			slog.Error("StreamMission recv error", "err", err)
			return err
		}

		switch payload := msg.Payload.(type) {
		case *missionv1.MissionServerMessage_TaskStatusUpdate:
			if m.onStatusUpdate != nil {
				m.onStatusUpdate(payload.TaskStatusUpdate)
			}
		}
	}
}

// CreateTask sends a CreateTask request on the currently open stream.
func (m *MissionStream) CreateTask(ct *missionv1.CreateTask) error {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	if m.stream == nil {
		return errors.New("mission stream not connected")
	}
	return m.stream.Send(&missionv1.MissionClientMessage{
		Payload: &missionv1.MissionClientMessage_CreateTask{CreateTask: ct},
	})
}
