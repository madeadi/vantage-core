package config

import (
	"os"
	"time"
	model2 "vantageos-core/cmd/core/model"

	"gopkg.in/yaml.v3"
)

// Config holds only the bootstrap settings that must be known before the
// embedded PocketBase instance is up. Everything else — the allowed agents,
// missions, layouts and agent↔layout transforms — lives in PocketBase
// collections and is loaded (and live-reloaded) from there. See cmd/core/pbconfig.go.
type Config struct {
	// HTTPListenAddr is the address the core HTTP mux (agent/mission/task/
	// telemetry APIs, admin UI, swagger) listens on. Falls back to ":8080"
	// when unset, same pattern as GRPCListenAddr.
	HTTPListenAddr    string           `yaml:"http_listen_addr"`
	GRPCListenAddr    string           `yaml:"grpc_listen_addr"`
	GRPCAdvertiseAddr string           `yaml:"grpc_advertise_addr"`
	PocketBase        PocketBaseConfig `yaml:"pocketbase"`
	MQTT              MQTTConfig       `yaml:"mqtt"`
	Telemetry         TelemetryConfig  `yaml:"telemetry"`
}

type PocketBaseConfig struct {
	Enabled    bool   `yaml:"enabled"`
	ListenAddr string `yaml:"listen_addr"`
	DataDir    string `yaml:"data_dir"`
}

// MQTTConfig is core's own connection to the broker agents publish to — the
// credentials for the telemetry ingest daemon (see cmd/core/telemetry/ingest),
// not an agent's. Off by default: MQTT telemetry is additive to the existing
// gRPC agent path (see specs/mqtt_telemetry.specs.md Phase A), so a core
// instance with no broker configured keeps running exactly as it did before
// this feature existed.
type MQTTConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Broker      string `yaml:"broker"`    // e.g. "tcp://127.0.0.1:1883"
	ClientID    string `yaml:"client_id"` // core's own mqtt client id
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	TopicPrefix string `yaml:"topic_prefix"` // must match the prefix agents publish under
	// BrokerAdvertiseURL is the broker address handed to agents at
	// registration (spec Step 15), same reasoning as
	// grpc_listen_addr/grpc_advertise_addr: core's own connection address
	// (Broker, above) may not be what a remote agent should dial (e.g. an
	// internal hostname vs. a public one). Falls back to Broker when unset,
	// so a deployment where core and agents share the same reachable
	// address needs only one setting.
	BrokerAdvertiseURL string `yaml:"broker_advertise_url"`
}

// TelemetryConfig configures Step 12's Postgres/TimescaleDB persistence and
// (ThrottleWindow) retrofits Step 9's event throttling window into the same
// bootstrap-only block -- see specs/mqtt_telemetry.specs.md's Ops section.
// Off by default, same reasoning as MQTTConfig: a core instance with
// persistence_enabled: false keeps validating and emitting agent_events
// exactly as before, it just doesn't write telemetry rows anywhere.
//
// The per-group persist toggle (telemetry_settings.persist_enabled, live in
// PocketBase -- see cmd/core/telemetry/persistcfg) is a second gate on top
// of this one: both must be on for a given group's telemetry to be written.
type TelemetryConfig struct {
	PersistenceEnabled bool `yaml:"persistence_enabled"`
	// DSN is the Postgres/TimescaleDB connection string, e.g.
	// "postgres://user:pass@host:5432/dbname". Bootstrap-only, per CLAUDE.md
	// -- unlike the config collections, this database isn't PocketBase, so
	// there's no live-editable row to source it from.
	DSN string `yaml:"dsn"`
	// BatchSize/FlushInterval configure the batch writer (see
	// cmd/core/telemetry/store.Config); zero values fall back to that
	// package's own defaults (100 rows / 1s).
	BatchSize     int           `yaml:"batch_size"`
	FlushInterval time.Duration `yaml:"flush_interval"`
	// Retention overrides the telemetry hypertable's retention policy
	// (default 90 days, baked into migrations/0001_init.sql as the sane
	// starting point -- see that file's doc comment). A non-zero value here
	// re-applies the policy at startup via store.ApplyRetentionPolicy so a
	// deployment can change it without hand-editing SQL.
	Retention time.Duration `yaml:"retention"`
	// ThrottleWindow configures Step 9's event throttling window (see
	// cmd/core/telemetry/events.Config.Window); zero falls back to that
	// package's own default (60s). Listed under "telemetry:" per the Ops
	// section even though throttling itself doesn't depend on persistence
	// being enabled.
	ThrottleWindow time.Duration `yaml:"throttle_window"`
}

// AgentConfig is one row of the "agents" PocketBase collection.
type AgentConfig struct {
	ID   model2.AgentID
	Key  string
	Name string
}

// MissionConfig is one row of the "missions" PocketBase collection.
type MissionConfig struct {
	ID   string
	Name string
	Key  string
}

// LayoutConfig is one row of the "layouts" PocketBase collection. It is not
// consumed by core today (it backs the admin UI) but is loaded for completeness.
type LayoutConfig struct {
	ID               string
	Name             string
	CoordinateSystem string
}

// AgentLayoutConfig is one row of the "agent_layouts" PocketBase collection.
type AgentLayoutConfig struct {
	LayoutID             string
	AgentID              model2.AgentID
	NorthOffset          float64
	TransformationMatrix [][]float64
}

func LoadConfig(path string) (*Config, error) {
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
