package main

import (
	"flag"
	"fmt"
	"log/slog"
	"vantageos-core/pkg/agent/server"
	dummybot "vantageos-core/pkg/agent_skill/dummy_bot"
)

func main() {

	configPath := flag.String("config", "spsmr.config.yaml", "path to config file")
	flag.Parse()
	slog.Info("Welcome to SPS MR", "configPath", *configPath)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return
	}

	server := &server.Server{
		CoreURL: cfg.Url,
	}

	robot := dummybot.New()
	app := NewApp(robot, *cfg, *server)

	fmt.Println(app)

	app.Run()
}
