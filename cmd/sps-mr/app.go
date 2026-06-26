package main

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"
	"time"
	"vantageos-core/pkg/agent/server"
	agentv1 "vantageos-core/proto/agent/v1"
)

type App struct {
	// Robot  Robot
	Config Config
	Server server.Server

	client agentv1.AgentServiceClient
}

func (a *App) Run() {
	slog.Info("Welcome to SPS MR Robot")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	const backoffMax = 60 * time.Second
	backoff := time.Second

	serverCfg := server.ConnectConfig{
		AgentID: a.Config.AgentID,
		Key:     a.Config.Key,
		Name:    "SPS MR Robot",
	}
	for ctx.Err() == nil {
		conn, err := a.Server.Connect(serverCfg)
		if err != nil {
			slog.Error("failed to connect", "err", err)
		} else {
			a.client = agentv1.NewAgentServiceClient(conn)
			runTelemetry(ctx, a.client, a.Config.AgentID)
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
