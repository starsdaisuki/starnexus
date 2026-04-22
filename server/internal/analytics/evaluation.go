package analytics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type EvaluationReport struct {
	GeneratedAt          int64                  `json:"generated_at"`
	WindowHours          int                    `json:"window_hours"`
	NodeCount            int                    `json:"node_count"`
	MeanCoveragePercent  float64                `json:"mean_coverage_percent"`
	RiskDistribution     map[string]int         `json:"risk_distribution"`
	TotalSignals         int                    `json:"total_signals"`
	EventCount           int                    `json:"event_count"`
	AnomalyEventCount    int                    `json:"anomaly_event_count"`
	StatusEventCount     int                    `json:"status_event_count"`
	SignalEventAgreement SignalEventAgreement   `json:"signal_event_agreement"`
	GroundTruth          *GroundTruthEvaluation `json:"ground_truth,omitempty"`
	NodeSummaries        []EvaluationNode       `json:"node_summaries"`
	Notes                []string               `json:"notes"`
}

type SignalEventAgreement struct {
	NodesWithSignals          int     `json:"nodes_with_signals"`
	NodesWithEvents           int     `json:"nodes_with_events"`
	NodesWithSignalsAndEvents int     `json:"nodes_with_signals_and_events"`
	ProxyPrecisionPercent     float64 `json:"proxy_precision_percent"`
	ProxyRecallPercent        float64 `json:"proxy_recall_percent"`
}

type EvaluationNode struct {
	NodeID           string  `json:"node_id"`
	NodeName         string  `json:"node_name"`
	RiskLevel        string  `json:"risk_level"`
	CoveragePercent  float64 `json:"coverage_percent"`
	SignalCount      int     `json:"signal_count"`
	CPUShiftPercent  float64 `json:"cpu_shift_percent"`
	MemShiftPercent  float64 `json:"memory_shift_percent"`
	ConnShiftPercent float64 `json:"connections_shift_percent"`
	Summary          string  `json:"summary"`
}

type ExperimentLabel struct {
	ExperimentID      string `json:"experiment_id"`
	NodeID            string `json:"node_id"`
	InjectionType     string `json:"injection_type"`
	ExpectedMetric    string `json:"expected_metric"`
	ExpectedDirection string `json:"expected_direction"`
	StartedAt         int64  `json:"started_at"`
	EndedAt           int64  `json:"ended_at"`
	DurationSeconds   int64  `json:"duration_seconds"`
	SSHHost           string `json:"ssh_host,omitempty"`
	Notes             string `json:"notes,omitempty"`
}

type GroundTruthEvaluation struct {
	ExperimentCount           int                    `json:"experiment_count"`
	DetectedCount             int                    `json:"detected_count"`
	MissedCount               int                    `json:"missed_count"`
	RecoveredCount            int                    `json:"recovered_count"`
	StatusDetectionCount      int                    `json:"status_detection_count"`
	AnomalyDetectionCount     int                    `json:"anomaly_detection_count"`
	MeanDetectionDelaySeconds float64                `json:"mean_detection_delay_seconds"`
	MeanRecoveryDelaySeconds  float64                `json:"mean_recovery_delay_seconds"`
	ObservationNodeHours      float64                `json:"observation_node_hours"`
	ExperimentNodeHours       float64                `json:"experiment_node_hours"`
	SteadyStateNodeHours      float64                `json:"steady_state_node_hours"`
	FalsePositiveEventCount   int                    `json:"false_positive_event_count"`
	FalsePositiveStatusCount  int                    `json:"false_positive_status_count"`
	FalsePositiveAnomalyCount int                    `json:"false_positive_anomaly_count"`
	FalsePositiveRate         float64                `json:"false_positive_events_per_node_hour"`
	FalsePositiveStatusRate   float64                `json:"false_positive_status_per_node_hour"`
	FalsePositiveAnomalyRate  float64                `json:"false_positive_anomaly_per_node_hour"`
	DetectionRatePercent      float64                `json:"detection_rate_percent"`
	RecoveryRatePercent       float64                `json:"recovery_rate_percent"`
	Experiments               []ExperimentEvaluation `json:"experiments"`
}

