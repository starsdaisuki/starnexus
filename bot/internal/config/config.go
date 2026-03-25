package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TelegramToken            string `yaml:"telegram_token"`
	ChatIDs                  []int64 `yaml:"chat_ids"`
	ServerURL                string `yaml:"server_url"`
	APIToken                 string `yaml:"api_token"`
	PollIntervalSeconds      int    `yaml:"poll_interval_seconds"`
	HeartbeatIntervalSeconds int    `yaml:"heartbeat_interval_seconds"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		PollIntervalSeconds:      30,
		HeartbeatIntervalSeconds: 300,
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
