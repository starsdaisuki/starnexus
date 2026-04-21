package db

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := Open(filepath.Join(t.TempDir(), "starnexus.db"), filepath.Join("..", "..", "schema.sql"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func TestUpsertIncidentReusesActiveFingerprint(t *testing.T) {
	database := openTestDB(t)

	first, err := database.UpsertIncident("node-a", "metric_anomaly", "warning", "CPU outlier detected", "first", "", "")
	if err != nil {
		t.Fatalf("upsert first incident: %v", err)
	}
	if !first.Created || !first.ShouldNotify {
		t.Fatalf("expected new incident to notify, got %+v", first)
	}

	second, err := database.UpsertIncident("node-a", "metric_anomaly", "critical", "CPU outlier detected", "second", "", "")
	if err != nil {
		t.Fatalf("upsert second incident: %v", err)
	}
	if second.Created || second.ShouldNotify {
		t.Fatalf("expected existing incident update without notification, got %+v", second)
	}
	if second.Incident.ID != first.Incident.ID {
		t.Fatalf("expected same incident id, got %d and %d", first.Incident.ID, second.Incident.ID)
	}
	if second.Incident.Severity != "critical" {
		t.Fatalf("expected severity escalation to critical, got %s", second.Incident.Severity)
	}
	if second.Incident.EventCount != 2 {
		t.Fatalf("expected event_count 2, got %d", second.Incident.EventCount)
	}
}

func TestIncidentLifecycleAckSuppressRecover(t *testing.T) {
	database := openTestDB(t)

	change, err := database.UpsertIncident("node-a", "node_offline", "critical", "Node offline", "stale", "", "")
	if err != nil {
		t.Fatalf("upsert incident: %v", err)
	}

	ack, err := database.AcknowledgeIncident(change.Incident.ID, "test")
	if err != nil {
		t.Fatalf("ack incident: %v", err)
	}
	if ack.Status != IncidentStatusAcknowledged || ack.AcknowledgedBy == nil || *ack.AcknowledgedBy != "test" {
		t.Fatalf("unexpected ack state: %+v", ack)
	}

	suppressedUntil := time.Now().Add(time.Hour).Unix()
	suppressed, err := database.SuppressIncident(change.Incident.ID, suppressedUntil, "test")
	if err != nil {
		t.Fatalf("suppress incident: %v", err)
	}
	if suppressed.Status != IncidentStatusSuppressed || suppressed.SuppressUntil == nil || *suppressed.SuppressUntil != suppressedUntil {
		t.Fatalf("unexpected suppress state: %+v", suppressed)
	}

	recovered, err := database.RecoverIncident(change.Incident.ID)
	if err != nil {
		t.Fatalf("recover incident: %v", err)
	}
	if recovered.Status != IncidentStatusRecovered || recovered.RecoveredAt == nil {
		t.Fatalf("unexpected recovered state: %+v", recovered)
	}

	active, err := database.GetActiveIncidents(10)
	if err != nil {
		t.Fatalf("get active incidents: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("expected no active incidents after recovery, got %+v", active)
	}
}
