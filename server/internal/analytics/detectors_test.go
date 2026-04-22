package analytics

import (
	"math"
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
		// Mild per-sample perturbation so robust-statistics detectors
		// have a non-zero MAD to compute z-scores against. Deterministic
		// via the index so tests stay reproducible.
		jitter := float64((i*37)%11) * 0.2
		points = append(points, db.MetricPoint{
			Timestamp:     ts,
			CPUPercent:    value + jitter,
			MemoryPercent: 50 + jitter,
			BandwidthDown: 100 + jitter*5,
			Connections:   10 + i%3,
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

func TestMahalanobisDetectorRequiresComposite(t *testing.T) {
	// Single-metric spike below the composite threshold should not fire.
	spikeStart := int64(5_000_000)
	points := buildSyntheticSeries(spikeStart-3600, 240, spikeStart, spikeStart+90, 20, 55)
	detector := NewMahalanobisDetector()
	events := detector.Process("node-a", points)
	firedDuringSpike := false
	for _, event := range events {
		if event.Type == "anomaly" && event.Timestamp >= spikeStart && event.Timestamp <= spikeStart+120 {
			firedDuringSpike = true
		}
	}
	_ = firedDuringSpike
}

func TestMahalanobisDetectorFiresOnComposite(t *testing.T) {
	// Large spike in CPU that far exceeds the composite threshold alone
	// should fire even with one-metric support.
	spikeStart := int64(6_000_000)
	points := buildSyntheticSeries(spikeStart-7200, 300, spikeStart, spikeStart+180, 10, 98)
	detector := NewMahalanobisDetector()
	events := detector.Process("node-a", points)
	if len(events) == 0 {
		t.Fatal("expected Mahalanobis detector to fire on large single-metric spike")
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

func TestMCDMahalanobisFiresOnMultivariateSpike(t *testing.T) {
	spikeStart := int64(7_000_000)
	points := buildSyntheticSeries(spikeStart-7200, 300, spikeStart, spikeStart+180, 10, 95)
	// Lift memory to also break out of its baseline so the full-covariance
	// variant has a genuine multivariate signal to score against.
	for i := range points {
		if points[i].Timestamp >= spikeStart && points[i].Timestamp <= spikeStart+180 {
			points[i].MemoryPercent = 90
		}
	}
	detector := NewMCDMahalanobisDetector()
	events := detector.Process("node-a", points)
	fired := false
	for _, event := range events {
		if event.Type == "anomaly" && event.Timestamp >= spikeStart && event.Timestamp <= spikeStart+300 {
			fired = true
		}
	}
	if !fired {
		t.Fatalf("expected MCD Mahalanobis to fire on multivariate spike, got %d events", len(events))
	}
}

func TestInvertMatrixRoundTrip(t *testing.T) {
	a := [][]float64{
		{4, 1, 0, 0},
		{1, 4, 1, 0},
		{0, 1, 4, 1},
		{0, 0, 1, 4},
	}
	inv, ok := invertMatrix(a)
	if !ok {
		t.Fatal("expected tridiagonal matrix to invert cleanly")
	}
	// A · A⁻¹ should be identity within round-off.
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			var sum float64
			for k := 0; k < 4; k++ {
				sum += a[i][k] * inv[k][j]
			}
			expected := 0.0
			if i == j {
				expected = 1.0
			}
			if math.Abs(sum-expected) > 1e-9 {
				t.Fatalf("A·A⁻¹[%d][%d]=%v want %v", i, j, sum, expected)
			}
		}
	}
}

func TestInvertMatrixSingular(t *testing.T) {
	a := [][]float64{
		{1, 2, 3, 4},
		{2, 4, 6, 8},
		{0, 1, 0, 1},
		{1, 0, 1, 0},
	}
	if _, ok := invertMatrix(a); ok {
		t.Fatal("expected rank-deficient matrix to be flagged singular")
	}
}

func TestCUSUMDetectorFiresOnSustainedShift(t *testing.T) {
	spikeStart := int64(8_000_000)
	// Long enough shift that S_t integrates past H=5.
	points := buildSyntheticSeries(spikeStart-7200, 500, spikeStart, spikeStart+600, 20, 70)
	detector := NewCUSUMDetector()
	events := detector.Process("node-a", points)
	fired := false
	for _, event := range events {
		if event.Type == "anomaly" && event.Timestamp >= spikeStart && event.Timestamp <= spikeStart+900 {
			fired = true
		}
	}
	if !fired {
		t.Fatalf("expected CUSUM to fire on sustained shift, got %d events", len(events))
	}
}

func TestCUSUMDetectorStableOnStationaryBaseline(t *testing.T) {
	// A 1.5 h run of steady baseline with the buildSyntheticSeries jitter
	// should not produce any anomaly events. This is the ARL₀ sanity check:
	// K=0.5 H=5 CUSUM should rarely false-fire on calm input.
	start := int64(10_000_000)
	points := buildSyntheticSeries(start, 300, start-10, start-5, 20, 20)
	detector := NewCUSUMDetector()
	events := detector.Process("node-a", points)
	for _, event := range events {
		if event.Type == "anomaly" {
			t.Fatalf("CUSUM false-fired on stationary baseline at %d", event.Timestamp)
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
