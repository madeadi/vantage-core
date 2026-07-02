package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"vantageos-core/pkg/missionsdk"

	"gopkg.in/yaml.v3"
)

func main() {
	configPath := flag.String("config", "spsmission.config.yaml", "path to config file")
	flag.Parse()
	slog.Info("Welcome to SPS Mission!", "configPath", *configPath)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return
	}

	srv := missionsdk.Server{
		CoreURL: cfg.Core.CoreUrl,
	}

	dr := NewMemoryRepo()
	svc := NewSPSService(dr)

	handlers := map[string]DeliveryMissionHandler{
		DeliveryTypeFromKitchen: NewMissionFromKitchen(cfg.FromKitchen, dr),
		DeliveryTypeToKitchen:   NewMissionToKitchen(cfg.ToKitchen, dr),
	}

	app := NewApp(*cfg, srv, handlers)

	ctrl := NewController(svc, app)
	mux := http.NewServeMux()
	ctrl.RegisterRoutes(mux)
	go func() {
		port := ":" + cfg.Http.Port
		slog.Info("sps_mission HTTP listening", "addr", port)
		if err := http.ListenAndServe(port, mux); err != nil {
			slog.Error("sps_mission HTTP server stopped", "err", err)
		}
	}()

	app.Run()
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
