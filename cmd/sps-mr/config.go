package main

import (
	"os"
	"vantageos-core/pkg/agentsdk"

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
	agentsdk.BaseConfig   `yaml:",inline"`
	agentsdk.LayoutConfig `yaml:",inline"`

	RobotType RobotType               `yaml:"robot_type"`
	Slamtec   SlamtecConfig           `yaml:"slamtec"`
	Cameras   []agentsdk.CameraConfig `yaml:"cameras"`
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
