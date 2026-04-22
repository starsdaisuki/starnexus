package analytics

import (
	"testing"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func TestBuildGroundTruthEvaluationMeasuresDelays(t *testing.T) {
	nodeID := "jp-lisahost"
	recoveryBody := "Node healthy"
	events := []db.Event{
		{NodeID: &nodeID, Type: "anomaly", Severity: "critical", Title: "CPU outlier detected", CreatedAt: 1120},
		{NodeID: &nodeID, Type: "status_change", Severity: "info", Title: "Node recovered", Body: &recoveryBody, CreatedAt: 1305},
	}
	points := map[string][]db.MetricPoint{
		nodeID: {
			{Timestamp: 1000, CPUPercent: 8},
			{Timestamp: 1080, CPUPercent: 99},
			{Timestamp: 1140, CPUPercent: 100},
			{Timestamp: 1210, CPUPercent: 7},
		},
	}
	labels := []ExperimentLabel{
		{
			ExperimentID:    "lisahost-cpu",
			NodeID:          nodeID,
			InjectionType:   "cpu_stress",
			ExpectedMetric:  "cpu_percent",
			StartedAt:       1000,
			EndedAt:         1210,
			DurationSeconds: 210,
		},
	}

	evaluation := BuildGroundTruthEvaluation(labels, events, points)
	if evaluation.DetectedCount != 1 || evaluation.MissedCount != 0 {
		t.Fatalf("unexpected detection counts: %#v", evaluation)
	}
	if evaluation.Experiments[0].DetectionDelaySeconds != 120 {
		t.Fatalf("unexpected detection delay: %#v", evaluation.Experiments[0])
	}
	if evaluation.Experiments[0].DetectionType != "anomaly" || evaluation.AnomalyDetectionCount != 1 || evaluation.StatusDetectionCount != 0 {
		t.Fatalf("unexpected detection source: %#v", evaluation)
	}
	if !evaluation.Experiments[0].Recovered || evaluation.Experiments[0].RecoveryDelaySeconds != 95 {
		t.Fatalf("unexpected recovery result: %#v", evaluation.Experiments[0])
	}
	if evaluation.Experiments[0].PeakMetricValue != 100 {
		t.Fatalf("unexpected peak metric: %.1f", evaluation.Experiments[0].PeakMetricValue)
	}
	if evaluation.ObservationNodeHours == 0 || evaluation.ExperimentNodeHours == 0 {
		t.Fatalf("expected exposure hours to be calculated: %#v", evaluation)
	}
}

func TestBuildGroundTruthEvaluationCalculatesFalsePositiveRate(t *testing.T) {
	nodeID := "node-a"
	events := []db.Event{
		{NodeID: &nodeID, Type: "anomaly", Severity: "warning", Title: "CPU outlier detected", CreatedAt: 2100},
		{NodeID: &nodeID, Type: "status_change", Severity: "warning", Title: "Node degraded", CreatedAt: 4000},
	}
	points := map[string][]db.MetricPoint{
		nodeID: {
			{Timestamp: 1000, CPUPercent: 5},
			{Timestamp: 4600, CPUPercent: 5},
		},
	}
	labels := []ExperimentLabel{
		{
			ExperimentID:   "cpu-test",
			NodeID:         nodeID,
			InjectionType:  "cpu_stress",
			ExpectedMetric: "cpu_percent",
			StartedAt:      2000,
			EndedAt:        2200,
		},
	}

	evaluation := BuildGroundTruthEvaluation(labels, events, points)
	if evaluation.FalsePositiveEventCount != 1 || evaluation.FalsePositiveStatusCount != 1 || evaluation.FalsePositiveAnomalyCount != 0 {
		t.Fatalf("unexpected false-positive counts: %#v", evaluation)
	}
	if evaluation.ObservationNodeHours != 1 {
		t.Fatalf("expected 1 observation node-hour, got %.4f", evaluation.ObservationNodeHours)
	}
	if evaluation.ExperimentNodeHours <= 0 || evaluation.SteadyStateNodeHours <= 0 {
		t.Fatalf("expected non-zero exposure values: %#v", evaluation)
	}
	if evaluation.FalsePositiveRate <= 0 {
		t.Fatalf("expected false-positive rate, got %#v", evaluation)
	}
}
