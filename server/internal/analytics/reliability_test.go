package analytics

import (
	"testing"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func TestBuildReliabilityAnalyticsRanksWeakNodeFirst(t *testing.T) {
	now := int64(1_700_000_000)
	recent := now - 30
	stale := now - 900
	provider := "test"
	locationSource := "manual"
	nodeID := "weak"

	report := BuildReliabilityAnalytics(24, now, []FleetNodeSample{
		{
			Node: db.Node{
				ID:             "healthy",
				Name:           "Healthy",
				Provider:       &provider,
				LocationSource: &locationSource,
				Status:         "online",
				LastSeen:       &recent,
			},
			Analytics: DetailAnalytics{CoveragePercent: 100, RiskLevel: "stable"},
		},
		{
			Node: db.Node{
				ID:             nodeID,
				Name:           "Weak",
				Provider:       &provider,
				LocationSource: &locationSource,
				Status:         "degraded",
				LastSeen:       &stale,
			},
			Analytics: DetailAnalytics{CoveragePercent: 35, RiskLevel: "critical"},
		},
	}, []db.Event{
		{NodeID: &nodeID, Type: "status_change", Severity: "critical"},
		{NodeID: &nodeID, Type: "status_change", Severity: "warning"},
	}, nil)

	if len(report.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(report.Nodes))
	}
	if report.Nodes[0].NodeID != nodeID {
		t.Fatalf("expected weak node to rank first, got %s", report.Nodes[0].NodeID)
	}
	if report.Nodes[0].DataQuality != "weak" {
		t.Fatalf("expected weak data quality, got %s", report.Nodes[0].DataQuality)
	}
	if report.IncidentCount != 2 || report.CriticalEventCount != 1 || report.WarningEventCount != 1 {
		t.Fatalf("unexpected event counts: %+v", report)
	}
	if report.FleetOperationalScore <= 0 || report.FleetOperationalScore >= 100 {
		t.Fatalf("fleet score should be bounded but not saturated, got %.2f", report.FleetOperationalScore)
	}
}

func TestBuildReliabilityAnalyticsHandlesEmptyInput(t *testing.T) {
	report := BuildReliabilityAnalytics(24, 0, nil, nil, nil)
	if report.Summary == "" {
		t.Fatal("expected summary")
	}
	if len(report.Nodes) != 0 {
		t.Fatalf("expected no nodes, got %d", len(report.Nodes))
	}
}

func TestBuildReliabilityAnalyticsSeparatesAnomalyAndExperimentSignals(t *testing.T) {
	now := int64(1_700_000_000)
	lastSeen := now - 30
	nodeID := "node-a"
	report := BuildReliabilityAnalytics(24, now, []FleetNodeSample{
		{
			Node:      db.Node{ID: nodeID, Name: "Node A", Status: "online", LastSeen: &lastSeen},
			Analytics: DetailAnalytics{CoveragePercent: 100, RiskLevel: "stable"},
		},
	}, []db.Event{
		{NodeID: &nodeID, Type: "anomaly", Severity: "critical", CreatedAt: now - 600},
		{NodeID: &nodeID, Type: "anomaly", Severity: "warning", CreatedAt: now - 100},
	}, []ExperimentLabel{
		{NodeID: nodeID, StartedAt: now - 120, EndedAt: now - 60},
	})

	if report.IncidentCount != 0 {
		t.Fatalf("anomaly events should not be counted as operational incidents, got %d", report.IncidentCount)
	}
	if report.SignalEventCount != 1 {
		t.Fatalf("expected one non-experiment statistical signal, got %d", report.SignalEventCount)
	}
	if report.ExperimentEventCount != 1 {
		t.Fatalf("expected one experiment signal, got %d", report.ExperimentEventCount)
	}
}
