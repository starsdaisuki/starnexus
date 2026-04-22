package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port                    int     `yaml:"port"`
	DBPath                  string  `yaml:"db_path"`
	APIToken                string  `yaml:"api_token"`
	WebDir                  string  `yaml:"web_dir"`
	NodeLocationsPath       string  `yaml:"node_locations_path"`
	ExperimentLabelsPath    string  `yaml:"experiment_labels_path"`
	AgentBinaryPath         string  `yaml:"agent_binary_path"`
	GeoIPDBPath             string  `yaml:"geoip_db_path"`
	OfflineThresholdSeconds int     `yaml:"offline_threshold_seconds"`
	BotToken                string  `yaml:"bot_token"`
	BotChatIDs              []int64 `yaml:"bot_chat_ids"`
	MistralAPIKey           string  `yaml:"mistral_api_key"`
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
		ExperimentLabelsPath:    "./analysis-output/experiments.jsonl",
		OfflineThresholdSeconds: 90,
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
	if c.Port <= 0 || c.Port > 65535 {
		problems = append(problems, "port must be between 1 and 65535")
	}
	if strings.TrimSpace(c.DBPath) == "" {
		problems = append(problems, "db_path is required")
	}
	if isPlaceholder(c.APIToken) {
		problems = append(problems, "api_token is required and must not be a placeholder")
	}
	if c.OfflineThresholdSeconds < 30 || c.OfflineThresholdSeconds > 3600 {
		problems = append(problems, "offline_threshold_seconds must be between 30 and 3600")
	}
	if (strings.TrimSpace(c.BotToken) != "" || len(c.BotChatIDs) > 0) && isPlaceholder(c.BotToken) {
		problems = append(problems, "bot_token must be set when bot_chat_ids are configured")
	}
	if strings.TrimSpace(c.BotToken) != "" && len(c.BotChatIDs) == 0 {
		problems = append(problems, "bot_chat_ids must contain at least one chat id when bot_token is set")
	}
	if strings.Contains(strings.ToUpper(c.MistralAPIKey), "MISTRAL_KEY_HERE") {
		problems = append(problems, "mistral_api_key is still set to the example placeholder; remove it or set a real key")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid server config: %s", strings.Join(problems, "; "))
	}
	return nil
}

func isPlaceholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	upper := strings.ToUpper(trimmed)
	return trimmed == "" || upper == "CHANGE_ME" || upper == "BOT_TOKEN_HERE" || strings.Contains(upper, "YOUR-TOKEN-HERE")
}
