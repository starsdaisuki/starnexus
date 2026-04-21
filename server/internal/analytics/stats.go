package analytics

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type MetricAnalysis struct {
	Label        string        `json:"label"`
	Current      float64       `json:"current"`
	Mean         float64       `json:"mean"`
	Median       float64       `json:"median"`
	Min          float64       `json:"min"`
	Max          float64       `json:"max"`
	P95          float64       `json:"p95"`
	Stddev       float64       `json:"stddev"`
	MAD          float64       `json:"mad"`
	RobustZ      float64       `json:"robust_z"`
	SlopePerHour float64       `json:"slope_per_hour"`
	Trend        string        `json:"trend"`
	Volatility   string        `json:"volatility"`
	Outlier      bool          `json:"outlier"`
	Shift        ShiftAnalysis `json:"shift"`
}

type ShiftAnalysis struct {
	BaselineMean       float64 `json:"baseline_mean"`
	RecentMean         float64 `json:"recent_mean"`
	BaselineMedian     float64 `json:"baseline_median"`
	RecentMedian       float64 `json:"recent_median"`
	DeltaPercent       float64 `json:"delta_percent"`
	ShiftScore         float64 `json:"shift_score"`
	EWMA               float64 `json:"ewma"`
	EWMADeviation      float64 `json:"ewma_deviation"`
	RecentWindowSize   int     `json:"recent_window_size"`
	BaselineWindowSize int     `json:"baseline_window_size"`
	Significant        bool    `json:"significant"`
}

type DetailAnalytics struct {
	SampleCount     int            `json:"sample_count"`
	CoveragePercent float64        `json:"coverage_percent"`
	RiskLevel       string         `json:"risk_level"`
	Summary         string         `json:"summary"`
	Highlights      []string       `json:"highlights"`
	CPU             MetricAnalysis `json:"cpu"`
	Memory          MetricAnalysis `json:"memory"`
	BandwidthDown   MetricAnalysis `json:"bandwidth_down"`
	Connections     MetricAnalysis `json:"connections"`
}

func BuildDetailAnalytics(points []db.MetricPoint, windowHours int) DetailAnalytics {
	analytics := DetailAnalytics{
		SampleCount: len(points),
		Highlights:  []string{},
		RiskLevel:   "unknown",
		Summary:     "Not enough samples to characterize this node yet.",
	}

	if windowHours > 0 {
		analytics.CoveragePercent = math.Min(100, float64(len(points))*30/float64(windowHours*3600)*100)
	}

	if len(points) == 0 {
		return analytics
	}

	analytics.CPU = analyzeSeries("CPU", points, func(point db.MetricPoint) float64 { return point.CPUPercent })
	analytics.Memory = analyzeSeries("Memory", points, func(point db.MetricPoint) float64 { return point.MemoryPercent })
	analytics.BandwidthDown = analyzeSeries("Bandwidth Down", points, func(point db.MetricPoint) float64 { return point.BandwidthDown })
	analytics.Connections = analyzeSeries("Connections", points, func(point db.MetricPoint) float64 { return float64(point.Connections) })

	analytics.RiskLevel = classifyRisk(analytics)
	analytics.Highlights = buildHighlights(analytics)
	analytics.Summary = buildAnalyticsSummary(analytics)
	return analytics
}

func analyzeSeries(label string, points []db.MetricPoint, extract func(db.MetricPoint) float64) MetricAnalysis {
	values := make([]float64, 0, len(points))
	timestamps := make([]float64, 0, len(points))
	for _, point := range points {
		values = append(values, extract(point))
		timestamps = append(timestamps, float64(point.Timestamp))
	}

	sorted := slices.Clone(values)
	slices.Sort(sorted)

	analysis := MetricAnalysis{
		Label:   label,
		Current: values[len(values)-1],
		Mean:    mean(values),
		Median:  percentile(sorted, 50),
		Min:     sorted[0],
		Max:     sorted[len(sorted)-1],
		P95:     percentile(sorted, 95),
		Stddev:  stddev(values),
	}
	analysis.MAD = mad(sorted, analysis.Median)
	analysis.RobustZ = robustZ(analysis.Current, analysis.Median, analysis.MAD)
	analysis.SlopePerHour = slopePerHour(timestamps, values)
	analysis.Trend = classifyTrend(analysis.SlopePerHour, analysis.P95, analysis.Median)
	analysis.Volatility = classifyVolatility(analysis.Stddev, analysis.Mean, analysis.MAD, analysis.Median)
	analysis.Shift = analyzeShift(values)
	analysis.Outlier = math.Abs(analysis.RobustZ) >= 3.5 && math.Abs(analysis.Current-analysis.Median) >= minMeaningfulDelta(analysis.Median)
	return analysis
}