type ExperimentEvaluation struct {
	ExperimentID          string   `json:"experiment_id"`
	NodeID                string   `json:"node_id"`
	InjectionType         string   `json:"injection_type"`
	ExpectedMetric        string   `json:"expected_metric"`
	StartedAt             int64    `json:"started_at"`
	EndedAt               int64    `json:"ended_at"`
	Detected              bool     `json:"detected"`
	DetectionType         string   `json:"detection_type,omitempty"`
	DetectionSeverity     string   `json:"detection_severity,omitempty"`
	FirstDetectionAt      int64    `json:"first_detection_at,omitempty"`
	DetectionDelaySeconds int64    `json:"detection_delay_seconds,omitempty"`
	Recovered             bool     `json:"recovered"`
	FirstRecoveryAt       int64    `json:"first_recovery_at,omitempty"`
	RecoveryDelaySeconds  int64    `json:"recovery_delay_seconds,omitempty"`
	PeakMetricValue       float64  `json:"peak_metric_value"`
	DetectionTitles       []string `json:"detection_titles,omitempty"`
}

func BuildEvaluationReport(windowHours int, samples []FleetNodeSample, events []db.Event) EvaluationReport {
	report := EvaluationReport{
		GeneratedAt:      time.Now().Unix(),
		WindowHours:      windowHours,
		NodeCount:        len(samples),
		RiskDistribution: map[string]int{"critical": 0, "elevated": 0, "stable": 0, "unknown": 0},
		NodeSummaries:    make([]EvaluationNode, 0, len(samples)),
		Notes: []string{
			"This is an unsupervised proxy evaluation: status and anomaly events are treated as weak labels, not ground truth.",
			"Use controlled fault injection to measure true false positive rate and detection delay.",
		},
	}

	nodesWithSignals := map[string]bool{}
	nodesWithEvents := map[string]bool{}
	var coverageTotal float64

	for _, event := range events {
		report.EventCount++
		if event.Type == "anomaly" {
			report.AnomalyEventCount++
		}
		if event.Type == "status_change" {
			report.StatusEventCount++
		}
		if event.NodeID != nil {
			nodesWithEvents[*event.NodeID] = true
		}
	}

	for _, sample := range samples {
		risk := sample.Analytics.RiskLevel
		if _, ok := report.RiskDistribution[risk]; !ok {
			risk = "unknown"
		}
		report.RiskDistribution[risk]++

		signalCount := countSignals(sample.Analytics)
		report.TotalSignals += signalCount
		coverageTotal += sample.Analytics.CoveragePercent
		if signalCount > 0 || sample.Analytics.RiskLevel == "critical" || sample.Analytics.RiskLevel == "elevated" {
			nodesWithSignals[sample.Node.ID] = true
		}

		report.NodeSummaries = append(report.NodeSummaries, EvaluationNode{
			NodeID:           sample.Node.ID,
			NodeName:         sample.Node.Name,
			RiskLevel:        sample.Analytics.RiskLevel,
			CoveragePercent:  sample.Analytics.CoveragePercent,
			SignalCount:      signalCount,
			CPUShiftPercent:  sample.Analytics.CPU.Shift.DeltaPercent,
			MemShiftPercent:  sample.Analytics.Memory.Shift.DeltaPercent,
			ConnShiftPercent: sample.Analytics.Connections.Shift.DeltaPercent,
			Summary:          sample.Analytics.Summary,
		})
	}

	if report.NodeCount > 0 {
		report.MeanCoveragePercent = coverageTotal / float64(report.NodeCount)
	}

	report.SignalEventAgreement = buildSignalEventAgreement(nodesWithSignals, nodesWithEvents)
	report.Notes = append(report.Notes, buildEvaluationSummary(report))
	return report
}

