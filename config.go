package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Agents []AgentConfig `yaml:"agents"`
}

type AgentConfig struct {
	ID   AgentID `yaml:"id"`
	Key  string  `yaml:"key"`
	Name string  `yaml:"name"`
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
