package main

import (
	"context"
	"log/slog"
	"time"

	"vantageos-core/cmd/core/config"
	"vantageos-core/cmd/core/telemetry/ingest"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// ingestQueueSize is sized for ~100s of headroom at this project's
// 10-agent/1Hz scale target (see specs/mqtt_telemetry.specs.md), not for
// sustained overload at a materially higher rate.
const ingestQueueSize = 1024

// startTelemetryIngest connects core's own mqtt client and starts the
// telemetry ingest daemon (cmd/core/telemetry/ingest) in the background.
//
// Connection failures are logged, not fatal: MQTT telemetry is additive to
// the existing gRPC agent path, so a broker that's down or misconfigured
// must not take the rest of core down with it. SetAutoReconnect and
// SetConnectRetry mean the client keeps trying rather than giving up after
// the first failure, and OnConnect re-subscribes on every connect (including
// a reconnect), which is a harmless no-op if the subscription is already
// live server-side.
func startTelemetryIngest(cfg config.MQTTConfig) *ingest.Daemon {
	daemon := ingest.New(cfg.TopicPrefix, ingestQueueSize)

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectTimeout(10 * time.Second)

	opts.OnConnect = func(client mqtt.Client) {
		slog.Info("telemetry ingest: mqtt connected", "broker", cfg.Broker)
		if err := daemon.Subscribe(client); err != nil {
			slog.Error("telemetry ingest: subscribe failed", "error", err)
		}
	}
	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		slog.Error("telemetry ingest: mqtt connection lost", "error", err)
	}

	client := mqtt.NewClient(opts)
	go func() {
		token := client.Connect()
		token.Wait()
		if err := token.Error(); err != nil {
			slog.Error("telemetry ingest: initial connect failed, will keep retrying", "error", err)
		}
	}()

	go daemon.Run(context.Background())

	return daemon
}
