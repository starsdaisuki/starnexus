package analytics

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// ScoreReport holds the daily scoring results for all nodes.
type ScoreReport struct {
	Scores []NodeScoreResult
}

type NodeScoreResult struct {
	NodeID       string
	NodeName     string
	Availability float64 // 0-100%
	Latency      float64 // avg ms (-1 if no data)
	Stability    float64 // 0-100 (100 = perfectly stable)
	Composite    float64 // 0-100 weighted score
}

// FormatReport creates a Telegram-friendly daily report string.
func (r *ScoreReport) FormatReport() string {
	if len(r.Scores) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\xf0\x9f\x93\x8a <b>Daily Node Report</b>\n\n")

	for _, s := range r.Scores {
		icon := "\xf0\x9f\x9f\xa2"
		if s.Composite < 70 {
			icon = "\xf0\x9f\x94\xb4"
		} else if s.Composite < 90 {
			icon = "\xf0\x9f\x9f\xa1"
		}

		latStr := "N/A"
		if s.Latency >= 0 {
			latStr = fmt.Sprintf("%.1fms", s.Latency)
		}

		sb.WriteString(fmt.Sprintf(
			"%s <b>%s</b>\n   Avail: %.1f%% | Latency: %s | Stability: %.0f | Score: <b>%.0f</b>/100\n\n",
			icon, s.NodeName, s.Availability, latStr, s.Stability, s.Composite,
		))
	}

	return sb.String()
}

// RunScoring recalculates node scores based on last 30 days of data.
func RunScoring(database *db.DB) *ScoreReport {
	nodeIDs, err := database.GetNodeIDs()
	if err != nil {
		log.Printf("[analytics] Failed to get node IDs for scoring: %v", err)
		return nil
	}

	now := time.Now().Unix()
	thirtyDaysAgo := now - 30*86400
	totalSeconds := float64(now - thirtyDaysAgo)

	report := &ScoreReport{}

	for _, nodeID := range nodeIDs {
		nodeName := database.GetNodeName(nodeID)
		result := NodeScoreResult{
			NodeID:   nodeID,
			NodeName: nodeName,
			Latency:  -1,
		}

		// Availability: % of time online
		onlineSec, err := database.GetOnlineSeconds(nodeID, thirtyDaysAgo, now)
		if err == nil && totalSeconds > 0 {
			result.Availability = math.Min(100, float64(onlineSec)/totalSeconds*100)
		}

		// Latency: avg link latency
		avgLat, err := database.GetAvgLinkLatency(nodeID)
		if err == nil {
			result.Latency = avgLat
		}

		// Stability: based on CPU/memory stddev over last 7 days
		sevenDaysAgo := now - 7*86400
		metrics, err := database.GetRawMetrics(nodeID, sevenDaysAgo, now)
		if err == nil && len(metrics) > 10 {
			cpuStddev := calcStddev(metrics, func(m db.RawMetric) float64 { return m.CPUPercent })
			memStddev := calcStddev(metrics, func(m db.RawMetric) float64 { return m.MemoryPercent })
			// Convert stddev to 0-100 score (lower stddev = higher score)
			// stddev of 0 → 100, stddev of 50 → 0
			avgStddev := (cpuStddev + memStddev) / 2
			result.Stability = math.Max(0, 100-avgStddev*2)
		} else {
			result.Stability = 50 // default if no data
		}

		// Composite: availability 40%, latency 30%, stability 30%
		latencyScore := 100.0
		if result.Latency >= 0 {
			// 0ms → 100, 200ms → 0
			latencyScore = math.Max(0, 100-result.Latency*0.5)
		}
		result.Composite = result.Availability*0.4 + latencyScore*0.3 + result.Stability*0.3

		// Save to DB
		if err := database.UpsertNodeScore(nodeID, result.Availability, latencyScore, result.Stability, result.Composite); err != nil {
			log.Printf("[analytics] Failed to save score for %s: %v", nodeID, err)
		}

		report.Scores = append(report.Scores, result)
	}

	log.Printf("[analytics] Scoring complete for %d nodes", len(report.Scores))
	return report
}

func calcStddev(metrics []db.RawMetric, extract func(db.RawMetric) float64) float64 {
	n := float64(len(metrics))
	if n < 2 {
		return 0
	}

	var sum, sumSq float64
	for _, m := range metrics {
		v := extract(m)
		sum += v
		sumSq += v * v
	}

	mean := sum / n
	variance := sumSq/n - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}
