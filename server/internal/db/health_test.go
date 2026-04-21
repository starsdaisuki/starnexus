package db

import "testing"

func TestHealthCheckReportsSQLiteState(t *testing.T) {
	database := openTestDB(t)

	health, err := database.HealthCheck()
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	if !health.OK || health.QuickCheck != "ok" {
		t.Fatalf("expected healthy database, got %+v", health)
	}
	if health.LatestMigration <= 0 {
		t.Fatalf("expected latest migration to be recorded, got %+v", health)
	}
}
