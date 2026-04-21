package analytics

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type ReliabilityNode struct {
	NodeID               string   `json:"node_id"`
	NodeName             string   `json:"node_name"`
	Status               string   `json:"status"`
	OperationalScore     float64  `json:"operational_score"`
	AvailabilityPercent  float64  `json:"availability_percent"`
	DataCoveragePercent  float64  `json:"data_coverage_percent"`
	LastSeenAgeSeconds   int64    `json:"last_seen_age_seconds"`
	IncidentCount        int      `json:"incident_count"`
	SignalEventCount     int      `json:"signal_event_count"`
	ExperimentEventCount int      `json:"experiment_event_count"`
	CriticalEventCount   int      `json:"critical_event_count"`
	WarningEventCount    int      `json:"warning_event_count"`
	DataQuality          string   `json:"data_quality"`
	Recommendation       string   `json:"recommendation"`
	Signals              []string `json:"signals"`
}

type ReliabilityAnalytics struct {
	WindowHours           int               `json:"window_hours"`
	FleetOperationalScore float64           `json:"fleet_operational_score"`
	FleetAvailability     float64           `json:"fleet_availability_percent"`
	FleetDataCoverage     float64           `json:"fleet_data_coverage_percent"`
	IncidentCount         int               `json:"incident_count"`
	SignalEventCount      int               `json:"signal_event_count"`
	ExperimentEventCount  int               `json:"experiment_event_count"`
	CriticalEventCount    int               `json:"critical_event_count"`
	WarningEventCount     int               `json:"warning_event_count"`
	Summary               string            `json:"summary"`
	Nodes                 []ReliabilityNode `json:"nodes"`
}

type reliabilityEventCounts struct {
	incidents  int
	signals    int
	experiment int
	critical   int
	warning    int
}

func BuildReliabilityAnalytics(windowHours int, now int64, samples []FleetNodeSample, events []db.Event, labels []ExperimentLabel) ReliabilityAnalytics {
	if now <= 0 {
		now = 0
	}
	report := ReliabilityAnalytics{
		WindowHours: windowHours,
		Nodes:       make([]ReliabilityNode, 0, len(samples)),
	}

	eventsByNode := map[string]reliabilityEventCounts{}
	for _, event := range events {
		countFleetEvent(&report, event, labels)
		if event.NodeID == nil || *event.NodeID == "" {
			continue
		}
		counts := eventsByNode[*event.NodeID]
		countEvent(&counts, event, labels)
		eventsByNode[*event.NodeID] = counts
	}

	var totalScore, totalAvailability, totalCoverage float64
	for _, sample := range samples {
		counts := eventsByNode[sample.Node.ID]
		node := buildReliabilityNode(sample, counts, now)
		report.Nodes = append(report.Nodes, node)
		totalScore += node.OperationalScore
		totalAvailability += node.AvailabilityPercent
		totalCoverage += node.DataCoveragePercent
	}

	if len(report.Nodes) > 0 {
		report.FleetOperationalScore = totalScore / float64(len(report.Nodes))
		report.FleetAvailability = totalAvailability / float64(len(report.Nodes))
		report.FleetDataCoverage = totalCoverage / float64(len(report.Nodes))
	}

	slices.SortFunc(report.Nodes, func(a, b ReliabilityNode) int {
		if a.OperationalScore != b.OperationalScore {
			if a.OperationalScore < b.OperationalScore {
				return -1
			}
			return 1
		}
		if a.CriticalEventCount != b.CriticalEventCount {
			return b.CriticalEventCount - a.CriticalEventCount
		}
		if a.WarningEventCount != b.WarningEventCount {
			return b.WarningEventCount - a.WarningEventCount
		}
		return strings.Compare(a.NodeName, b.NodeName)
	})

	report.Summary = buildReliabilitySummary(report)
	return report
}

