package config

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"strings"

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

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, err
	}

	if cfg.NodeName == "" {
		cfg.NodeName = cfg.NodeID
	}
	if cfg.Provider == "" {
		cfg.Provider = "Unknown"
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	var problems []string
	if !validHTTPURL(c.ServerURL) {
		problems = append(problems, "server_url must be an absolute http(s) URL")
	}
	if isPlaceholder(c.APIToken) {
		problems = append(problems, "api_token is required and must not be a placeholder")
	}
	if strings.TrimSpace(c.NodeID) == "" {
		problems = append(problems, "node_id is required")
	}
	if strings.TrimSpace(c.NodeName) == "" {
		problems = append(problems, "node_name is required")
	}
	if c.Latitude < -90 || c.Latitude > 90 {
		problems = append(problems, "latitude must be between -90 and 90")
	}
	if c.Longitude < -180 || c.Longitude > 180 {
		problems = append(problems, "longitude must be between -180 and 180")
	}
	if c.ReportIntervalSeconds < 5 || c.ReportIntervalSeconds > 3600 {
		problems = append(problems, "report_interval_seconds must be between 5 and 3600")
	}
	if c.ConnIntervalSeconds < 1 || c.ConnIntervalSeconds > 300 {
		problems = append(problems, "connection_report_interval_seconds must be between 1 and 300")
	}
	for i, target := range c.ProbeTargets {
		if strings.TrimSpace(target.NodeID) == "" {
			problems = append(problems, fmt.Sprintf("probe_targets[%d].node_id is required", i))
		}
		if strings.TrimSpace(target.Host) == "" {
			problems = append(problems, fmt.Sprintf("probe_targets[%d].host is required", i))
		}
		if target.Port < 0 || target.Port > 65535 {
			problems = append(problems, fmt.Sprintf("probe_targets[%d].port must be between 0 and 65535", i))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid agent config: %s", strings.Join(problems, "; "))
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
	return trimmed == "" || upper == "CHANGE_ME" || strings.Contains(upper, "YOUR-TOKEN-HERE")
}
