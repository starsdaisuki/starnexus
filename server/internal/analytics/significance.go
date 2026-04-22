package analytics

import (
	"math"
	"sort"
)

// PairwiseTest reports how two detectors compare on the same set of
// labelled experiments. Comparisons are paired: the same experiment is
// evaluated by both detectors, so the right inference is a paired test,
// not an unpaired two-sample test.
type PairwiseTest struct {
	DetectorA           string  `json:"detector_a"`
	DetectorB           string  `json:"detector_b"`
	ExperimentCount     int     `json:"experiment_count"`
	DetectionAOnly      int     `json:"detection_a_only"`
	DetectionBOnly      int     `json:"detection_b_only"`
	DetectionDiscordant int     `json:"detection_discordant"`
	DetectionPValue     float64 `json:"detection_p_value"`
	DelayPairedCount    int     `json:"delay_paired_count"`
	DelayMeanDeltaSec   float64 `json:"delay_mean_delta_seconds"`
	DelayPValue         float64 `json:"delay_p_value"`
}

// BuildPairwiseTests runs paired significance tests between every
// ordered pair of detectors. Two tests per pair:
//
//  1. Detection rate — exact two-sided binomial (sign) test on the
//     discordant set. H0: when A and B disagree on an experiment, each
//     direction is equally likely.
//  2. Detection delay — paired random-sign permutation test on the
//     experiments both detectors caught. H0: the per-experiment delay
//     deltas are symmetric around zero.
//
// Permutation iterations are fixed at 5000 with a deterministic LCG so
// the p-values are reproducible when rerun on the same inputs.
func BuildPairwiseTests(benchmarks []DetectorBenchmark) []PairwiseTest {
	tests := []PairwiseTest{}
	for i := 0; i < len(benchmarks); i++ {
		for j := i + 1; j < len(benchmarks); j++ {
			tests = append(tests, pairwiseTest(benchmarks[i], benchmarks[j]))
		}
	}
	return tests
}

func pairwiseTest(a, b DetectorBenchmark) PairwiseTest {
	aByID := map[string]ExperimentEvaluation{}
	bByID := map[string]ExperimentEvaluation{}
	for _, exp := range a.GroundTruth.Experiments {
		aByID[exp.ExperimentID] = exp
	}
	for _, exp := range b.GroundTruth.Experiments {
		bByID[exp.ExperimentID] = exp
	}
	ids := make([]string, 0, len(aByID))
	for id := range aByID {
		if _, ok := bByID[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	test := PairwiseTest{
		DetectorA:       a.Name,
		DetectorB:       b.Name,
		ExperimentCount: len(ids),
	}

	deltas := make([]float64, 0, len(ids))
	for _, id := range ids {
		ea := aByID[id]
		eb := bByID[id]
		switch {
		case ea.Detected && !eb.Detected:
			test.DetectionAOnly++
		case !ea.Detected && eb.Detected:
			test.DetectionBOnly++
		}
		if ea.Detected && eb.Detected {
			deltas = append(deltas, float64(ea.DetectionDelaySeconds-eb.DetectionDelaySeconds))
		}
	}
	test.DetectionDiscordant = test.DetectionAOnly + test.DetectionBOnly
	if test.DetectionDiscordant == 0 {
		test.DetectionPValue = 1.0
	} else {
		// Two-sided exact binomial test: P(|X - n/2| >= |k - n/2|)
		// where X ~ Binomial(n, 0.5) and k = DetectionAOnly.
		test.DetectionPValue = exactBinomialTwoSided(test.DetectionAOnly, test.DetectionDiscordant)
	}

	test.DelayPairedCount = len(deltas)
	if len(deltas) == 0 {
		test.DelayPValue = 1.0
		return test
	}

	var sum float64
	for _, d := range deltas {
		sum += d
	}
	test.DelayMeanDeltaSec = sum / float64(len(deltas))

	test.DelayPValue = randomSignPermutationPValue(deltas, 5000, 17)
	return test
}

// exactBinomialTwoSided computes the exact two-sided p-value of
// observing k "successes" out of n Bernoulli(0.5) trials. For the
// symmetric null distribution this reduces to 2·P(X ≤ min(k, n-k))
// capped at 1.
func exactBinomialTwoSided(k, n int) float64 {
	if n <= 0 {
		return 1.0
	}
	if k > n-k {
		k = n - k
	}
	logCoeff := logFactorial(n) - float64(n)*math.Log(2)
	var tail float64
	for i := 0; i <= k; i++ {
		tail += math.Exp(logCoeff + logFactorial(n) - logFactorial(i) - logFactorial(n-i) - logFactorial(n))
	}
	// Simpler and numerically stable: sum Binomial(n, i) * 0.5^n directly.
	tail = 0
	for i := 0; i <= k; i++ {
		tail += math.Exp(logBinomial(n, i) - float64(n)*math.Log(2))
	}
	p := 2 * tail
	if p > 1 {
		p = 1
	}
	return p
}

func logFactorial(n int) float64 {
	if n < 2 {
		return 0
	}
	var total float64
	for i := 2; i <= n; i++ {
		total += math.Log(float64(i))
	}
	return total
}

func logBinomial(n, k int) float64 {
	if k < 0 || k > n {
		return math.Inf(-1)
	}
	return logFactorial(n) - logFactorial(k) - logFactorial(n-k)
}

// randomSignPermutationPValue tests the null that the delta
// distribution is symmetric around zero by flipping the sign of each
// observed delta independently at random and counting how often the
// permuted mean reaches the observed mean in magnitude.
func randomSignPermutationPValue(deltas []float64, iterations int, seed uint64) float64 {
	if len(deltas) == 0 || iterations <= 0 {
		return 1.0
	}
	var observed float64
	for _, d := range deltas {
		observed += d
	}
	observedMean := observed / float64(len(deltas))
	target := math.Abs(observedMean)

	state := seed | 1
	hits := 0
	for iter := 0; iter < iterations; iter++ {
		var permuted float64
		for _, d := range deltas {
			state = nextLCG(state)
			if (state>>63)&1 == 1 {
				permuted -= d
			} else {
				permuted += d
			}
		}
		if math.Abs(permuted/float64(len(deltas))) >= target-1e-9 {
			hits++
		}
	}
	// +1 smoothing avoids p=0 reports when the observed stat is
	// extreme; standard practice for permutation tests.
	return float64(hits+1) / float64(iterations+1)
}
