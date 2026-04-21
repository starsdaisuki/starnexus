package analytics

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

const (
	minDataPoints       = 100
	anomalyDedupSeconds = 3 * 3600
)

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
		if metric.Outlier && math.Abs(metric.RobustZ) >= 4 {
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
		if metric.Shift.Significant && math.Abs(metric.Shift.ShiftScore) >= 3 {
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
	if riskLevel == "critical" || metric.Current >= 90 || math.Abs(metric.Shift.ShiftScore) >= 5 {
		return "critical"
	}
	return "warning"
}

func formatOutlierMessage(metric MetricAnalysis, riskLevel string) string {
	return fmt.Sprintf(
		"%s is outside its robust baseline: current %.1f, median %.1f, robust z %.1f (%s risk).",
		metric.Label,
		metric.Current,
		metric.Median,
		metric.RobustZ,
		riskLevel,
	)
}

func formatShiftMessage(metric MetricAnalysis, riskLevel string) string {
	return fmt.Sprintf(
		"%s shifted versus baseline: recent median %.1f vs baseline %.1f (%.0f%%, shift score %.1f, %s risk).",
		metric.Label,
		metric.Shift.RecentMedian,
		metric.Shift.BaselineMedian,
		metric.Shift.DeltaPercent,
		metric.Shift.ShiftScore,
		riskLevel,
	)
}
