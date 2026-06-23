// @title           VantageOS Core API
// @version         1.0
// @description     VantageOS backend — agent registry and task management.
// @host            localhost:8080
// @BasePath        /
package main

import (
	"log/slog"
	"net/http"

	_ "vantageos-core/docs"

	httpSwagger "github.com/swaggo/http-swagger"
)

func main() {
	slog.Info("Welcome to VantageOS core")

	cfg, err := LoadConfig("config.yaml")
	if err != nil {
		slog.Error("failed to load config", "err", err)
		return
	}

	// initialise AgentRegistry
	var allowedAgents []AllowedAgent
	for _, agent := range cfg.Agents {
		allowedAgents = append(allowedAgents, AllowedAgent{
			AgentID: agent.ID,
			Key:     agent.Key,
			Name:    agent.Name,
		})
	}
	registry := NewAgentRegistry(MQTTConfig{
		Host: "mqtt.vantageos.io",
		Port: 8883,
	}, allowedAgents)

	mux := http.NewServeMux()
	registry.RegisterRoutes(mux)
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)

	slog.Info("HTTP listening", "addr", ":8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		slog.Error("server stopped", "err", err)
	}
}
