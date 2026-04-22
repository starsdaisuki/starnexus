package analytics

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// SyntheticEvent is a detector-generated event used for offline benchmark
// evaluation. It converts to db.Event so the existing ground-truth evaluator
// can score each detector apples-to-apples.
type SyntheticEvent struct {
	NodeID    string
	Type      string // "anomaly" for firings, "status_change" for recoveries
	Severity  string
	Timestamp int64
	Title     string
	Body      string
	Metric    string
	Value     float64
}

func (e SyntheticEvent) ToDBEvent() db.Event {
	nodeID := e.NodeID
	body := e.Body
	return db.Event{
		NodeID:    &nodeID,
		Type:      e.Type,
		Severity:  e.Severity,
		Title:     e.Title,
		Body:      &body,
		CreatedAt: e.Timestamp,
	}
}

// Detector is a streaming anomaly detector used for offline benchmarks.
// Implementations replay a node's metric history in timestamp order and emit
// edge-triggered firing/recovery events so detection delay and false-positive
// rate can be compared fairly against the same ground-truth labels.
type Detector interface {
	Name() string
	Description() string
	Process(nodeID string, points []db.MetricPoint) []SyntheticEvent
}

// metricSelector extracts a scalar value from a MetricPoint. Each detector
// runs independently per metric, so switching metrics is just a function.
type metricSelector struct {
	Key     string // "cpu_percent", "memory_percent", "bandwidth_down", "connections"
	Label   string // Pretty label used in event titles
	Extract func(db.MetricPoint) float64
}

func defaultMetricSelectors() []metricSelector {
	return []metricSelector{
		{Key: "cpu_percent", Label: "CPU", Extract: func(p db.MetricPoint) float64 { return p.CPUPercent }},
		{Key: "memory_percent", Label: "Memory", Extract: func(p db.MetricPoint) float64 { return p.MemoryPercent }},
		{Key: "bandwidth_down", Label: "Bandwidth Down", Extract: func(p db.MetricPoint) float64 { return p.BandwidthDown }},
		{Key: "connections", Label: "Connections", Extract: func(p db.MetricPoint) float64 { return float64(p.Connections) }},
	}
}

// ----- Detector 1: Fixed Threshold (Nagios-style) -----

// FixedThresholdDetector fires when a metric stays above a hard limit for
// MinHold consecutive samples. This is the classic Nagios/Zabbix approach:
// no statistics, just a static line. It is the weakest baseline and the
// one most projects implicitly compare against.
type FixedThresholdDetector struct {
	Thresholds map[string]float64
	MinHold    int
}

func NewFixedThresholdDetector() *FixedThresholdDetector {
	return &FixedThresholdDetector{
		Thresholds: map[string]float64{
			"cpu_percent":    80,
			"memory_percent": 90,
			"bandwidth_down": 10240,
			"connections":    500,
		},
		MinHold: 2,
	}
}

func (d *FixedThresholdDetector) Name() string { return "fixed_threshold" }
func (d *FixedThresholdDetector) Description() string {
	return "Static per-metric thresholds (CPU 80, Memory 90, Connections 500, Bandwidth 10240 KB/s) with 2-sample debounce."
}

func (d *FixedThresholdDetector) Process(nodeID string, points []db.MetricPoint) []SyntheticEvent {
	var events []SyntheticEvent
	for _, sel := range defaultMetricSelectors() {
		threshold, ok := d.Thresholds[sel.Key]
		if !ok {
			continue
		}
		events = append(events, scanEdgeTriggered(nodeID, sel, points, func(value float64, _ int) bool {
			return value >= threshold
		}, d.MinHold, fmt.Sprintf("fixed threshold %.0f", threshold))...)
	}
	return events
}

// ----- Detector 2: Plain Z-Score (rolling) -----