func analyzeShift(values []float64) ShiftAnalysis {
	shift := ShiftAnalysis{}
	if len(values) < 12 {
		shift.EWMA = ewma(values, 0.2)
		shift.EWMADeviation = lastOrZero(values) - shift.EWMA
		return shift
	}

	recentCount := recentWindowSize(len(values))
	baselineCount := len(values) - recentCount
	if baselineCount < 6 {
		recentCount = len(values) / 2
		baselineCount = len(values) - recentCount
	}
	if baselineCount < 6 || recentCount < 3 {
		shift.EWMA = ewma(values, 0.2)
		shift.EWMADeviation = lastOrZero(values) - shift.EWMA
		return shift
	}

	baseline := values[:baselineCount]
	recent := values[baselineCount:]

	baselineSorted := slices.Clone(baseline)
	recentSorted := slices.Clone(recent)
	slices.Sort(baselineSorted)
	slices.Sort(recentSorted)

	baselineMedian := percentile(baselineSorted, 50)
	recentMedian := percentile(recentSorted, 50)
	scale := math.Max(mad(baselineSorted, baselineMedian), minMeaningfulDelta(baselineMedian))

	shift = ShiftAnalysis{
		BaselineMean:       mean(baseline),
		RecentMean:         mean(recent),
		BaselineMedian:     baselineMedian,
		RecentMedian:       recentMedian,
		DeltaPercent:       relativeDeltaPercent(recentMedian, baselineMedian),
		ShiftScore:         (recentMedian - baselineMedian) / scale,
		EWMA:               ewma(values, 0.2),
		RecentWindowSize:   recentCount,
		BaselineWindowSize: baselineCount,
	}
	shift.EWMADeviation = lastOrZero(values) - shift.EWMA
	shift.Significant = math.Abs(shift.ShiftScore) >= 2.5 && math.Abs(shift.DeltaPercent) >= 10
	return shift
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func stddev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	avg := mean(values)
	var total float64
	for _, value := range values {
		diff := value - avg
		total += diff * diff
	}
	return math.Sqrt(total / float64(len(values)))
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	weight := rank - float64(lower)
	return sorted[lower] + (sorted[upper]-sorted[lower])*weight
}

func mad(sorted []float64, median float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	deviations := make([]float64, 0, len(sorted))
	for _, value := range sorted {
		deviations = append(deviations, math.Abs(value-median))
	}
	slices.Sort(deviations)
	return percentile(deviations, 50)
}

func robustZ(current, median, mad float64) float64 {
	if mad < 1e-9 {
		return 0
	}
	return 0.6745 * (current - median) / mad
}

func ewma(values []float64, alpha float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if alpha <= 0 || alpha > 1 {
		alpha = 0.2
	}
	current := values[0]
	for _, value := range values[1:] {
		current = alpha*value + (1-alpha)*current
	}
	return current
}

func lastOrZero(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return values[len(values)-1]
}

func relativeDeltaPercent(current, baseline float64) float64 {
	scale := math.Max(math.Abs(baseline), 1)
	return ((current - baseline) / scale) * 100
}

func minMeaningfulDelta(baseline float64) float64 {
	return math.Max(math.Abs(baseline)*0.08, 1)
}

func recentWindowSize(total int) int {
	size := total / 6
	switch {
	case size < 6:
		return 6
	case size > 120:
		return 120
	default:
		return size
	}
}

func slopePerHour(timestamps, values []float64) float64 {
	if len(timestamps) < 2 {
		return 0
	}

	var sumX, sumY, sumXY, sumXX float64
	for i := range timestamps {
		x := timestamps[i]
		y := values[i]
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}

	n := float64(len(timestamps))
	denominator := n*sumXX - sumX*sumX
	if math.Abs(denominator) < 1e-9 {
		return 0
	}

	slopePerSecond := (n*sumXY - sumX*sumY) / denominator
	return slopePerSecond * 3600
}

func classifyTrend(slopePerHour, p95, median float64) string {
	scale := math.Max(math.Abs(median), math.Max(p95, 1))
	if slopePerHour > scale*0.08 {
		return "rising"
	}
	if slopePerHour < -scale*0.08 {
		return "falling"
	}
	return "stable"
}

