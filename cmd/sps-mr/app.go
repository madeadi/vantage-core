package main

import (
	"context"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"vantageos-core/cmd/sps-mr/custom_task_handler"
	"vantageos-core/pkg/agentsdk/server"
	"vantageos-core/pkg/agentsdk/service"
	"vantageos-core/pkg/agentsdk/task_handler"
	agentv1 "vantageos-core/proto/agent/v1"
)

type App struct {
	Robot  Robot
	Config Config
	Server server.Server
}

func NewApp(robot Robot, cfg Config, srv server.Server) *App {
	return &App{
		Robot:  robot,
		Config: cfg,
		Server: srv,
	}
}

func (a *App) Run() {
	slog.Info("Welcome to SPS MR Robot")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	const backoffMax = 60 * time.Second
	backoff := time.Second

	// services
	pt := service.NewStreamPose(a.Config.AgentID, a.Robot, 500*time.Millisecond, a.Config.LayoutID)

	// Set up the task manager and register task handlers
	ds := custom_task_handler.NewDeliveryService(a.Robot, a.Robot, a.Robot)
	tm := service.NewAgentTaskManager(
		task_handler.NewGotoHandler(a.Robot),
		task_handler.NewGoHomeHandler(a.Robot),
		task_handler.NewJackHandler(a.Robot),
		custom_task_handler.NewLoadToAV(ds),
		custom_task_handler.NewUnloadFromAV(ds),
	)
	st := service.NewStreamTask(a.Config.AgentID, tm)

	serverCfg := server.ConnectConfig{
		AgentID: a.Config.AgentID,
		Key:     a.Config.Key,
		Name:    "SPS MR Robot",
		Skills:  tm.Skills(),
		Cameras: a.Config.Cameras,
	}
	for ctx.Err() == nil {
		conn, err := a.Server.Connect(serverCfg)
		if err != nil {
			slog.Error("failed to connect", "err", err)
		} else {
			client := agentv1.NewAgentServiceClient(conn)

			connCtx, connCancel := context.WithCancel(ctx)
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer connCancel() // ensure st.Run exits if pt fails
				pt.Run(connCtx, client)
			}()

			st.Run(connCtx, client)

			connCancel()
			wg.Wait()
			conn.Close()
			backoff = time.Second
		}

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

	slog.Info("Shutting down")
}
