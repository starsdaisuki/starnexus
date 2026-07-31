package db

import (
	"testing"
	"time"
)

func TestUpsertReportPreservesCollectedAtForRawMetrics(t *testing.T) {
	database := openTestDB(t)

	collectedAt := time.Now().Unix() - 600
	req := reportRequest("node-a", collectedAt, 12)
	if _, err := database.UpsertReport(req, true); err != nil {
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
	if _, err := database.UpsertReport(reportRequest("node-a", now, 10), true); err != nil {
		t.Fatalf("upsert current report: %v", err)
	}
	if _, err := database.UpsertReport(reportRequest("node-a", now-600, 90), true); err != nil {
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
	// node-b must be a registered node — links to unknown targets are
	// dropped so decommissioned peers cannot resurrect themselves.
	if _, err := database.UpsertReport(reportRequest("node-b", now, 5), true); err != nil {
		t.Fatalf("register peer node: %v", err)
	}

	current := reportRequest("node-a", now, 10)
	current.Links = []ReportLink{{TargetNodeID: "node-b", LatencyMs: 20, PacketLoss: 0}}
	if _, err := database.UpsertReport(current, true); err != nil {
		t.Fatalf("upsert current report: %v", err)
	}

	replay := reportRequest("node-a", now-600, 90)
	replay.Links = []ReportLink{{TargetNodeID: "node-b", LatencyMs: 999, PacketLoss: 100}}
	if _, err := database.UpsertReport(replay, true); err != nil {
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

func TestUpsertReportDropsLinksToUnknownNodes(t *testing.T) {
	database := openTestDB(t)

	now := time.Now().Unix()
	report := reportRequest("node-a", now, 10)
	report.Links = []ReportLink{{TargetNodeID: "retired-node", LatencyMs: -1, PacketLoss: 100}}
	if _, err := database.UpsertReport(report, true); err != nil {
		t.Fatalf("upsert report: %v", err)
	}

	var count int
	if err := database.conn.QueryRow(
		"SELECT COUNT(*) FROM links WHERE target_node_id = ?", "retired-node",
	).Scan(&count); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected link to unknown node to be dropped, got %d row(s)", count)
	}

	// And a node deleted after the fact must stay deleted even though the
	// agent keeps probing it.
	if _, err := database.UpsertReport(reportRequest("node-b", now, 5), true); err != nil {
		t.Fatalf("register peer node: %v", err)
	}
	linked := reportRequest("node-a", now, 10)
	linked.Links = []ReportLink{{TargetNodeID: "node-b", LatencyMs: 20, PacketLoss: 0}}
	if _, err := database.UpsertReport(linked, true); err != nil {
		t.Fatalf("upsert linked report: %v", err)
	}
	if err := database.DeleteNode("node-b"); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	stale := reportRequest("node-a", now+60, 10)
	stale.Links = []ReportLink{{TargetNodeID: "node-b", LatencyMs: -1, PacketLoss: 100}}
	if _, err := database.UpsertReport(stale, true); err != nil {
		t.Fatalf("upsert stale report: %v", err)
	}
	if err := database.conn.QueryRow(
		"SELECT COUNT(*) FROM links WHERE target_node_id = ?", "node-b",
	).Scan(&count); err != nil {
		t.Fatalf("count links after delete: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleted node's link to stay deleted, got %d row(s)", count)
	}
}

func TestUpsertReportReplayPreservesNodeStatus(t *testing.T) {
	database := openTestDB(t)

	now := time.Now().Unix()
	if _, err := database.UpsertReport(reportRequest("node-a", now, 10), true); err != nil {
		t.Fatalf("upsert live report: %v", err)
	}
	if err := database.SetNodeStatus("node-a", "degraded"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	// A replayed batch from an agent's disk queue must not flip a
	// degraded node back to online behind the incident bookkeeping.
	if _, err := database.UpsertReport(reportRequest("node-a", now-600, 15), false); err != nil {
		t.Fatalf("upsert replay report: %v", err)
	}

	var status string
	if err := database.conn.QueryRow("SELECT status FROM nodes WHERE id = ?", "node-a").Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "degraded" {
		t.Fatalf("expected replay to preserve degraded status, got %q", status)
	}

	// A live report is still allowed to flip it.
	if _, err := database.UpsertReport(reportRequest("node-a", now, 12), true); err != nil {
		t.Fatalf("upsert live report: %v", err)
	}
	if err := database.conn.QueryRow("SELECT status FROM nodes WHERE id = ?", "node-a").Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "online" {
		t.Fatalf("expected live report to set online, got %q", status)
	}
}

func TestGetOnlineSecondsCountsHourlyAggregates(t *testing.T) {
	database := openTestDB(t)

	now := time.Now().Unix()
	if _, err := database.UpsertReport(reportRequest("node-a", now, 10), true); err != nil {
		t.Fatalf("upsert report: %v", err)
	}

	// Simulate downsampled history: 10 days ago the raw rows are gone
	// and only the hourly aggregate remains. Availability over 30 days
	// must still count it, or healthy nodes cap at raw-retention/window.
	tenDaysAgoHour := ((now - 10*86400) / 3600) * 3600
	if _, err := database.conn.Exec(`
		INSERT INTO metrics_hourly (node_id, hour, cpu_avg, cpu_max, cpu_stddev, mem_avg, mem_max, mem_stddev, bw_up_avg, bw_down_avg, load_avg, sample_count)
		VALUES ('node-a', ?, 10, 12, 1, 20, 22, 1, 0, 0, 0.1, 120)
	`, tenDaysAgoHour); err != nil {
		t.Fatalf("insert hourly aggregate: %v", err)
	}

	// +1 because the window is [from, to) and the raw row sits at `now`.
	onlineSec, err := database.GetOnlineSeconds("node-a", now-30*86400, now+1)
	if err != nil {
		t.Fatalf("get online seconds: %v", err)
	}
	// 1 raw row (30 s) + 120 hourly samples (3600 s).
	if onlineSec != 30+120*30 {
		t.Fatalf("expected %d online seconds, got %d", 30+120*30, onlineSec)
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