func BuildGroundTruthEvaluation(labels []ExperimentLabel, events []db.Event, pointsByNode map[string][]db.MetricPoint) GroundTruthEvaluation {
	evaluation := GroundTruthEvaluation{
		ExperimentCount: len(labels),
		Experiments:     make([]ExperimentEvaluation, 0, len(labels)),
	}
	if len(labels) == 0 {
		return evaluation
	}

	var detectionDelayTotal int64
	var recoveryDelayTotal int64
	for _, label := range labels {
		result := evaluateExperiment(label, events, pointsByNode[label.NodeID])
		if result.Detected {
			evaluation.DetectedCount++
			detectionDelayTotal += result.DetectionDelaySeconds
			switch result.DetectionType {
			case "status_change":
				evaluation.StatusDetectionCount++
			case "anomaly":
				evaluation.AnomalyDetectionCount++
			}
		} else {
			evaluation.MissedCount++
		}
		if result.Recovered {
			evaluation.RecoveredCount++
			recoveryDelayTotal += result.RecoveryDelaySeconds
		}
		evaluation.Experiments = append(evaluation.Experiments, result)
	}

	if evaluation.DetectedCount > 0 {
		evaluation.MeanDetectionDelaySeconds = float64(detectionDelayTotal) / float64(evaluation.DetectedCount)
	}
	if evaluation.RecoveredCount > 0 {
		evaluation.MeanRecoveryDelaySeconds = float64(recoveryDelayTotal) / float64(evaluation.RecoveredCount)
	}
	evaluation.FalsePositiveEventCount, evaluation.FalsePositiveStatusCount, evaluation.FalsePositiveAnomalyCount = countFalsePositiveEvents(labels, events)
	evaluation.ObservationNodeHours, evaluation.ExperimentNodeHours, evaluation.SteadyStateNodeHours = calculateGroundTruthExposure(labels, pointsByNode)
	if evaluation.SteadyStateNodeHours > 0 {
		evaluation.FalsePositiveRate = float64(evaluation.FalsePositiveEventCount) / evaluation.SteadyStateNodeHours
		evaluation.FalsePositiveStatusRate = float64(evaluation.FalsePositiveStatusCount) / evaluation.SteadyStateNodeHours
		evaluation.FalsePositiveAnomalyRate = float64(evaluation.FalsePositiveAnomalyCount) / evaluation.SteadyStateNodeHours
	}
	evaluation.DetectionRatePercent = float64(evaluation.DetectedCount) / float64(evaluation.ExperimentCount) * 100
	evaluation.RecoveryRatePercent = float64(evaluation.RecoveredCount) / float64(evaluation.ExperimentCount) * 100
	return evaluation
}

func LoadExperimentLabelsJSONL(path string) ([]ExperimentLabel, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var labels []ExperimentLabel
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var label ExperimentLabel
		if err := json.Unmarshal(line, &label); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if label.ExperimentID == "" {
			label.ExperimentID = fmt.Sprintf("%s-%d", label.NodeID, label.StartedAt)
		}
		if label.ExpectedMetric == "" {
			label.ExpectedMetric = "cpu_percent"
		}
		if label.DurationSeconds == 0 && label.EndedAt > label.StartedAt {
			label.DurationSeconds = label.EndedAt - label.StartedAt
		}
		labels = append(labels, label)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return labels, nil
}

