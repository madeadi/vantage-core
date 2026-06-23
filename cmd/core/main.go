// @title           VantageOS Core API
// @version         1.0
// @description     VantageOS backend — agent registry and task management.
// @host            localhost:8080
// @BasePath        /
package main

import (
	"flag"
	"log/slog"
	"net/http"

	_ "vantageos-core/docs"
	"vantageos-core/pkg/pubsub"

	httpSwagger "github.com/swaggo/http-swagger"
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
	registry := NewAgentRegistry(hub, allowedAgents)

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

	slog.Info("HTTP listening", "addr", ":8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		slog.Error("server stopped", "err", err)
	}
}
