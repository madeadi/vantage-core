package config

import (
	"os"
	model2 "vantageos-core/cmd/core/model"

	"gopkg.in/yaml.v3"
)

// Config holds only the bootstrap settings that must be known before the
// embedded PocketBase instance is up. Everything else — the allowed agents,
// missions, layouts and agent↔layout transforms — lives in PocketBase
// collections and is loaded (and live-reloaded) from there. See cmd/core/pbconfig.go.
type Config struct {
	GRPCListenAddr    string           `yaml:"grpc_listen_addr"`
	GRPCAdvertiseAddr string           `yaml:"grpc_advertise_addr"`
	PocketBase        PocketBaseConfig `yaml:"pocketbase"`
	MQTT              MQTTConfig       `yaml:"mqtt"`
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
