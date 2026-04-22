package db

import (
	"testing"
	"time"
)

func TestUpsertReportPreservesCollectedAtForRawMetrics(t *testing.T) {
	database := openTestDB(t)

	collectedAt := time.Now().Unix() - 600
	req := reportRequest("node-a", collectedAt, 12)
	if _, err := database.UpsertReport(req); err != nil {
		t.Fatalf("upsert report: %v", err)
	}

	var createdAt int64
	if err := database.conn.QueryRow("SELECT created_at FROM metrics_raw WHERE node_id = ?", "node-a").Scan(&createdAt); err != nil {
		t.Fatalf("read metrics_raw: %v", err)
	}
	if createdAt != collectedAt {
		t.Fatalf("expected collected_at %d, got %d", collectedAt, createdAt)
	}
}

func TestUpsertReportDoesNotLetReplayOverwriteLatestMetrics(t *testing.T) {
	database := openTestDB(t)

	now := time.Now().Unix()
	if _, err := database.UpsertReport(reportRequest("node-a", now, 10)); err != nil {
		t.Fatalf("upsert current report: %v", err)
	}
	if _, err := database.UpsertReport(reportRequest("node-a", now-600, 90)); err != nil {
		t.Fatalf("upsert replay report: %v", err)
	}

	var cpu float64
	var updatedAt int64
	if err := database.conn.QueryRow("SELECT cpu_percent, updated_at FROM node_metrics WHERE node_id = ?", "node-a").Scan(&cpu, &updatedAt); err != nil {
		t.Fatalf("read node metrics: %v", err)
	}
	if cpu != 10 || updatedAt != now {
		t.Fatalf("expected latest metrics to remain cpu=10 updated_at=%d, got cpu=%v updated_at=%d", now, cpu, updatedAt)
	}
}

func TestUpsertReportDoesNotLetReplayOverwriteLatestLinks(t *testing.T) {
	database := openTestDB(t)

	now := time.Now().Unix()
	current := reportRequest("node-a", now, 10)
	current.Links = []ReportLink{{TargetNodeID: "node-b", LatencyMs: 20, PacketLoss: 0}}
	if _, err := database.UpsertReport(current); err != nil {
		t.Fatalf("upsert current report: %v", err)
	}

	replay := reportRequest("node-a", now-600, 90)
	replay.Links = []ReportLink{{TargetNodeID: "node-b", LatencyMs: 999, PacketLoss: 100}}
	if _, err := database.UpsertReport(replay); err != nil {
		t.Fatalf("upsert replay report: %v", err)
	}

	var latency float64
	var packetLoss float64
	var status string
	var updatedAt int64
	if err := database.conn.QueryRow(`
		SELECT latency_ms, packet_loss, status, updated_at
		FROM links
		WHERE source_node_id = ? AND target_node_id = ?
	`, "node-a", "node-b").Scan(&latency, &packetLoss, &status, &updatedAt); err != nil {
		t.Fatalf("read link: %v", err)
	}
	if latency != 20 || packetLoss != 0 || status != "good" || updatedAt != now {
		t.Fatalf("expected current link to remain, got latency=%v loss=%v status=%s updated_at=%d", latency, packetLoss, status, updatedAt)
	}
}

func reportRequest(nodeID string, collectedAt int64, cpu float64) *ReportRequest {
	req := &ReportRequest{
		CollectedAt: collectedAt,
		NodeID:      nodeID,
		Name:        "Node A",
		Provider:    "Provider",
		Latitude:    1,
		Longitude:   2,
	}
	req.Metrics.CPUPercent = cpu
	req.Metrics.MemoryPercent = 20
	return req
}
