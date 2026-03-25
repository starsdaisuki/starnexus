package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port                    int    `yaml:"port"`
	DBPath                  string `yaml:"db_path"`
	APIToken                string `yaml:"api_token"`
	WebDir                  string `yaml:"web_dir"`
	OfflineThresholdSeconds int    `yaml:"offline_threshold_seconds"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:                    8900,
		DBPath:                  "./starnexus.db",
		WebDir:                  "./web",
		OfflineThresholdSeconds: 90,
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