// PlainZScoreDetector maintains a rolling window of the last W samples,
// computes mean and stddev, and fires when the current value exceeds
// mean + k*stddev. Uses non-robust moments — the classic statistical
// "textbook" anomaly detector. Sensitive to heavy tails and bursts, which
// is exactly the behaviour we want to show as a weakness.
type PlainZScoreDetector struct {
	Window      int
	K           float64
	MinCurrents map[string]float64
	MinHold     int
}

func NewPlainZScoreDetector() *PlainZScoreDetector {
	return &PlainZScoreDetector{
		Window: 100,
		K:      3.0,
		MinCurrents: map[string]float64{
			"cpu_percent":    30,
			"memory_percent": 40,
			"bandwidth_down": 512,
			"connections":    50,
		},
		MinHold: 2,
	}
}

func (d *PlainZScoreDetector) Name() string { return "plain_zscore" }
func (d *PlainZScoreDetector) Description() string {
	return "Rolling mean±stddev z-score (|z|≥3, window 100≈50min). Non-robust: long tails inflate stddev and mask real shifts."
}

func (d *PlainZScoreDetector) Process(nodeID string, points []db.MetricPoint) []SyntheticEvent {
	var events []SyntheticEvent
	for _, sel := range defaultMetricSelectors() {
		floor := d.MinCurrents[sel.Key]
		events = append(events, scanEdgeTriggered(nodeID, sel, points, func(value float64, idx int) bool {
			if value < floor {
				return false
			}
			start := idx - d.Window
			if start < 0 {
				start = 0
			}
			if idx-start < 20 {
				return false
			}
			window := make([]float64, 0, idx-start)
			for j := start; j < idx; j++ {
				window = append(window, sel.Extract(points[j]))
			}
			mu := mean(window)
			sigma := stddev(window)
			if sigma < 1e-6 {
				return false
			}
			z := (value - mu) / sigma
			return z >= d.K
		}, d.MinHold, fmt.Sprintf("z-score ≥ %.1f", d.K))...)
	}
	return events
}

// ----- Detector 3: EWMA (exponentially weighted) -----

// EWMADetector tracks an exponentially-weighted moving average and an
// EWMA of the absolute residual. Fires when current deviation from the
// average exceeds k × residual-EWMA. This is the "control chart" school
// of anomaly detection — better at tracking slow drifts than plain z,
// still sensitive to sustained high values because the mean chases them.
type EWMADetector struct {
	Alpha       float64
	BetaDev     float64
	K           float64
	MinCurrents map[string]float64
	MinHold     int
}

func NewEWMADetector() *EWMADetector {
	return &EWMADetector{
		Alpha:   0.1,
		BetaDev: 0.1,
		K:       3.0,
		MinCurrents: map[string]float64{
			"cpu_percent":    30,
			"memory_percent": 40,
			"bandwidth_down": 512,
			"connections":    50,
		},
		MinHold: 2,
	}
}

func (d *EWMADetector) Name() string { return "ewma" }
func (d *EWMADetector) Description() string {
	return "Exponentially-weighted moving average with residual EWMA (α=0.1, k=3). Adapts to slow drift but chases sustained spikes."
}

func (d *EWMADetector) Process(nodeID string, points []db.MetricPoint) []SyntheticEvent {
	var events []SyntheticEvent
	for _, sel := range defaultMetricSelectors() {
		floor := d.MinCurrents[sel.Key]
		var ewmaValue, devValue float64
		initialized := false
		events = append(events, scanEdgeTriggered(nodeID, sel, points, func(value float64, _ int) bool {
			if !initialized {
				ewmaValue = value
				devValue = 0
				initialized = true
				return false
			}
			residual := math.Abs(value - ewmaValue)
			fires := false
			if value >= floor && devValue > 1e-6 && residual >= d.K*devValue && value > ewmaValue {
				fires = true
			}
			ewmaValue = d.Alpha*value + (1-d.Alpha)*ewmaValue
			devValue = d.BetaDev*residual + (1-d.BetaDev)*devValue
			return fires
		}, d.MinHold, fmt.Sprintf("EWMA deviation ≥ %.1fσ", d.K))...)
	}
	return events
}

