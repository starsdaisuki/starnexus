package analytics

import (
	"fmt"
	"log"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

const (
	minDataPoints       = 100
	anomalyDedupSeconds = 6 * 3600
)

type anomalyPolicy struct {
	RobustZThreshold  float64
	ShiftScoreMinimum float64
	MinCurrent        float64
	MinRecentMedian   float64
	MinDelta          float64
	MinDeltaPercent   float64
}

type AnomalyAlert struct {
	NodeID    string
	NodeName  string
	Metric    string
	Kind      string
	Severity  string
	Title     string
	Message   string
	RiskLevel string
}

func (a AnomalyAlert) String() string {
	return fmt.Sprintf("⚠️ <b>%s</b>: %s", a.NodeName, a.Message)
}

func RunAnomalyDetection(database *db.DB) []AnomalyAlert {
	nodeIDs, err := database.GetNodeIDs()
	if err != nil {
		log.Printf("[analytics] Failed to get node IDs: %v", err)
		return nil
	}

	now := time.Now().Unix()
	dayAgo := now - 86400
	var alerts []AnomalyAlert

	for _, nodeID := range nodeIDs {
		count, err := database.GetRawMetricCount(nodeID, dayAgo)
		if err != nil || count < minDataPoints {
			continue
		}

		points, err := database.GetMetricPoints(nodeID, dayAgo, now)
		if err != nil || len(points) < minDataPoints {
			continue
		}

		nodeName := database.GetNodeName(nodeID)
		detail := BuildDetailAnalytics(points, 24)
		nodeAlerts := buildNodeAlerts(nodeID, nodeName, detail)
		for _, alert := range nodeAlerts {
			hasRecent, err := database.HasRecentEvent(nodeID, "anomaly", alert.Title, anomalyDedupSeconds)
			if err != nil {
				log.Printf("[analytics] recent event lookup failed for %s: %v", nodeID, err)
				continue
			}
			if hasRecent {
				continue
			}
			_ = database.RecordEvent(nodeID, "anomaly", alert.Severity, alert.Title, alert.Message, "")
			alerts = append(alerts, alert)
		}
	}

	return alerts
}

func buildNodeAlerts(nodeID, nodeName string, detail DetailAnalytics) []AnomalyAlert {
	var alerts []AnomalyAlert
	for _, metric := range []MetricAnalysis{detail.CPU, detail.Memory, detail.BandwidthDown, detail.Connections} {
		if shouldAlertOutlier(metric) {
			alerts = append(alerts, AnomalyAlert{
				NodeID:    nodeID,
				NodeName:  nodeName,
				Metric:    metric.Label,
				Kind:      "outlier",
				Severity:  severityForMetric(metric, detail.RiskLevel),
				Title:     fmt.Sprintf("%s outlier detected", metric.Label),
				Message:   formatOutlierMessage(metric, detail.RiskLevel),
				RiskLevel: detail.RiskLevel,
			})
		}
		if shouldAlertShift(metric) {
			alerts = append(alerts, AnomalyAlert{
				NodeID:    nodeID,
				NodeName:  nodeName,
				Metric:    metric.Label,
				Kind:      "baseline_shift",
				Severity:  severityForMetric(metric, detail.RiskLevel),
				Title:     fmt.Sprintf("%s baseline shift detected", metric.Label),
				Message:   formatShiftMessage(metric, detail.RiskLevel),
				RiskLevel: detail.RiskLevel,
			})
		}
	}
	return alerts
}

func severityForMetric(metric MetricAnalysis, riskLevel string) string {
	switch metric.Label {
	case "CPU":
		if metric.Current >= 90 || metric.Shift.RecentMedian >= 85 || riskLevel == "critical" {
			return "critical"
		}
	case "Memory":
		if metric.Current >= 92 || metric.Shift.RecentMedian >= 90 || riskLevel == "critical" {
			return "critical"
		}
	case "Connections":
		if metric.Current >= 500 || metric.Shift.RecentMedian >= 500 {
			return "critical"
		}
	case "Bandwidth Down":
		if metric.Current >= 10240 || metric.Shift.RecentMedian >= 10240 {
			return "critical"
		}
	}
	return "warning"
}

func shouldAlertOutlier(metric MetricAnalysis) bool {
	policy := policyForMetric(metric.Label)
	if !metric.Outlier {
		return false
	}
	if metric.RobustZ < policy.RobustZThreshold {
		return false
	}
	if metric.Current < policy.MinCurrent {
		return false
	}
	if metric.Current-metric.Median < policy.MinDelta {
		return false
	}
	return true
}

func shouldAlertShift(metric MetricAnalysis) bool {
	policy := policyForMetric(metric.Label)
	if !metric.Shift.Significant {
		return false
	}
	if metric.Shift.ShiftScore < policy.ShiftScoreMinimum {
		return false
	}
	if metric.Shift.RecentMedian < policy.MinRecentMedian {
		return false
	}
	if metric.Shift.RecentMedian-metric.Shift.BaselineMedian < policy.MinDelta {
		return false
	}
	if metric.Shift.DeltaPercent < policy.MinDeltaPercent {
		return false
	}
	return true
}

func policyForMetric(label string) anomalyPolicy {
	switch label {
	case "CPU":
		return anomalyPolicy{
			RobustZThreshold:  5.5,
			ShiftScoreMinimum: 4,
			MinCurrent:        70,
			MinRecentMedian:   55,
			MinDelta:          15,
			MinDeltaPercent:   30,
		}
	case "Memory":
		return anomalyPolicy{
			RobustZThreshold:  5,
			ShiftScoreMinimum: 4,
			MinCurrent:        80,
			MinRecentMedian:   75,
			MinDelta:          8,
			MinDeltaPercent:   15,
		}
	case "Connections":
		return anomalyPolicy{
			RobustZThreshold:  6,
			ShiftScoreMinimum: 4.5,
			MinCurrent:        100,
			MinRecentMedian:   100,
			MinDelta:          50,
			MinDeltaPercent:   80,
		}
	case "Bandwidth Down":
		return anomalyPolicy{
			RobustZThreshold:  8,
			ShiftScoreMinimum: 5,
			MinCurrent:        1024,
			MinRecentMedian:   1024,
			MinDelta:          512,
			MinDeltaPercent:   100,
		}
	default:
		return anomalyPolicy{
			RobustZThreshold:  6,
			ShiftScoreMinimum: 5,
			MinCurrent:        1,
			MinRecentMedian:   1,
			MinDelta:          1,
			MinDeltaPercent:   50,
		}
	}
}

func formatOutlierMessage(metric MetricAnalysis, riskLevel string) string {
	return fmt.Sprintf(
		"%s pressure is above its robust baseline: current %.1f, median %.1f, robust z %.1f (%s risk).",
		metric.Label,
		metric.Current,
		metric.Median,
		metric.RobustZ,
		riskLevel,
	)
}

func formatShiftMessage(metric MetricAnalysis, riskLevel string) string {
	return fmt.Sprintf(
		"%s pressure shifted upward versus baseline: recent median %.1f vs baseline %.1f (%.0f%%, shift score %.1f, %s risk).",
		metric.Label,
		metric.Shift.RecentMedian,
		metric.Shift.BaselineMedian,
		metric.Shift.DeltaPercent,
		metric.Shift.ShiftScore,
		riskLevel,
	)
}