func evaluateExperiment(label ExperimentLabel, events []db.Event, points []db.MetricPoint) ExperimentEvaluation {
	result := ExperimentEvaluation{
		ExperimentID:    label.ExperimentID,
		NodeID:          label.NodeID,
		InjectionType:   label.InjectionType,
		ExpectedMetric:  label.ExpectedMetric,
		StartedAt:       label.StartedAt,
		EndedAt:         label.EndedAt,
		PeakMetricValue: peakMetric(points, label.ExpectedMetric, label.StartedAt, label.EndedAt),
	}

	detectionEnd := label.EndedAt + 300
	recoveryEnd := label.EndedAt + 900
	for _, event := range eventsChronological(events) {
		if event.NodeID == nil || *event.NodeID != label.NodeID {
			continue
		}
		if !isDetectionEvent(event) {
			continue
		}
		if event.CreatedAt >= label.StartedAt && event.CreatedAt <= detectionEnd {
			result.Detected = true
			result.DetectionType = event.Type
			result.DetectionSeverity = event.Severity
			result.FirstDetectionAt = event.CreatedAt
			result.DetectionDelaySeconds = event.CreatedAt - label.StartedAt
			result.DetectionTitles = append(result.DetectionTitles, event.Title)
			break
		}
	}

	for _, event := range eventsChronological(events) {
		if event.NodeID == nil || *event.NodeID != label.NodeID {
			continue
		}
		if event.CreatedAt >= label.EndedAt && event.CreatedAt <= recoveryEnd && isRecoveryEvent(event) {
			result.Recovered = true
			result.FirstRecoveryAt = event.CreatedAt
			result.RecoveryDelaySeconds = event.CreatedAt - label.EndedAt
			break
		}
	}

	return result
}

func countFalsePositiveEvents(labels []ExperimentLabel, events []db.Event) (total int, statusCount int, anomalyCount int) {
	for _, event := range events {
		if !isDetectionEvent(event) {
			continue
		}
		if event.NodeID == nil {
			total++
			switch event.Type {
			case "status_change":
				statusCount++
			case "anomaly":
				anomalyCount++
			}
			continue
		}
		insideExperiment := false
		for _, label := range labels {
			if label.NodeID != *event.NodeID {
				continue
			}
			if event.CreatedAt >= label.StartedAt && event.CreatedAt <= label.EndedAt+300 {
				insideExperiment = true
				break
			}
		}
		if !insideExperiment {
			total++
			switch event.Type {
			case "status_change":
				statusCount++
			case "anomaly":
				anomalyCount++
			}
		}
	}
	return total, statusCount, anomalyCount
}

func calculateGroundTruthExposure(labels []ExperimentLabel, pointsByNode map[string][]db.MetricPoint) (observationHours float64, experimentHours float64, steadyStateHours float64) {
	for nodeID, points := range pointsByNode {
		if len(points) < 2 {
			continue
		}
		start, end := metricSpan(points)
		if end <= start {
			continue
		}
		observationSeconds := end - start
		experimentSeconds := overlapSeconds(start, end, experimentIntervalsForNode(labels, nodeID, false))
		exclusionSeconds := overlapSeconds(start, end, experimentIntervalsForNode(labels, nodeID, true))
		steadySeconds := observationSeconds - exclusionSeconds
		if steadySeconds < 0 {
			steadySeconds = 0
		}
		observationHours += float64(observationSeconds) / 3600
		experimentHours += float64(experimentSeconds) / 3600
		steadyStateHours += float64(steadySeconds) / 3600
	}
	return observationHours, experimentHours, steadyStateHours
}

type interval struct {
	start int64
	end   int64
}

func metricSpan(points []db.MetricPoint) (int64, int64) {
	start := points[0].Timestamp
	end := points[0].Timestamp
	for _, point := range points[1:] {
		if point.Timestamp < start {
			start = point.Timestamp
		}
		if point.Timestamp > end {
			end = point.Timestamp
		}
	}
	return start, end
}

func experimentIntervalsForNode(labels []ExperimentLabel, nodeID string, includeDetectionGrace bool) []interval {
	intervals := []interval{}
	for _, label := range labels {
		if label.NodeID != nodeID || label.EndedAt <= label.StartedAt {
			continue
		}
		end := label.EndedAt
		if includeDetectionGrace {
			end += 300
		}
		intervals = append(intervals, interval{start: label.StartedAt, end: end})
	}
	return mergeIntervals(intervals)
}

