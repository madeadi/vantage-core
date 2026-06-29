package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Agents            []AgentConfig       `yaml:"agents"`
	AgentLayouts      []AgentLayoutConfig `yaml:"agent_layouts"`
	GRPCListenAddr    string              `yaml:"grpc_listen_addr"`
	GRPCAdvertiseAddr string              `yaml:"grpc_advertise_addr"`
}

type AgentConfig struct {
	ID   AgentID `yaml:"id"`
	Key  string  `yaml:"key"`
	Name string  `yaml:"name"`
}

type AgentLayoutConfig struct {
	LayoutID             string      `yaml:"layout_id"`
	AgentID              AgentID     `yaml:"agent_id"`
	NorthOffset          float64     `yaml:"north_offset"`
	TransformationMatrix [][]float64 `yaml:"transformation_matrix"`
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
