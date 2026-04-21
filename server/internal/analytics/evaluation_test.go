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
	if !evaluation.Experiments[0].Recovered || evaluation.Experiments[0].RecoveryDelaySeconds != 95 {
		t.Fatalf("unexpected recovery result: %#v", evaluation.Experiments[0])
	}
	if evaluation.Experiments[0].PeakMetricValue != 100 {
		t.Fatalf("unexpected peak metric: %.1f", evaluation.Experiments[0].PeakMetricValue)
	}
}