// ----- Detector 4: Robust Z + Shift (production surrogate, offline replay) -----

// RobustShiftDetector replays the production analytics layer offline: at a
// regular interval it computes median/MAD and baseline-shift statistics on
// a 24h rolling window, applies the same policyForMetric gates as the live
// anomaly scheduler, and emits events on state transitions. This is the
// honest retrospective version of what StarNexus does in production.
type RobustShiftDetector struct {
	ScanIntervalSeconds int64
	WindowSeconds       int64
	MinSamples          int
}

func NewRobustShiftDetector() *RobustShiftDetector {
	return &RobustShiftDetector{
		ScanIntervalSeconds: 300,
		WindowSeconds:       86400,
		MinSamples:          minDataPoints,
	}
}

func (d *RobustShiftDetector) Name() string { return "robust_shift" }
func (d *RobustShiftDetector) Description() string {
	return "Robust z-score (median/MAD) with baseline-shift and multi-gate policy on a 24h rolling window, scanned every 5 min. Production surrogate."
}

func (d *RobustShiftDetector) Process(nodeID string, points []db.MetricPoint) []SyntheticEvent {
	if len(points) == 0 {
		return nil
	}
	selectors := defaultMetricSelectors()
	firing := map[string]bool{}
	fireTs := map[string]int64{}
	var events []SyntheticEvent

	start := points[0].Timestamp
	end := points[len(points)-1].Timestamp
	for t := start + d.ScanIntervalSeconds; t <= end; t += d.ScanIntervalSeconds {
		window := windowPoints(points, t-d.WindowSeconds, t)
		if len(window) < d.MinSamples {
			continue
		}
		detail := BuildDetailAnalytics(window, int(d.WindowSeconds/3600))
		activeNow := map[string]bool{}
		for _, metric := range []MetricAnalysis{detail.CPU, detail.Memory, detail.BandwidthDown, detail.Connections} {
			if shouldAlertOutlier(metric) || shouldAlertShift(metric) {
				activeNow[metric.Label] = true
			}
		}
		for _, sel := range selectors {
			label := metricLabelForKey(sel.Key)
			if activeNow[label] && !firing[label] {
				events = append(events, SyntheticEvent{
					NodeID:    nodeID,
					Type:      "anomaly",
					Severity:  "warning",
					Timestamp: t,
					Title:     fmt.Sprintf("%s outlier detected", label),
					Body:      "robust z + baseline shift gate fired",
					Metric:    sel.Key,
					Value:     sel.Extract(window[len(window)-1]),
				})
				firing[label] = true
				fireTs[label] = t
			} else if !activeNow[label] && firing[label] {
				events = append(events, SyntheticEvent{
					NodeID:    nodeID,
					Type:      "status_change",
					Severity:  "info",
					Timestamp: t,
					Title:     "Node recovered",
					Body:      fmt.Sprintf("%s returned to baseline", label),
					Metric:    sel.Key,
					Value:     sel.Extract(window[len(window)-1]),
				})
				firing[label] = false
			}
		}
	}
	return events
}

// ----- Detector 5: Multivariate Mahalanobis (robust, diagonal) -----

// MahalanobisDetector combines robust z-scores across multiple metrics
// into a single composite score and fires when the composite crosses a
// chi-squared-derived threshold. It is the "multivariate anomaly"
// baseline: instead of alerting on each metric independently, it asks
// whether the overall state vector is unusual compared to the rolling
// baseline.
//
// The implementation uses a diagonal covariance approximation with
// median and MAD for scale (Σ_ii = (1.4826·MAD)^2, Σ_ij = 0 for i≠j).
// This preserves the robust-statistics property of the univariate
// detector, adds a multi-metric combination rule, but does not model
// cross-correlation. Full-covariance MCD-Mahalanobis is left for
// future work because it requires iterative outlier trimming that is
// not easily justified on a short rolling window.
//
// Only positive deviations count toward the composite because we care
// about pressure, not drops. A node dropping to 0% CPU is flagged by
// the status-change path, not the anomaly path.
type MahalanobisDetector struct {
	Window           int
	ComponentK       float64 // per-metric z-gate to include in composite
	CompositeK       float64 // composite (L2 of selected z's) threshold
	MinCurrents      map[string]float64
	MinComponentHits int
	MinHold          int
}

