package analytics

import (
	"fmt"
	"math"
	"slices"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// ----- Detector 7: CUSUM (Page's cumulative sum changepoint) -----

// CUSUMDetector is the classical Page (1954) CUSUM changepoint detector,
// one-sided upper, applied per-metric with an OR combiner.
//
// The unknown-parameter variant is used: instead of assuming a fixed
// target mean μ₀ and dispersion σ₀, we standardise each incoming sample
// against a rolling robust baseline (median, 1.4826·MAD) computed on the
// last BaselineWindow samples. This is the natural adaptation for VPS
// load data where the "normal" centre drifts over hours.
//
// Recurrence (per metric):
//
//	S_t = max(0, S_{t-1} + (x_t - μ_hat)/σ_hat - K)
//
// where K = 0.5 is the reference value (half of the standardised shift
// we want to detect fast). The detector fires when S_t crosses the
// decision interval H = 5. A soft reset zeroes S_t after a firing so the
// detector can re-arm on subsequent shifts.
//
// Classical ARL (average run length) theory for K=0.5, H=5 on N(0,1)
// input: ARL₀ ≈ 930 under the null (false-positive rate ≈ 1/930 per
// sample ≈ 0.1% at 30 s cadence ≈ one false firing per 7.7 h per metric,
// before the OR combiner). Post-shift ARL₁ ≈ 6.4 samples for a 1σ shift.
type CUSUMDetector struct {
	BaselineWindow int
	K              float64
	H              float64
	MinCurrents    map[string]float64
	MinHold        int
}

func NewCUSUMDetector() *CUSUMDetector {
	return &CUSUMDetector{
		BaselineWindow: 100,
		K:              0.5,
		H:              5.0,
		MinCurrents: map[string]float64{
			"cpu_percent":    30,
			"memory_percent": 40,
			"bandwidth_down": 512,
			"connections":    50,
		},
		MinHold: 2,
	}
}

func (d *CUSUMDetector) Name() string { return "cusum" }
func (d *CUSUMDetector) Description() string {
	return "Page's CUSUM (K=0.5, H=5) on robust-standardised residuals, per metric with OR combiner. Classical changepoint baseline with ARL₀≈930 samples."
}

func (d *CUSUMDetector) Process(nodeID string, points []db.MetricPoint) []SyntheticEvent {
	if len(points) == 0 {
		return nil
	}
	selectors := defaultMetricSelectors()
	series := make([][]float64, len(selectors))
	for i, sel := range selectors {
		series[i] = make([]float64, len(points))
		for j, point := range points {
			series[i][j] = sel.Extract(point)
		}
	}

	firing := false
	consecutive := 0
	var events []SyntheticEvent
	warmup := d.BaselineWindow

	cusum := make([]float64, len(selectors))
	triggeredBy := make([]bool, len(selectors))

	for idx := range points {
		if idx < warmup+d.MinHold {
			continue
		}
		anyTrigger := false
		primaryMetric := ""
		primaryValue := 0.0

		for k, sel := range selectors {
			floor := d.MinCurrents[sel.Key]
			current := series[k][idx]
			if current < floor {
				cusum[k] = 0
				triggeredBy[k] = false
				continue
			}
			start := idx - d.BaselineWindow
			if start < 0 {
				start = 0
			}
			window := slices.Clone(series[k][start:idx])
			slices.Sort(window)
			med := percentile(window, 50)
			scale := 1.4826 * mad(window, med)
			if scale < 1e-6 {
				cusum[k] = 0
				triggeredBy[k] = false
				continue
			}
			z := (current - med) / scale
			cusum[k] = math.Max(0, cusum[k]+z-d.K)
			if cusum[k] >= d.H {
				anyTrigger = true
				triggeredBy[k] = true
				if primaryMetric == "" || z > primaryValue {
					primaryMetric = sel.Label
					primaryValue = z
				}
			} else {
				triggeredBy[k] = false
			}
		}

		if anyTrigger {
			consecutive++
		} else {
			consecutive = 0
		}

		ts := points[idx].Timestamp
		if !firing && consecutive >= d.MinHold {
			label := primaryMetric
			if label == "" {
				label = "composite"
			}
			fired := 0
			for _, t := range triggeredBy {
				if t {
					fired++
				}
			}
			events = append(events, SyntheticEvent{
				NodeID:    nodeID,
				Type:      "anomaly",
				Severity:  "warning",
				Timestamp: ts,
				Title:     fmt.Sprintf("%s outlier detected", label),
				Body:      fmt.Sprintf("CUSUM crossed H=%.1f on %d metric(s); K=%.2f", d.H, fired, d.K),
				Metric:    "multivariate",
				Value:     primaryValue,
			})
			firing = true
			// Soft reset so the detector can re-arm on subsequent shifts.
			for k := range cusum {
				cusum[k] = 0
			}
		} else if firing && consecutive == 0 {
			events = append(events, SyntheticEvent{
				NodeID:    nodeID,
				Type:      "status_change",
				Severity:  "info",
				Timestamp: ts,
				Title:     "Node recovered",
				Body:      "CUSUM returned below decision interval on all metrics",
				Metric:    "multivariate",
				Value:     0,
			})
			firing = false
		}
	}
	return events
}