func mergeIntervals(intervals []interval) []interval {
	if len(intervals) < 2 {
		return intervals
	}
	for i := 0; i < len(intervals); i++ {
		for j := i + 1; j < len(intervals); j++ {
			if intervals[j].start < intervals[i].start {
				intervals[i], intervals[j] = intervals[j], intervals[i]
			}
		}
	}
	merged := []interval{intervals[0]}
	for _, current := range intervals[1:] {
		last := &merged[len(merged)-1]
		if current.start <= last.end {
			if current.end > last.end {
				last.end = current.end
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func overlapSeconds(start, end int64, intervals []interval) int64 {
	var total int64
	for _, item := range intervals {
		overlapStart := maxInt64(start, item.start)
		overlapEnd := minInt64(end, item.end)
		if overlapEnd > overlapStart {
			total += overlapEnd - overlapStart
		}
	}
	return total
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func eventsChronological(events []db.Event) []db.Event {
	ordered := append([]db.Event(nil), events...)
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if ordered[j].CreatedAt < ordered[i].CreatedAt {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	return ordered
}

func isDetectionEvent(event db.Event) bool {
	return event.Type == "anomaly" || event.Type == "status_change"
}

func isRecoveryEvent(event db.Event) bool {
	if event.Type != "status_change" {
		return false
	}
	title := strings.ToLower(event.Title)
	body := ""
	if event.Body != nil {
		body = strings.ToLower(*event.Body)
	}
	return strings.Contains(title, "recovered") || strings.Contains(body, "recovered") || strings.Contains(body, "healthy")
}

func peakMetric(points []db.MetricPoint, metric string, start, end int64) float64 {
	peak := 0.0
	for _, point := range points {
		if point.Timestamp < start || point.Timestamp > end {
			continue
		}
		value := metricValue(point, metric)
		if value > peak {
			peak = value
		}
	}
	return peak
}

func metricValue(point db.MetricPoint, metric string) float64 {
	switch metric {
	case "cpu_percent", "cpu":
		return point.CPUPercent
	case "memory_percent", "memory":
		return point.MemoryPercent
	case "bandwidth_down":
		return point.BandwidthDown
	case "bandwidth_up":
		return point.BandwidthUp
	case "connections":
		return float64(point.Connections)
	default:
		return point.CPUPercent
	}
}

func buildSignalEventAgreement(nodesWithSignals, nodesWithEvents map[string]bool) SignalEventAgreement {
	agreement := SignalEventAgreement{
		NodesWithSignals: len(nodesWithSignals),
		NodesWithEvents:  len(nodesWithEvents),
	}
	for nodeID := range nodesWithSignals {
		if nodesWithEvents[nodeID] {
			agreement.NodesWithSignalsAndEvents++
		}
	}
	if agreement.NodesWithSignals > 0 {
		agreement.ProxyPrecisionPercent = float64(agreement.NodesWithSignalsAndEvents) / float64(agreement.NodesWithSignals) * 100
	}
	if agreement.NodesWithEvents > 0 {
		agreement.ProxyRecallPercent = float64(agreement.NodesWithSignalsAndEvents) / float64(agreement.NodesWithEvents) * 100
	}
	return agreement
}

func buildEvaluationSummary(report EvaluationReport) string {
	parts := []string{
		fmt.Sprintf("%d nodes", report.NodeCount),
		fmt.Sprintf("%.0f%% mean coverage", report.MeanCoveragePercent),
		fmt.Sprintf("%d total statistical signals", report.TotalSignals),
		fmt.Sprintf("%.0f%% proxy precision", nanSafe(report.SignalEventAgreement.ProxyPrecisionPercent)),
		fmt.Sprintf("%.0f%% proxy recall", nanSafe(report.SignalEventAgreement.ProxyRecallPercent)),
	}
	return strings.Join(parts, " • ")
}

func nanSafe(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}
