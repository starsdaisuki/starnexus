package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type ProbeTarget struct {
	NodeID string `yaml:"node_id"`
	Host   string `yaml:"host"`
	Port   int    `yaml:"port"` // TCP port to probe (0 = ICMP ping fallback)
}

type Config struct {
	ServerURL             string         `yaml:"server_url"`
	APIToken              string         `yaml:"api_token"`
	NodeID                string         `yaml:"node_id"`
	NodeName              string         `yaml:"node_name"`
	Provider              string         `yaml:"provider"`
	PublicIP              string         `yaml:"public_ip"`
	Latitude              float64        `yaml:"latitude"`
	Longitude             float64        `yaml:"longitude"`
	ReportIntervalSeconds int            `yaml:"report_interval_seconds"`
	ProbeTargets          []ProbeTarget  `yaml:"probe_targets"`
	GeoIPDBPath           string         `yaml:"geoip_db_path"`
	ConnIntervalSeconds   int            `yaml:"connection_report_interval_seconds"`
	PortLabels            map[int]string `yaml:"port_labels"`
	ProxyProcesses        []string       `yaml:"proxy_processes"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		ReportIntervalSeconds: 30,
		ConnIntervalSeconds:   5,
		GeoIPDBPath:           "./GeoLite2-City.mmdb",
		ProxyProcesses:        []string{"xray", "sing-box", "x-ui", "3x-ui"},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
