package locations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func TestLoadAndApplyReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node-locations.yaml")
	content := []byte("nodes:\n  - id: tokyo-sonet\n    latitude: 35.701\n    longitude: 139.772\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write override file: %v", err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	req := &db.ReportRequest{
		NodeID:         "tokyo-sonet",
		Latitude:       35.0,
		Longitude:      139.0,
		LocationSource: "geoip",
	}
	if !store.ApplyReport(req) {
		t.Fatalf("expected override to apply")
	}
	if req.Latitude != 35.701 || req.Longitude != 139.772 {
		t.Fatalf("unexpected coordinates after apply: %#v", req)
	}
	if req.LocationSource != "manual_override" {
		t.Fatalf("unexpected location source %q", req.LocationSource)
	}
}
