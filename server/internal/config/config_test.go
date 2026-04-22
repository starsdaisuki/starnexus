package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsPlaceholderToken(t *testing.T) {
	path := writeConfig(t, `
port: 8900
db_path: "./starnexus.db"
api_token: "CHANGE_ME"
offline_threshold_seconds: 90
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "api_token") {
		t.Fatalf("expected api_token validation error, got %v", err)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, `
port: 8900
db_path: "./starnexus.db"
api_token: "secret-token"
offline_threshold_seconds: 90
unknown_field: true
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown_field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestLoadAcceptsMinimalValidConfig(t *testing.T) {
	path := writeConfig(t, `
port: 8900
db_path: "./starnexus.db"
api_token: "secret-token"
offline_threshold_seconds: 90
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load valid config: %v", err)
	}
	if cfg.Port != 8900 || cfg.DBPath == "" {
		t.Fatalf("unexpected config: %+v", cfg)
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