func classifyVolatility(stddev, mean, mad, median float64) string {
	scale := math.Max(math.Abs(mean), math.Abs(median))
	if scale < 1 {
		scale = 1
	}
	cv := stddev / scale
	robust := mad / scale
	score := math.Max(cv, robust)
	switch {
	case score >= 0.35:
		return "high"
	case score >= 0.15:
		return "moderate"
	default:
		return "low"
	}
}

func classifyRisk(analytics DetailAnalytics) string {
	metrics := []MetricAnalysis{analytics.CPU, analytics.Memory, analytics.BandwidthDown, analytics.Connections}
	hasHighVolatility := false
	hasRisingPressure := false
	hasStrongOutlier := false
	hasStrongShift := false
	for _, metric := range metrics {
		if metric.Volatility == "high" {
			hasHighVolatility = true
		}
		if metric.Trend == "rising" && (metric.Label == "CPU" || metric.Label == "Memory" || metric.Label == "Connections") {
			hasRisingPressure = true
		}
		if metric.Shift.Significant && (metric.Label == "CPU" || metric.Label == "Memory" || metric.Label == "Connections") {
			hasRisingPressure = true
		}
		if metric.Outlier && math.Abs(metric.RobustZ) >= 5 {
			hasStrongOutlier = true
		}
		if metric.Shift.Significant && math.Abs(metric.Shift.ShiftScore) >= 4 && math.Abs(metric.Shift.DeltaPercent) >= 15 {
			hasStrongShift = true
		}
	}

	switch {
	case analytics.CPU.Current >= 85 || analytics.Memory.Current >= 90 || hasStrongOutlier || hasStrongShift:
		return "critical"
	case analytics.CPU.Current >= 70 || analytics.Memory.Current >= 80 || hasHighVolatility || hasRisingPressure:
		return "elevated"
	default:
		return "stable"
	}
}

func buildHighlights(analytics DetailAnalytics) []string {
	highlights := []string{}
	if analytics.CPU.Outlier {
		highlights = append(highlights, fmt.Sprintf("CPU is %.1f MAD-based sigma away from its median baseline.", analytics.CPU.RobustZ))
	}
	if analytics.CPU.Shift.Significant {
		highlights = append(highlights, fmt.Sprintf("CPU recent median moved %.0f%% versus baseline (shift score %.1f).", analytics.CPU.Shift.DeltaPercent, analytics.CPU.Shift.ShiftScore))
	}
	if analytics.Memory.Outlier {
		highlights = append(highlights, fmt.Sprintf("Memory usage is a strong outlier versus its recent baseline (robust z %.1f).", analytics.Memory.RobustZ))
	}
	if analytics.Memory.Shift.Significant {
		highlights = append(highlights, fmt.Sprintf("Memory recent median moved %.0f%% versus baseline (shift score %.1f).", analytics.Memory.Shift.DeltaPercent, analytics.Memory.Shift.ShiftScore))
	}
	if analytics.Connections.Trend == "rising" {
		highlights = append(highlights, fmt.Sprintf("TCP connections are trending upward at %.1f per hour.", analytics.Connections.SlopePerHour))
	}
	if analytics.Connections.Shift.Significant {
		highlights = append(highlights, fmt.Sprintf("Connection load shifted by %.0f%% relative to the historical baseline.", analytics.Connections.Shift.DeltaPercent))
	}
	if analytics.BandwidthDown.Volatility == "high" {
		highlights = append(highlights, "Bandwidth shows high short-window volatility, which may indicate bursty traffic or unstable ingress.")
	}
	if analytics.CoveragePercent < 60 {
		highlights = append(highlights, fmt.Sprintf("Only %.0f%% of the selected window is covered by samples, so confidence is limited.", analytics.CoveragePercent))
	}
	if len(highlights) == 0 {
		highlights = append(highlights, "Recent metrics are statistically stable relative to the selected window.")
	}
	return highlights
}

func buildAnalyticsSummary(analytics DetailAnalytics) string {
	parts := []string{
		fmt.Sprintf("%d samples covering %.0f%% of the selected window", analytics.SampleCount, analytics.CoveragePercent),
		fmt.Sprintf("risk level: %s", analytics.RiskLevel),
		fmt.Sprintf("CPU trend %s", analytics.CPU.Trend),
		fmt.Sprintf("memory volatility %s", analytics.Memory.Volatility),
	}
	if analytics.CPU.Shift.Significant || analytics.Memory.Shift.Significant {
		parts = append(parts, fmt.Sprintf("recent baseline shift %.0f%% / %.0f%%", analytics.CPU.Shift.DeltaPercent, analytics.Memory.Shift.DeltaPercent))
	}
	return strings.Join(parts, " • ")
}
