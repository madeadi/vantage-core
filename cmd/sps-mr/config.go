package main

import (
	"os"

	"gopkg.in/yaml.v2"
)

type RobotType string

const (
	RobotTypeSlamtec RobotType = "slamtec"
	RobotTypeDummy   RobotType = "dummy"
)

type SlamtecConfig struct {
	BaseUrl string `yaml:"base_url"`
	Port    string `yaml:"port"`
}

type Config struct {
	Url       string    `yaml:"core_url"`
	AgentID   string    `yaml:"id"`
	Key       string    `yaml:"key"`
	LayoutID  string    `yaml:"layout_id"`
	RobotType RobotType `yaml:"robot_type"`

	Slamtec SlamtecConfig `yaml:"slamtec"`
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
