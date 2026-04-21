package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWebDirFallsBackToSharedFrontend(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(cwd)

	sharedDir := filepath.Join(tempDir, "..", "web", "public")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("mkdir shared dir: %v", err)
	}

	if got := resolveWebDir("./web"); got != "../web/public" {
		t.Fatalf("expected fallback to ../web/public, got %q", got)
	}
}
