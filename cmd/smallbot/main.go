package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"syscall"
	"time"
	"vantageos-core/pkg/agent"
	agentv1 "vantageos-core/proto/agent/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"gopkg.in/yaml.v3"
	"os/signal"
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

func register(cfg *Config) (*agent.RegisterResponse, error) {
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

	var regResp agent.RegisterResponse
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

func runStreamTasks(ctx context.Context, client agentv1.AgentServiceClient, agentID string) {
	md := metadata.Pairs("agent_id", agentID)
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
				if err := stream.Send(&agentv1.TaskAck{
					TaskId: t.Id,
					Status: agentv1.TaskStatus_TASK_STATUS_STARTED,
				}); err != nil {
					slog.Error("failed to send STARTED ack", "err", err)
					return
				}

				// simulate work — replace with real task execution
				select {
				case <-time.After(5 * time.Second):
					if err := stream.Send(&agentv1.TaskAck{
						TaskId: t.Id,
						Status: agentv1.TaskStatus_TASK_STATUS_FINISHED,
					}); err != nil {
						slog.Error("failed to send FINISHED ack", "err", err)
					}
					slog.Info("task finished", "id", t.Id)
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
			if err := stream.Send(&agentv1.TaskAck{
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

func connectAndRun(ctx context.Context, grpcAddr string, agentID string, token string) {
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
	runStreamTasks(ctx, client, agentID)
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

	regResp, err := register(cfg)
	if err != nil {
		slog.Error("registration failed", "err", err)
		return
	}
	slog.Info("registered", "agent_id", regResp.AgentID)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	const backoffMax = 30 * time.Second
	backoff := time.Second

	for ctx.Err() == nil {
		connectAndRun(ctx, regResp.GRPCAddr, regResp.AgentID, regResp.Token)
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
