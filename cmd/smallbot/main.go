package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"
	"vantageos-core/pkg/agentsdk"
	agentv1 "vantageos-core/proto/agent/v1"

	"os/signal"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"gopkg.in/yaml.v3"
)

type Config struct {
	AgentID   string `yaml:"id"`
	AgentKey  string `yaml:"key"`
	AgentName string `yaml:"name"`
	CoreURL   string `yaml:"core_url"`
	GRPCAddr  string `yaml:"grpc_addr"`
}

type registerRequest struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Skills       []interface{} `json:"skills"`
	EventSources []interface{} `json:"event_sources"`
}

func loadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func register(cfg *Config) (*agentsdk.RegisterResponse, error) {
	body, err := json.Marshal(registerRequest{
		ID:           cfg.AgentID,
		Name:         cfg.AgentName,
		Skills:       []interface{}{},
		EventSources: []interface{}{},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, cfg.CoreURL+"/agents/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.AgentKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registration rejected: status %d", resp.StatusCode)
	}

	var regResp agentsdk.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return nil, err
	}
	return &regResp, nil
}

// tokenCreds injects the bearer token into every gRPC call's metadata.
type tokenCreds struct {
	token string
}

func (t tokenCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + t.token}, nil
}

func (t tokenCreds) RequireTransportSecurity() bool { return false }

// taskRecord is the result of a completed or aborted task kept in taskState.history.
type taskRecord struct {
	status agentv1.TaskStatus
	result []byte
}

// taskState holds in-memory task history and current execution state. It is
// created once in main and passed through reconnect cycles so history survives
// transient network disconnects.
type taskState struct {
	mu            sync.Mutex
	sendMu        sync.Mutex // serialises all stream.Send calls
	currentTaskID string
	currentCancel context.CancelFunc
	history       map[string]taskRecord
	stream        agentv1.AgentService_StreamTasksClient
}

func newTaskState() *taskState {
	return &taskState{history: make(map[string]taskRecord)}
}

func (ts *taskState) setStream(s agentv1.AgentService_StreamTasksClient) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.stream = s
}

// send serialises writes to the active stream. gRPC client streams must not
// have concurrent Send calls.
func (ts *taskState) send(ack *agentv1.TaskAck) error {
	ts.sendMu.Lock()
	defer ts.sendMu.Unlock()
	ts.mu.Lock()
	s := ts.stream
	ts.mu.Unlock()
	if s == nil {
		return errors.New("no active stream")
	}
	return s.Send(ack)
}

// finishTask records the terminal status in history and clears currentTaskID.
func (ts *taskState) finishTask(taskID string, status agentv1.TaskStatus, result []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.history[taskID] = taskRecord{status: status, result: result}
	if ts.currentTaskID == taskID {
		ts.currentTaskID = ""
		ts.currentCancel = nil
	}
}

func runStreamTasks(ctx context.Context, client agentv1.AgentServiceClient, agentID string, ts *taskState) {
	md := metadata.Pairs("agent_id", agentID)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.StreamTasks(streamCtx)
	if err != nil {
		slog.Error("failed to open StreamTasks", "err", err)
		return
	}
	ts.setStream(stream)
	defer ts.setStream(nil)
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

			ts.mu.Lock()

			// Case 1: this task is already executing — re-ack so core knows.
			if ts.currentTaskID == task.Id {
				ts.mu.Unlock()
				slog.Info("task already executing, re-acking", "id", task.Id)
				if err := ts.send(&agentv1.TaskAck{
					TaskId: task.Id,
					Status: agentv1.TaskStatus_TASK_STATUS_STARTED,
				}); err != nil {
					slog.Error("failed to send STARTED re-ack", "err", err)
				}
				continue
			}

			// Case 2: task was already completed — report the stored status.
			if rec, ok := ts.history[task.Id]; ok {
				ts.mu.Unlock()
				slog.Info("task found in history, reporting status", "id", task.Id, "status", rec.status)
				if err := ts.send(&agentv1.TaskAck{
					TaskId: task.Id,
					Status: rec.status,
					Result: rec.result,
				}); err != nil {
					slog.Error("failed to send historical ack", "err", err)
				}
				continue
			}

			// Case 3: genuinely new task — cancel any prior task and start this one.
			if ts.currentCancel != nil {
				ts.currentCancel()
			}
			taskCtx, cancel := context.WithCancel(ctx)
			ts.currentTaskID = task.Id
			ts.currentCancel = cancel
			ts.mu.Unlock()

			go func(t *agentv1.Task, tCtx context.Context) {
				if err := ts.send(&agentv1.TaskAck{
					TaskId: t.Id,
					Status: agentv1.TaskStatus_TASK_STATUS_STARTED,
				}); err != nil {
					slog.Error("failed to send STARTED ack", "err", err)
					return
				}

				// simulate work — replace with real task execution
				select {
				case <-time.After(5 * time.Second):
					ts.finishTask(t.Id, agentv1.TaskStatus_TASK_STATUS_FINISHED, nil)
					if err := ts.send(&agentv1.TaskAck{
						TaskId: t.Id,
						Status: agentv1.TaskStatus_TASK_STATUS_FINISHED,
					}); err != nil {
						slog.Error("failed to send FINISHED ack", "err", err)
					}
					slog.Info("task finished", "id", t.Id)
				case <-tCtx.Done():
					// cancelled by an AbortCommand; abort ack is sent in the abort case below
				}
			}(task, taskCtx)

		case *agentv1.ServerMessage_Abort:
			slog.Info("abort received", "task_id", payload.Abort.TaskId)
			ts.mu.Lock()
			if ts.currentCancel != nil && ts.currentTaskID == payload.Abort.TaskId {
				ts.currentCancel()
			}
			ts.mu.Unlock()
			ts.finishTask(payload.Abort.TaskId, agentv1.TaskStatus_TASK_STATUS_ABORTED, nil)
			if err := ts.send(&agentv1.TaskAck{
				TaskId: payload.Abort.TaskId,
				Status: agentv1.TaskStatus_TASK_STATUS_ABORTED,
			}); err != nil {
				slog.Error("failed to send ABORTED ack", "err", err)
			}
		}
	}
}

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

func connectAndRun(ctx context.Context, grpcAddr string, agentID string, token string, ts *taskState) {
	conn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(tokenCreds{token: token}),
	)
	if err != nil {
		slog.Error("failed to dial gRPC", "err", err)
		return
	}
	defer conn.Close()

	client := agentv1.NewAgentServiceClient(conn)

	go runTelemetry(ctx, client, agentID)
	runStreamTasks(ctx, client, agentID, ts)
}

func main() {
	configPath := flag.String("config", "smallbot.config.yaml", "path to config file")
	flag.Parse()

	slog.Info("Welcome to smallbot")

	cfg, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	ts := newTaskState()

	const backoffMax = 60 * time.Second
	backoff := time.Second

	for ctx.Err() == nil {
		regResp, err := register(cfg)
		if err != nil {
			slog.Error("registration failed, will retry", "err", err, "in", backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
			}
			if backoff < backoffMax {
				backoff *= 2
			}
			continue
		}
		slog.Info("registered", "agent_id", regResp.AgentID)
		backoff = time.Second

		connectAndRun(ctx, regResp.GRPCAddr, regResp.AgentID, regResp.Token, ts)

		if ctx.Err() != nil {
			break
		}
		slog.Info("stream disconnected, reconnecting", "in", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
		}
		if backoff < backoffMax {
			backoff *= 2
		}
	}

	slog.Info("shutting down")
}
