package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ServerURL             string  `yaml:"server_url"`
	APIToken              string  `yaml:"api_token"`
	NodeID                string  `yaml:"node_id"`
	NodeName              string  `yaml:"node_name"`
	Provider              string  `yaml:"provider"`
	Latitude              float64 `yaml:"latitude"`
	Longitude             float64 `yaml:"longitude"`
	ReportIntervalSeconds int     `yaml:"report_interval_seconds"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		ReportIntervalSeconds: 30,
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
