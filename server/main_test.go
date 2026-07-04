package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWebDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(cwd)

	// Nothing configured, nothing on disk → embedded frontend.
	if got := resolveWebDir(""); got != "" {
		t.Fatalf("expected embedded fallback, got %q", got)
	}

	// An explicitly configured dir that does not exist is a config
	// mistake — surface it by falling back to embedded, not by silently
	// picking a different directory.
	if got := resolveWebDir("./web"); got != "" {
		t.Fatalf("expected embedded fallback for missing configured dir, got %q", got)
	}

	// Nothing configured + repo checkout layout → dev fallback to the
	// canonical frontend.
	sharedDir := filepath.Join(tempDir, "..", "web", "public")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("mkdir shared dir: %v", err)
	}
	if got := resolveWebDir(""); got != "../web/public" {
		t.Fatalf("expected fallback to ../web/public, got %q", got)
	}

	// An explicitly configured dir that exists wins.
	configured := filepath.Join(tempDir, "custom-web")
	if err := os.MkdirAll(configured, 0o755); err != nil {
		t.Fatalf("mkdir configured dir: %v", err)
	}
	if got := resolveWebDir(configured); got != configured {
		t.Fatalf("expected configured dir %q, got %q", configured, got)
	}
}
