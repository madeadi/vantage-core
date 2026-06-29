package main

import (
	"flag"
	"fmt"
	"log/slog"
	"vantageos-core/pkg/agent/server"
	dummybot "vantageos-core/pkg/agent_skill/dummy_bot"
	"vantageos-core/pkg/agent_skill/slamtec"
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

	var robot Robot

	if cfg.RobotType == RobotTypeSlamtec {
		slog.Info("Slamtec config", "baseURL", cfg.Slamtec.BaseUrl, "port", cfg.Slamtec.Port)
		robot = slamtec.New(cfg.Slamtec.BaseUrl, cfg.Slamtec.Port)
	} else {
		slog.Info("DummyBot config")
		robot = dummybot.New()
	}

	app := NewApp(robot, *cfg, *server)

	fmt.Println(app)

	app.Run()
}
