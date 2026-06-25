// @title           VantageOS Core API
// @version         1.0
// @description     VantageOS backend — agent registry and task management.
// @host            localhost:8080
// @BasePath        /
package main

import (
	"flag"
	"log/slog"
	"net"
	"net/http"

	_ "vantageos-core/docs"
	"vantageos-core/pkg/pubsub"
	agentv1 "vantageos-core/proto/agent/v1"

	httpSwagger "github.com/swaggo/http-swagger"
	"google.golang.org/grpc"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	slog.Info("Welcome to VantageOS core")

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		return
	}

	hub := pubsub.NewWSHub()

	var allowedAgents []AllowedAgent
	for _, agent := range cfg.Agents {
		allowedAgents = append(allowedAgents, AllowedAgent{
			AgentID: agent.ID,
			Key:     agent.Key,
			Name:    agent.Name,
		})
	}
	grpcListenAddr := cfg.GRPCListenAddr
	if grpcListenAddr == "" {
		grpcListenAddr = ":9090"
	}
	grpcAdvertiseAddr := cfg.GRPCAdvertiseAddr
	if grpcAdvertiseAddr == "" {
		grpcAdvertiseAddr = "localhost:9090"
	}
	registry := NewAgentRegistry(hub, allowedAgents, grpcAdvertiseAddr)
	tm := &TaskManager{sender: registry, currentTasks: make(map[AgentID]*Task)}
	_ = tm // wired; expose via HTTP handlers in a follow-on

	mux := http.NewServeMux()
	registry.RegisterRoutes(mux)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		agentID := AgentID(r.URL.Query().Get("agent_id"))
		token := r.URL.Query().Get("token")
		if !registry.Authenticate(agentID, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		hub.ServeHTTP(w, r)
	})
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)

	// gRPC server
	lis, err := net.Listen("tcp", grpcListenAddr)
	if err != nil {
		slog.Error("failed to listen on gRPC port", "err", err)
		return
	}
	grpcServer := grpc.NewServer(
		grpc.StreamInterceptor(authStreamInterceptor(registry)),
	)
	telemetry := NewTelemetryListener()
	agentv1.RegisterAgentServiceServer(grpcServer, &agentGRPCServer{registry: registry, telemetry: telemetry})
	go func() {
		slog.Info("gRPC listening", "addr", grpcListenAddr)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC server stopped", "err", err)
		}
	}()

	slog.Info("HTTP listening", "addr", ":8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		slog.Error("server stopped", "err", err)
	}
}
