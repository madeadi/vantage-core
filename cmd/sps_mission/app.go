package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"vantageos-core/pkg/missionsdk"

	missionv1 "vantageos-core/proto/mission/v1"
)

type App struct {
	Config   Config
	Server   missionsdk.Server
	handlers map[string]DeliveryMissionHandler // keyed by delivery type, matching mission config IDs

	mu      sync.Mutex
	streams map[string]*missionsdk.MissionStream // keyed by mission ID
}

func NewApp(cfg Config, srv missionsdk.Server, handlers map[string]DeliveryMissionHandler) *App {
	return &App{
		Config:   cfg,
		Server:   srv,
		handlers: handlers,
		streams:  make(map[string]*missionsdk.MissionStream),
	}
}

// StartDelivery looks up the handler registered for delivery.Type and starts it.
func (a *App) StartDelivery(delivery Delivery) error {
	h, ok := a.handlers[string(delivery.Type)]
	if !ok {
		return fmt.Errorf("no handler registered for delivery type %q", delivery.Type)
	}
	return h.Start(delivery)
}

func (a *App) Run() {
	slog.Info("Running SPS Mission App...", "missions", len(a.Config.Missions))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var wg sync.WaitGroup
	for _, m := range a.Config.Missions {
		wg.Add(1)
		go func(m missionsdk.MissionConfig) {
			defer wg.Done()
			a.runMission(ctx, m)
		}(m)
	}

	wg.Wait()
	slog.Info("Shutting down")
}

// runMission maintains a single mission's connection to core, reconnecting
// with exponential backoff until ctx is cancelled.
func (a *App) runMission(ctx context.Context, m missionsdk.MissionConfig) {
	const backoffMax = 60 * time.Second
	backoff := time.Second

	serverCfg := missionsdk.ConnectConfig{
		MissionID: m.ID,
		Key:       m.Key,
		Name:      m.ID,
	}

	for ctx.Err() == nil {
		conn, err := a.Server.Connect(serverCfg)
		if err != nil {
			slog.Error("failed to connect", "mission_id", m.ID, "err", err)
		} else {
			slog.Info("connected to core", "mission_id", m.ID)
			connCtx, connCancel := context.WithCancel(ctx)
			client := missionv1.NewMissionServiceClient(conn)

			h := a.handlers[m.ID]
			onStatusUpdate := func(ts *missionv1.TaskStatusUpdate) {
				slog.Info("task status update", "mission_id", m.ID, "task_id", ts.TaskContext.Id, "status", ts.GetStatus())
			}
			if h != nil {
				onStatusUpdate = h.HandleTaskUpdate
			}
			ms := missionsdk.NewMissionStream(m.ID, onStatusUpdate)
			if h != nil {
				h.Bind(ms)
			}

			a.mu.Lock()
			a.streams[m.ID] = ms
			a.mu.Unlock()

			_ = ms.Run(connCtx, client)

			a.mu.Lock()
			delete(a.streams, m.ID)
			a.mu.Unlock()

			connCancel()
			conn.Close()
			backoff = time.Second
		}

		if ctx.Err() != nil {
			break
		}

		slog.Info("stream disconnected, reconnecting", "mission_id", m.ID, "in", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
		}
		if backoff < backoffMax {
			backoff *= 2
		}
	}
}
