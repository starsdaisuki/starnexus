package analytics

import (
	"fmt"
	"slices"
	"strings"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type FleetNodeSample struct {
	Node      db.Node
	Score     *db.NodeScore
	Analytics DetailAnalytics
}

type FleetNodeInsight struct {
	NodeID         string   `json:"node_id"`
	NodeName       string   `json:"node_name"`
	RiskLevel      string   `json:"risk_level"`
	CompositeScore float64  `json:"composite_score,omitempty"`
	Coverage       float64  `json:"coverage_percent"`
	SignalCount    int      `json:"signal_count"`
	Summary        string   `json:"summary"`
	Highlights     []string `json:"highlights"`
}

type FleetAnalytics struct {
	WindowHours  int                `json:"window_hours"`
	Critical     int                `json:"critical"`
	Elevated     int                `json:"elevated"`
	Stable       int                `json:"stable"`
	Summary      string             `json:"summary"`
	NodeInsights []FleetNodeInsight `json:"node_insights"`
}

func BuildFleetAnalytics(windowHours int, samples []FleetNodeSample) FleetAnalytics {
	fleet := FleetAnalytics{
		WindowHours:  windowHours,
		NodeInsights: make([]FleetNodeInsight, 0, len(samples)),
	}

	for _, sample := range samples {
		insight := FleetNodeInsight{
			NodeID:      sample.Node.ID,
			NodeName:    sample.Node.Name,
			RiskLevel:   sample.Analytics.RiskLevel,
			Coverage:    sample.Analytics.CoveragePercent,
			SignalCount: countSignals(sample.Analytics),
			Summary:     sample.Analytics.Summary,
			Highlights:  slices.Clone(sample.Analytics.Highlights),
		}
		if sample.Score != nil {
			insight.CompositeScore = sample.Score.CompositeScore
		}

		switch sample.Analytics.RiskLevel {
		case "critical":
			fleet.Critical++
		case "elevated":
			fleet.Elevated++
		default:
			fleet.Stable++
		}
		fleet.NodeInsights = append(fleet.NodeInsights, insight)
	}

	slices.SortFunc(fleet.NodeInsights, func(a, b FleetNodeInsight) int {
		if riskRank(a.RiskLevel) != riskRank(b.RiskLevel) {
			return riskRank(a.RiskLevel) - riskRank(b.RiskLevel)
		}
		if a.SignalCount != b.SignalCount {
			return b.SignalCount - a.SignalCount
		}
		if a.CompositeScore != b.CompositeScore {
			if a.CompositeScore < b.CompositeScore {
				return -1
			}
			return 1
		}
		return strings.Compare(a.NodeName, b.NodeName)
	})

	fleet.Summary = buildFleetSummary(fleet)
	return fleet
}

func countSignals(analytics DetailAnalytics) int {
	count := 0
	metrics := []MetricAnalysis{analytics.CPU, analytics.Memory, analytics.BandwidthDown, analytics.Connections}
	for _, metric := range metrics {
		if metric.Outlier {
			count++
		}
		if metric.Shift.Significant {
			count++
		}
		if metric.Trend == "rising" {
			count++
		}
		if metric.Volatility == "high" {
			count++
		}
	}
	return count
}

func buildFleetSummary(fleet FleetAnalytics) string {
	total := len(fleet.NodeInsights)
	if total == 0 {
		return "No fleet analytics available yet."
	}
	parts := []string{
		fmt.Sprintf("%dh radar across %d nodes", fleet.WindowHours, total),
		fmt.Sprintf("%d critical", fleet.Critical),
		fmt.Sprintf("%d elevated", fleet.Elevated),
		fmt.Sprintf("%d stable", fleet.Stable),
	}
	if total > 0 {
		top := fleet.NodeInsights[0]
		parts = append(parts, fmt.Sprintf("top watch item: %s (%s)", top.NodeName, top.RiskLevel))
	}
	return strings.Join(parts, " • ")
}

func riskRank(level string) int {
	switch level {
	case "critical":
		return 0
	case "elevated":
		return 1
	case "stable":
		return 2
	default:
		return 3
	}
}