func buildReliabilityNode(sample FleetNodeSample, counts reliabilityEventCounts, now int64) ReliabilityNode {
	lastSeenAge := int64(-1)
	if sample.Node.LastSeen != nil && now > 0 {
		lastSeenAge = now - *sample.Node.LastSeen
		if lastSeenAge < 0 {
			lastSeenAge = 0
		}
	}

	availability := estimateAvailability(sample)
	coverage := clampPercent(sample.Analytics.CoveragePercent)
	eventHealth := math.Max(0, 100-float64(counts.critical)*18-float64(counts.warning)*8-float64(counts.signals)*2)
	stability := estimateStability(sample)
	stalePenalty := stalePenalty(lastSeenAge)

	score := 0.35*availability + 0.25*coverage + 0.25*stability + 0.15*eventHealth - stalePenalty
	node := ReliabilityNode{
		NodeID:               sample.Node.ID,
		NodeName:             sample.Node.Name,
		Status:               sample.Node.Status,
		OperationalScore:     clampPercent(score),
		AvailabilityPercent:  availability,
		DataCoveragePercent:  coverage,
		LastSeenAgeSeconds:   lastSeenAge,
		IncidentCount:        counts.incidents,
		SignalEventCount:     counts.signals,
		ExperimentEventCount: counts.experiment,
		CriticalEventCount:   counts.critical,
		WarningEventCount:    counts.warning,
		DataQuality:          classifyDataQuality(coverage, lastSeenAge),
		Signals:              buildReliabilitySignals(sample, counts, coverage, lastSeenAge),
	}
	node.Recommendation = reliabilityRecommendation(sample, node)
	return node
}

func estimateAvailability(sample FleetNodeSample) float64 {
	if sample.Score != nil && sample.Score.Availability > 0 {
		return clampPercent(sample.Score.Availability)
	}

	switch sample.Node.Status {
	case "online":
		return 100
	case "degraded":
		return 72
	case "offline":
		return 0
	default:
		return 50
	}
}

func estimateStability(sample FleetNodeSample) float64 {
	if sample.Score != nil && sample.Score.Stability > 0 {
		return clampPercent(sample.Score.Stability)
	}

	stability := 100.0
	switch sample.Analytics.RiskLevel {
	case "critical":
		stability -= 35
	case "elevated":
		stability -= 14
	}
	if sample.Analytics.CPU.Volatility == "high" {
		stability -= 10
	}
	if sample.Analytics.Memory.Volatility == "high" {
		stability -= 10
	}
	if sample.Analytics.Connections.Volatility == "high" {
		stability -= 8
	}
	return clampPercent(stability)
}

func classifyDataQuality(coverage float64, lastSeenAge int64) string {
	switch {
	case coverage >= 80 && (lastSeenAge < 0 || lastSeenAge <= 120):
		return "good"
	case coverage >= 50 && (lastSeenAge < 0 || lastSeenAge <= 600):
		return "partial"
	default:
		return "weak"
	}
}

func buildReliabilitySignals(sample FleetNodeSample, counts reliabilityEventCounts, coverage float64, lastSeenAge int64) []string {
	signals := []string{}
	if counts.critical > 0 {
		signals = append(signals, fmt.Sprintf("%d critical operational event(s) in window", counts.critical))
	}
	if counts.warning > 0 {
		signals = append(signals, fmt.Sprintf("%d warning operational event(s) in window", counts.warning))
	}
	if counts.signals > 0 {
		signals = append(signals, fmt.Sprintf("%d statistical signal(s) outside experiments", counts.signals))
	}
	if counts.experiment > 0 {
		signals = append(signals, fmt.Sprintf("%d signal(s) inside labelled experiments", counts.experiment))
	}
	if coverage < 80 {
		signals = append(signals, fmt.Sprintf("%.0f%% metric coverage", coverage))
	}
	if lastSeenAge > 120 {
		signals = append(signals, fmt.Sprintf("last report %.0f minutes ago", float64(lastSeenAge)/60))
	}
	if sample.Analytics.RiskLevel == "critical" || sample.Analytics.RiskLevel == "elevated" {
		signals = append(signals, fmt.Sprintf("%s statistical risk", sample.Analytics.RiskLevel))
	}
	if len(signals) == 0 {
		signals = append(signals, "healthy telemetry and low event pressure")
	}
	return signals
}