func NewMahalanobisDetector() *MahalanobisDetector {
	return &MahalanobisDetector{
		Window:     200,
		ComponentK: 2.5,
		CompositeK: 3.5,
		MinCurrents: map[string]float64{
			"cpu_percent":    30,
			"memory_percent": 40,
			"bandwidth_down": 256,
			"connections":    40,
		},
		MinComponentHits: 1,
		MinHold:          2,
	}
}

func (d *MahalanobisDetector) Name() string { return "mahalanobis" }
func (d *MahalanobisDetector) Description() string {
	return "Multivariate robust composite: per-metric robust z gated at 2.5σ, L2-combined, fires when composite ≥ 3.5σ. Diagonal-covariance approximation."
}

func (d *MahalanobisDetector) Process(nodeID string, points []db.MetricPoint) []SyntheticEvent {
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

	for idx := range points {
		start := idx - d.Window
		if start < 0 {
			start = 0
		}
		if idx-start < 30 {
			continue
		}

		var composite float64
		hits := 0
		primaryMetric := ""
		primaryValue := 0.0
		for k, sel := range selectors {
			floor := d.MinCurrents[sel.Key]
			current := series[k][idx]
			if current < floor {
				continue
			}
			window := series[k][start:idx]
			sorted := slices.Clone(window)
			slices.Sort(sorted)
			median := percentile(sorted, 50)
			madValue := mad(sorted, median)
			if madValue < 1e-6 {
				continue
			}
			z := 0.6745 * (current - median) / madValue
			if z <= d.ComponentK {
				continue
			}
			composite += z * z
			hits++
			if primaryMetric == "" || z > primaryValue {
				primaryMetric = sel.Label
				primaryValue = z
			}
		}

		composite = math.Sqrt(composite)
		triggered := hits >= d.MinComponentHits && composite >= d.CompositeK
		if triggered {
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
			events = append(events, SyntheticEvent{
				NodeID:    nodeID,
				Type:      "anomaly",
				Severity:  "warning",
				Timestamp: ts,
				Title:     fmt.Sprintf("%s outlier detected", label),
				Body:      fmt.Sprintf("multivariate composite z=%.2f across %d metric(s)", composite, hits),
				Metric:    "multivariate",
				Value:     composite,
			})
			firing = true
		} else if firing && consecutive == 0 {
			events = append(events, SyntheticEvent{
				NodeID:    nodeID,
				Type:      "status_change",
				Severity:  "info",
				Timestamp: ts,
				Title:     "Node recovered",
				Body:      "multivariate composite returned below threshold",
				Metric:    "multivariate",
				Value:     composite,
			})
			firing = false
		}
	}
	return events
}

// ----- helpers -----

func metricLabelForKey(key string) string {
	switch key {
	case "cpu_percent":
		return "CPU"
	case "memory_percent":
		return "Memory"
	case "bandwidth_down":
		return "Bandwidth Down"
	case "connections":
		return "Connections"
	}
	return key
}

func windowPoints(points []db.MetricPoint, from, to int64) []db.MetricPoint {
	lo := sort.Search(len(points), func(i int) bool { return points[i].Timestamp >= from })
	hi := sort.Search(len(points), func(i int) bool { return points[i].Timestamp > to })
	if hi <= lo {
		return nil
	}
	return points[lo:hi]
}

