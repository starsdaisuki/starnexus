package config

import (
	"os"

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

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
