// Package service contains the long-running gRPC streaming loops that connect
// an agentsdk process to the core server.
package service

import (
	"context"
	"io"
	"log/slog"
	"sync"
	agentv1 "vantageos-core/proto/agent/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// StreamTask manages the bidirectional StreamTasks gRPC stream.
// It receives [agentv1.Task] and [agentv1.AbortCommand] messages from the
// server and acknowledges each one with a [agentv1.TaskAck].
type StreamTask struct {
	agentID string
	sendMu  sync.Mutex

	tm *AgentTaskManager
}

// NewStreamTask returns a StreamTask that identifies itself to the server
// with the given agentID.
func NewStreamTask(agentID string, tm *AgentTaskManager) *StreamTask {
	return &StreamTask{agentID: agentID, tm: tm}
}

// send serialises all stream.Send calls — gRPC prohibits concurrent sends on the same stream.
func (s *StreamTask) send(stream grpc.BidiStreamingClient[agentv1.TaskAck, agentv1.ServerMessage], ack *agentv1.TaskAck) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return stream.Send(ack)
}

// Run opens the StreamTasks bidirectional stream and processes messages until
// ctx is cancelled or the stream closes.
//
// For each incoming Task it delegates to the AgentTaskManager, which spawns a
// goroutine, sends a STARTED ack, executes the handler, then sends FINISHED or
// FAILED. If an AbortCommand arrives while a task is running the manager
// cancels it and an ABORTED ack is sent immediately.
func (s *StreamTask) Run(ctx context.Context, client agentv1.AgentServiceClient) {
	md := metadata.Pairs("agent_id", s.agentID)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.StreamTasks(streamCtx)
	if err != nil {
		slog.Error("failed to open StreamTasks", "err", err)
		return
	}
	slog.Info("StreamTasks stream open")

	for {
		msg, err := stream.Recv()
		if err == io.EOF || ctx.Err() != nil {
			slog.Info("StreamTasks stream closed")
			return
		}
		if err != nil {
			slog.Error("StreamTasks recv error", "err", err)
			return
		}

		switch payload := msg.Payload.(type) {
		case *agentv1.ServerMessage_Task:
			task := payload.Task
			slog.Info("task received", "id", task.Id, "type", task.Type)

			onAck := func(ack *agentv1.TaskAck) {
				if err := s.send(stream, ack); err != nil {
					slog.Error("failed to send task ack", "task_id", ack.TaskId, "status", ack.Status, "err", err)
				}
			}

			if err := s.tm.HandleTask(ctx, task, onAck); err != nil {
				slog.Error("cannot start task", "id", task.Id, "type", task.Type, "err", err)
				_ = s.send(stream, &agentv1.TaskAck{
					TaskId:       task.Id,
					Status:       agentv1.TaskStatus_TASK_STATUS_CANNOT_START,
					ErrorMessage: err.Error(),
				})
			}

		case *agentv1.ServerMessage_Abort:
			taskID := payload.Abort.TaskId
			slog.Info("abort received", "task_id", taskID)
			if s.tm.AbortCurrentTask(taskID) {
				if err := s.send(stream, &agentv1.TaskAck{
					TaskId: taskID,
					Status: agentv1.TaskStatus_TASK_STATUS_ABORTED,
				}); err != nil {
					slog.Error("failed to send ABORTED ack", "err", err)
				}
			} else {
				slog.Warn("abort received for task that is not running", "task_id", taskID)
			}
		}
	}
}