// scanEdgeTriggered walks a metric series and emits a firing event on the
// first sample where the predicate holds for MinHold consecutive samples,
// and a "Node recovered" event when the predicate stops holding. This
// matches how the evaluator looks for detection and recovery events.
func scanEdgeTriggered(nodeID string, sel metricSelector, points []db.MetricPoint, predicate func(value float64, idx int) bool, minHold int, hint string) []SyntheticEvent {
	if minHold < 1 {
		minHold = 1
	}
	var events []SyntheticEvent
	firing := false
	consecutive := 0
	for idx, point := range points {
		value := sel.Extract(point)
		if predicate(value, idx) {
			consecutive++
		} else {
			consecutive = 0
		}
		if !firing && consecutive >= minHold {
			events = append(events, SyntheticEvent{
				NodeID:    nodeID,
				Type:      "anomaly",
				Severity:  "warning",
				Timestamp: point.Timestamp,
				Title:     fmt.Sprintf("%s outlier detected", sel.Label),
				Body:      fmt.Sprintf("%s %s (value %.2f)", sel.Label, hint, value),
				Metric:    sel.Key,
				Value:     value,
			})
			firing = true
		} else if firing && consecutive == 0 {
			events = append(events, SyntheticEvent{
				NodeID:    nodeID,
				Type:      "status_change",
				Severity:  "info",
				Timestamp: point.Timestamp,
				Title:     "Node recovered",
				Body:      fmt.Sprintf("%s returned below %s (value %.2f)", sel.Label, hint, value),
				Metric:    sel.Key,
				Value:     value,
			})
			firing = false
		}
	}
	return events
}

// ----- Benchmark driver -----

// DetectorBenchmark scores one detector against the same ground-truth
// labels used by the production evaluator. The output is directly
// comparable across detectors because every detector sees the same metric
// history and the same labels.
type DetectorBenchmark struct {
	Name             string                  `json:"name"`
	Description      string                  `json:"description"`
	TotalEvents      int                     `json:"total_events"`
	FiringEvents     int                     `json:"firing_events"`
	RecoveryEvents   int                     `json:"recovery_events"`
	GroundTruth      GroundTruthEvaluation   `json:"ground_truth"`
	DetectionDelays  []int64                 `json:"detection_delays_seconds,omitempty"`
	RecoveryDelays   []int64                 `json:"recovery_delays_seconds,omitempty"`
	BootstrapSummary *BootstrapIntervalGroup `json:"bootstrap,omitempty"`
}

// BootstrapIntervalGroup holds 95% bootstrap confidence intervals for the
// detection and recovery delays. CIs let us report "33.7s (CI 27–41s)"
// instead of a bare mean — the right move for n<30 experiments.
type BootstrapIntervalGroup struct {
	DetectionDelayMean      float64    `json:"detection_delay_mean_seconds"`
	DetectionDelayCI        [2]float64 `json:"detection_delay_ci95_seconds"`
	DetectionDelaySD        float64    `json:"detection_delay_sd_seconds"`
	RecoveryDelayMean       float64    `json:"recovery_delay_mean_seconds"`
	RecoveryDelayCI         [2]float64 `json:"recovery_delay_ci95_seconds"`
	RecoveryDelaySD         float64    `json:"recovery_delay_sd_seconds"`
	DetectionBootstrapCount int        `json:"detection_bootstrap_count"`
	RecoveryBootstrapCount  int        `json:"recovery_bootstrap_count"`
}

