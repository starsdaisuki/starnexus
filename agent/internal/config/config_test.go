package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsInvalidServerURL(t *testing.T) {
	path := writeConfig(t, `
server_url: "SERVER_IP:8900"
api_token: "secret-token"
node_id: "node-a"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "server_url") {
		t.Fatalf("expected server_url validation error, got %v", err)
	}
}

func TestLoadDefaultsNameAndProvider(t *testing.T) {
	path := writeConfig(t, `
server_url: "http://127.0.0.1:8900"
api_token: "secret-token"
node_id: "node-a"
latitude: 0
longitude: 0
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load valid config: %v", err)
	}
	if cfg.NodeName != "node-a" || cfg.Provider != "Unknown" {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, `
server_url: "http://127.0.0.1:8900"
api_token: "secret-token"
node_id: "node-a"
extra: true
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
