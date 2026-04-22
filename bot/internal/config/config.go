package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TelegramToken            string  `yaml:"telegram_token"`
	ChatIDs                  []int64 `yaml:"chat_ids"`
	ServerURL                string  `yaml:"server_url"`
	APIToken                 string  `yaml:"api_token"`
	PollIntervalSeconds      int     `yaml:"poll_interval_seconds"`
	HeartbeatIntervalSeconds int     `yaml:"heartbeat_interval_seconds"`
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

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	var problems []string
	if isPlaceholder(c.TelegramToken) {
		problems = append(problems, "telegram_token is required and must not be a placeholder")
	}
	if len(c.ChatIDs) == 0 {
		problems = append(problems, "chat_ids must contain at least one allowed chat id")
	}
	if !validHTTPURL(c.ServerURL) {
		problems = append(problems, "server_url must be an absolute http(s) URL")
	}
	if isPlaceholder(c.APIToken) {
		problems = append(problems, "api_token is required and must not be a placeholder")
	}
	if c.PollIntervalSeconds < 5 || c.PollIntervalSeconds > 3600 {
		problems = append(problems, "poll_interval_seconds must be between 5 and 3600")
	}
	if c.HeartbeatIntervalSeconds < 30 || c.HeartbeatIntervalSeconds > 86400 {
		problems = append(problems, "heartbeat_interval_seconds must be between 30 and 86400")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid bot config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func validHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func isPlaceholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	upper := strings.ToUpper(trimmed)
	return trimmed == "" || upper == "CHANGE_ME" || upper == "BOT_TOKEN_HERE" || strings.Contains(upper, "YOUR-TOKEN-HERE")
}