// RunDetectorBenchmark runs a detector on every node's metric series,
// evaluates its synthetic events against the labels, and bootstraps 95%
// CIs around the delay means.
func RunDetectorBenchmark(detector Detector, labels []ExperimentLabel, pointsByNode map[string][]db.MetricPoint) DetectorBenchmark {
	var synthetic []SyntheticEvent
	nodeIDs := make([]string, 0, len(pointsByNode))
	for nodeID := range pointsByNode {
		nodeIDs = append(nodeIDs, nodeID)
	}
	slices.Sort(nodeIDs)
	for _, nodeID := range nodeIDs {
		synthetic = append(synthetic, detector.Process(nodeID, pointsByNode[nodeID])...)
	}

	events := make([]db.Event, 0, len(synthetic))
	firing := 0
	recovery := 0
	for _, event := range synthetic {
		events = append(events, event.ToDBEvent())
		switch event.Type {
		case "anomaly":
			firing++
		case "status_change":
			recovery++
		}
	}

	gt := BuildGroundTruthEvaluation(labels, events, pointsByNode)
	detectionDelays := make([]int64, 0)
	recoveryDelays := make([]int64, 0)
	for _, experiment := range gt.Experiments {
		if experiment.Detected {
			detectionDelays = append(detectionDelays, experiment.DetectionDelaySeconds)
		}
		if experiment.Recovered {
			recoveryDelays = append(recoveryDelays, experiment.RecoveryDelaySeconds)
		}
	}

	bench := DetectorBenchmark{
		Name:            detector.Name(),
		Description:     detector.Description(),
		TotalEvents:     len(synthetic),
		FiringEvents:    firing,
		RecoveryEvents:  recovery,
		GroundTruth:     gt,
		DetectionDelays: detectionDelays,
		RecoveryDelays:  recoveryDelays,
	}
	if len(detectionDelays) > 0 || len(recoveryDelays) > 0 {
		group := bootstrapDelaySummary(detectionDelays, recoveryDelays, 2000)
		bench.BootstrapSummary = &group
	}
	return bench
}

// bootstrapDelaySummary runs a seeded non-parametric bootstrap to get a
// 95% CI for the mean detection and recovery delays. 2000 resamples is
// overkill for n<30 but cheap and conservative.
func bootstrapDelaySummary(detection, recovery []int64, iterations int) BootstrapIntervalGroup {
	detMean, detLo, detHi, detSD := bootstrapMeanCI(detection, iterations, 7)
	recMean, recLo, recHi, recSD := bootstrapMeanCI(recovery, iterations, 13)
	return BootstrapIntervalGroup{
		DetectionDelayMean:      detMean,
		DetectionDelayCI:        [2]float64{detLo, detHi},
		DetectionDelaySD:        detSD,
		RecoveryDelayMean:       recMean,
		RecoveryDelayCI:         [2]float64{recLo, recHi},
		RecoveryDelaySD:         recSD,
		DetectionBootstrapCount: len(detection),
		RecoveryBootstrapCount:  len(recovery),
	}
}

func bootstrapMeanCI(values []int64, iterations int, seed uint64) (mean_, lo, hi, sd float64) {
	if len(values) == 0 {
		return 0, 0, 0, 0
	}
	sum := 0.0
	for _, value := range values {
		sum += float64(value)
	}
	mean_ = sum / float64(len(values))
	if len(values) == 1 {
		return mean_, mean_, mean_, 0
	}
	variance := 0.0
	for _, value := range values {
		diff := float64(value) - mean_
		variance += diff * diff
	}
	sd = math.Sqrt(variance / float64(len(values)-1))
	if iterations <= 0 {
		return mean_, mean_, mean_, sd
	}
	means := make([]float64, iterations)
	state := seed | 1
	// LCGs have poor low-bit randomness, so we take the upper 32 bits
	// before reducing modulo len(values).
	for i := 0; i < iterations; i++ {
		resampleSum := 0.0
		for j := 0; j < len(values); j++ {
			state = nextLCG(state)
			idx := int((state >> 32) % uint64(len(values)))
			resampleSum += float64(values[idx])
		}
		means[i] = resampleSum / float64(len(values))
	}
	slices.Sort(means)
	loRank := int(math.Floor(0.025 * float64(iterations)))
	hiRank := int(math.Ceil(0.975*float64(iterations))) - 1
	if loRank < 0 {
		loRank = 0
	}
	if hiRank >= iterations {
		hiRank = iterations - 1
	}
	lo = means[loRank]
	hi = means[hiRank]
	return mean_, lo, hi, sd
}

// nextLCG is a 64-bit linear congruential RNG used for the bootstrap.
// A fixed-seed LCG keeps benchmark outputs reproducible without taking a
// math/rand dependency at package scope.
func nextLCG(state uint64) uint64 {
	return state*6364136223846793005 + 1442695040888963407
}
