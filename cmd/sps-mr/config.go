package main

import (
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Url     string `yaml:"core_url"`
	AgentID string `yaml:"id"`
	Key     string `yaml:"key"`
	Name    string `yaml:"name"`
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
