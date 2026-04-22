package analytics

import (
	"math"
	"testing"
)

func TestExactBinomialTwoSidedSymmetric(t *testing.T) {
	// All concordant → p = 1 by fallback in pairwiseTest, but the raw
	// binomial function should also report p ≤ 1 for any inputs.
	for _, c := range []struct {
		k, n   int
		wantLo float64
		wantHi float64
	}{
		{0, 10, 0.0, 0.0025},   // 0/10 success, symmetric → very small p
		{5, 10, 0.95, 1.0001},  // perfect balance → p ≈ 1
		{9, 10, 0.0, 0.03},     // 1 vs 9 → still small p
		{10, 10, 0.0, 0.002},   // all one side
	} {
		got := exactBinomialTwoSided(c.k, c.n)
		if got < c.wantLo || got > c.wantHi {
			t.Errorf("exactBinomialTwoSided(%d,%d) = %.4f, want in [%.4f, %.4f]", c.k, c.n, got, c.wantLo, c.wantHi)
		}
	}
}

func TestRandomSignPermutationPValueTrivial(t *testing.T) {
	// Deltas all zero → mean is zero, any permutation is ≥ 0, so p = 1.
	zeros := []float64{0, 0, 0, 0}
	if got := randomSignPermutationPValue(zeros, 1000, 42); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("expected p=1 for all-zero deltas, got %f", got)
	}

	// Deltas all strongly positive → observed mean is large; very few
	// sign flips will match it → small p.
	positives := []float64{10, 12, 9, 11, 13, 14, 10, 11}
	p := randomSignPermutationPValue(positives, 5000, 7)
	if p > 0.02 {
		t.Fatalf("expected p≤0.02 for strongly positive deltas, got %f", p)
	}
}

func TestBuildPairwiseTestsCrossCatalogue(t *testing.T) {
	mk := func(name string, exps []ExperimentEvaluation) DetectorBenchmark {
		return DetectorBenchmark{
			Name:        name,
			GroundTruth: GroundTruthEvaluation{Experiments: exps, ExperimentCount: len(exps)},
		}
	}
	exps := []ExperimentEvaluation{
		{ExperimentID: "e1", Detected: true, DetectionDelaySeconds: 20},
		{ExperimentID: "e2", Detected: true, DetectionDelaySeconds: 30},
		{ExperimentID: "e3", Detected: false},
		{ExperimentID: "e4", Detected: true, DetectionDelaySeconds: 25},
	}
	expsB := []ExperimentEvaluation{
		{ExperimentID: "e1", Detected: true, DetectionDelaySeconds: 60},
		{ExperimentID: "e2", Detected: false},
		{ExperimentID: "e3", Detected: true, DetectionDelaySeconds: 45},
		{ExperimentID: "e4", Detected: true, DetectionDelaySeconds: 55},
	}
	a := mk("fast", exps)
	b := mk("slow", expsB)
	tests := BuildPairwiseTests([]DetectorBenchmark{a, b})
	if len(tests) != 1 {
		t.Fatalf("expected one pairwise test, got %d", len(tests))
	}
	got := tests[0]
	if got.DetectionAOnly != 1 || got.DetectionBOnly != 1 {
		t.Fatalf("expected 1/1 discordant, got %+v", got)
	}
	if got.DelayPairedCount != 2 {
		t.Fatalf("expected 2 delay pairs (e1, e4), got %d", got.DelayPairedCount)
	}
	if got.DelayMeanDeltaSec >= 0 {
		t.Fatalf("fast should have smaller delay than slow, mean delta %f", got.DelayMeanDeltaSec)
	}
}
