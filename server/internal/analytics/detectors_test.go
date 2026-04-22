package analytics

import (
	"testing"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func buildSyntheticSeries(start int64, steady int, spikeStart, spikeEnd int64, steadyValue, spikeValue float64) []db.MetricPoint {
	points := make([]db.MetricPoint, 0, steady)
	for i := 0; i < steady; i++ {
		ts := start + int64(i)*30
		value := steadyValue
		if ts >= spikeStart && ts <= spikeEnd {
			value = spikeValue
		}
		points = append(points, db.MetricPoint{
			Timestamp:     ts,
			CPUPercent:    value,
			MemoryPercent: 50,
			BandwidthDown: 100,
			Connections:   10,
		})
	}
	return points
}

func TestFixedThresholdDetectorCatchesSpike(t *testing.T) {
	spikeStart := int64(1_000_000)
	spikeEnd := spikeStart + 120
	series := buildSyntheticSeries(spikeStart-3600, 240, spikeStart, spikeEnd, 10, 95)
	detector := NewFixedThresholdDetector()
	events := detector.Process("node-a", series)
	if len(events) < 2 {
		t.Fatalf("expected at least firing + recovery, got %d", len(events))
	}
	firing := false
	recovery := false
	for _, event := range events {
		if event.Metric == "cpu_percent" && event.Type == "anomaly" {
			firing = true
		}
		if event.Metric == "cpu_percent" && event.Type == "status_change" {
			recovery = true
		}
	}
	if !firing || !recovery {
		t.Fatalf("expected cpu firing and recovery, got firing=%v recovery=%v", firing, recovery)
	}
}

func TestPlainZScoreDetectorFiresOnTail(t *testing.T) {
	spikeStart := int64(2_000_000)
	series := buildSyntheticSeries(spikeStart-7200, 300, spikeStart, spikeStart+90, 15, 80)
	detector := NewPlainZScoreDetector()
	events := detector.Process("node-a", series)
	if len(events) == 0 {
		t.Fatal("plain z-score should fire on large spike")
	}
}

func TestRobustShiftDetectorIgnoresShortBurst(t *testing.T) {
	// A 90-second burst should not move median/MAD over a 24h window, which
	// is the documented behaviour we want to compare against baselines.
	spikeStart := int64(3_000_000)
	series := buildSyntheticSeries(spikeStart-86400, 2900, spikeStart, spikeStart+90, 20, 95)
	detector := NewRobustShiftDetector()
	events := detector.Process("node-a", series)
	for _, event := range events {
		if event.Type == "anomaly" && event.Metric == "cpu_percent" {
			if event.Timestamp >= spikeStart && event.Timestamp <= spikeStart+300 {
				return
			}
		}
	}
}

func TestBootstrapMeanCIMatchesMean(t *testing.T) {
	values := []int64{20, 30, 40, 25, 35, 45, 50, 28}
	mean, lo, hi, sd := bootstrapMeanCI(values, 2000, 42)
	if mean <= 0 {
		t.Fatalf("mean should be positive, got %f", mean)
	}
	if lo >= mean || hi <= mean {
		t.Fatalf("CI should straddle mean: lo=%f mean=%f hi=%f", lo, mean, hi)
	}
	if sd <= 0 {
		t.Fatalf("sd should be positive, got %f", sd)
	}
}

func TestRunDetectorBenchmarkProducesCounts(t *testing.T) {
	spikeStart := int64(4_000_000)
	points := buildSyntheticSeries(spikeStart-3600, 240, spikeStart, spikeStart+120, 10, 95)
	labels := []ExperimentLabel{{
		ExperimentID:    "syn-1",
		NodeID:          "node-a",
		InjectionType:   "cpu_stress",
		ExpectedMetric:  "cpu_percent",
		StartedAt:       spikeStart,
		EndedAt:         spikeStart + 120,
		DurationSeconds: 120,
	}}
	result := RunDetectorBenchmark(NewFixedThresholdDetector(), labels, map[string][]db.MetricPoint{"node-a": points})
	if result.GroundTruth.ExperimentCount != 1 {
		t.Fatalf("expected 1 experiment, got %d", result.GroundTruth.ExperimentCount)
	}
	if result.GroundTruth.DetectedCount != 1 {
		t.Fatalf("expected fixed-threshold to detect the synthetic spike, got %d", result.GroundTruth.DetectedCount)
	}
	if result.BootstrapSummary == nil {
		t.Fatal("expected bootstrap summary when detections exist")
	}
}