func reliabilityRecommendation(sample FleetNodeSample, node ReliabilityNode) string {
	if sample.Node.Status == "offline" {
		return "Restore agent connectivity before interpreting performance analytics."
	}
	if node.CriticalEventCount > 0 {
		return "Inspect critical status events and correlate them with CPU, memory, and link history."
	}
	if node.DataQuality == "weak" {
		return "Improve sample coverage; check agent uptime, report interval, and server reachability."
	}
	if node.SignalEventCount > 0 {
		return "Review statistical signals; confirm whether they are real incidents or normal workload changes."
	}
	if sample.Analytics.RiskLevel == "critical" || sample.Analytics.RiskLevel == "elevated" {
		return "Watch this node and compare current pressure against its 24h baseline."
	}
	if sample.Node.LocationSource != nil && *sample.Node.LocationSource == "geoip" {
		return "Optional: pin exact coordinates in node-locations.yaml for a more accurate map."
	}
	return "No immediate action; keep collecting baseline data."
}

func buildReliabilitySummary(report ReliabilityAnalytics) string {
	if len(report.Nodes) == 0 {
		return "No reliability analytics available yet."
	}
	weak := 0
	for _, node := range report.Nodes {
		if node.DataQuality == "weak" {
			weak++
		}
	}
	return fmt.Sprintf(
		"%dh reliability ledger: %.0f/100 fleet score, %.0f%% availability proxy, %.0f%% data coverage, %d operational incident(s), %d statistical signal(s), %d experiment signal(s), %d weak telemetry node(s)",
		report.WindowHours,
		report.FleetOperationalScore,
		report.FleetAvailability,
		report.FleetDataCoverage,
		report.IncidentCount,
		report.SignalEventCount,
		report.ExperimentEventCount,
		weak,
	)
}

func countFleetEvent(report *ReliabilityAnalytics, event db.Event, labels []ExperimentLabel) {
	counts := reliabilityEventCounts{}
	countEvent(&counts, event, labels)
	report.IncidentCount += counts.incidents
	report.SignalEventCount += counts.signals
	report.ExperimentEventCount += counts.experiment
	report.CriticalEventCount += counts.critical
	report.WarningEventCount += counts.warning
}

func countEvent(counts *reliabilityEventCounts, event db.Event, labels []ExperimentLabel) {
	if isInsideLabelledExperiment(event, labels) {
		if isReliabilitySignal(event) {
			counts.experiment++
		}
		return
	}

	if event.Type == "anomaly" {
		if event.Severity == "critical" || event.Severity == "warning" {
			counts.signals++
		}
		return
	}

	if event.Type != "status_change" {
		return
	}
	if event.Severity == "critical" {
		counts.incidents++
		counts.critical++
		return
	}
	if event.Severity == "warning" {
		counts.incidents++
		counts.warning++
	}
}

func isReliabilitySignal(event db.Event) bool {
	if event.Type == "anomaly" || event.Type == "status_change" {
		return event.Severity == "critical" || event.Severity == "warning"
	}
	return false
}

func isInsideLabelledExperiment(event db.Event, labels []ExperimentLabel) bool {
	if event.NodeID == nil || len(labels) == 0 {
		return false
	}
	for _, label := range labels {
		if label.NodeID != *event.NodeID {
			continue
		}
		if event.CreatedAt >= label.StartedAt && event.CreatedAt <= label.EndedAt+300 {
			return true
		}
	}
	return false
}

func stalePenalty(lastSeenAge int64) float64 {
	switch {
	case lastSeenAge < 0:
		return 20
	case lastSeenAge > 600:
		return 25
	case lastSeenAge > 120:
		return 10
	default:
		return 0
	}
}

func clampPercent(value float64) float64 {
	switch {
	case math.IsNaN(value) || math.IsInf(value, 0):
		return 0
	case value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return value
	}
}
