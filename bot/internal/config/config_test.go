package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsMissingTelegramToken(t *testing.T) {
	path := writeConfig(t, `
telegram_token: "BOT_TOKEN_HERE"
chat_ids:
  - 123
server_url: "http://127.0.0.1:8900"
api_token: "secret-token"
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "telegram_token") {
		t.Fatalf("expected telegram_token validation error, got %v", err)
	}
}

func TestLoadAcceptsValidConfig(t *testing.T) {
	path := writeConfig(t, `
telegram_token: "123456:abcdef"
chat_ids:
  - 123
server_url: "http://127.0.0.1:8900"
api_token: "secret-token"
poll_interval_seconds: 30
heartbeat_interval_seconds: 300
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load valid config: %v", err)
	}
	if cfg.PollIntervalSeconds != 30 || cfg.HeartbeatIntervalSeconds != 300 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := writeConfig(t, `
telegram_token: "123456:abcdef"
chat_ids:
  - 123
server_url: "http://127.0.0.1:8900"
api_token: "secret-token"
surprise: true
`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "surprise") {
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
