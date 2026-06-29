// Package service contains the long-running gRPC streaming loops that connect
// an agent process to the core server.
package service

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
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
}

// NewStreamTask returns a StreamTask that identifies itself to the server
// with the given agentID.
func NewStreamTask(agentID string) *StreamTask {
	return &StreamTask{agentID: agentID}
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
// For each incoming Task it spawns a goroutine that sends a STARTED ack,
// simulates work, then sends a FINISHED ack. If an AbortCommand arrives while
// a task is running, the goroutine is cancelled via context and an ABORTED ack
// is sent immediately.
func (s *StreamTask) Run(ctx context.Context, client agentv1.AgentServiceClient) {
	md := metadata.Pairs("agent_id", s.agentID)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.StreamTasks(streamCtx)
	if err != nil {
		slog.Error("failed to open StreamTasks", "err", err)
		return
	}
	slog.Info("StreamTasks stream open")

	var currentTaskCancel context.CancelFunc

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

			if currentTaskCancel != nil {
				currentTaskCancel()
			}
			taskCtx, cancel := context.WithCancel(ctx)
			currentTaskCancel = cancel

			go func(t *agentv1.Task, tCtx context.Context) {
				defer cancel()
				if err := s.send(stream, &agentv1.TaskAck{
					TaskId: t.Id,
					Status: agentv1.TaskStatus_TASK_STATUS_STARTED,
				}); err != nil {
					slog.Error("failed to send STARTED ack", "err", err)
					return
				}

				// simulate work — replace with real task execution
				select {
				case <-time.After(5 * time.Second):
					// Guard against the race where abort fired at the same instant.
					select {
					case <-tCtx.Done():
					default:
						if err := s.send(stream, &agentv1.TaskAck{
							TaskId: t.Id,
							Status: agentv1.TaskStatus_TASK_STATUS_FINISHED,
						}); err != nil {
							slog.Error("failed to send FINISHED ack", "err", err)
						}
						slog.Info("task finished", "id", t.Id)
					}
				case <-tCtx.Done():
					// cancelled by AbortCommand
				}
			}(task, taskCtx)

		case *agentv1.ServerMessage_Abort:
			slog.Info("abort received", "task_id", payload.Abort.TaskId)
			if currentTaskCancel != nil {
				currentTaskCancel()
				currentTaskCancel = nil
			}
			if err := s.send(stream, &agentv1.TaskAck{
				TaskId: payload.Abort.TaskId,
				Status: agentv1.TaskStatus_TASK_STATUS_ABORTED,
			}); err != nil {
				slog.Error("failed to send ABORTED ack", "err", err)
			}
		}
	}
}
