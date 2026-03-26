package analytics

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// GenerateDailyReport creates a full daily report with metrics summary,
// anomalies, status changes, scores, and optional AI analysis.
func GenerateDailyReport(database *db.DB, mistralKey string) string {
	now := time.Now().Unix()
	dayAgo := now - 86400

	nodeIDs, err := database.GetNodeIDs()
	if err != nil {
		log.Printf("[report] Failed to get nodes: %v", err)
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\xf0\x9f\x93\x8a <b>StarNexus Daily Report</b>\n")
	sb.WriteString(fmt.Sprintf("<i>%s</i>\n\n", time.Now().UTC().Format("2006-01-02 15:04 UTC")))

	// --- Node metrics summary ---
	var aiContext strings.Builder
	aiContext.WriteString("24-hour VPS monitoring data:\n\n")

	allNodes, _ := database.GetAllNodes()
	nodeMap := make(map[string]db.Node)
	for _, n := range allNodes {
		nodeMap[n.ID] = n
	}

	for _, nodeID := range nodeIDs {
		node := nodeMap[nodeID]
		nodeName := node.Name
		if nodeName == "" {
			nodeName = nodeID
		}

		// Status icon based on current live status
		icon := statusIcon(node.Status)

		metrics, err := database.GetRawMetrics(nodeID, dayAgo, now)
		if err != nil || len(metrics) == 0 {
			sb.WriteString(fmt.Sprintf("%s <b>%s</b> — no metrics data\n\n", icon, nodeName))
			aiContext.WriteString(fmt.Sprintf("Node: %s — status: %s, no raw metrics in last 24h\n\n", nodeName, node.Status))
			continue
		}

		// Calculate stats
		var cpuSum, cpuMax, memSum, memMax, bwSum float64
		for _, m := range metrics {
			cpuSum += m.CPUPercent
			memSum += m.MemoryPercent
			bwSum += m.BandwidthDown
			if m.CPUPercent > cpuMax {
				cpuMax = m.CPUPercent
			}
			if m.MemoryPercent > memMax {
				memMax = m.MemoryPercent
			}
		}
		n := float64(len(metrics))
		cpuAvg := cpuSum / n
		memAvg := memSum / n
		bwAvg := bwSum / n

		// Uptime: use actual time span covered by data, not fixed 24h
		// This avoids showing low uptime when raw collection just started
		firstSample := metrics[0].CreatedAt
		lastSample := metrics[len(metrics)-1].CreatedAt
		dataSpan := float64(lastSample - firstSample)
		if dataSpan < 60 {
			dataSpan = 60 // avoid division by tiny numbers
		}
		reportedSeconds := n * 30.0
		uptimeRatio := reportedSeconds / dataSpan * 100
		if uptimeRatio > 100 {
			uptimeRatio = 100
		}

		sb.WriteString(fmt.Sprintf(
			"%s <b>%s</b>\n   CPU: avg %.1f%% / max %.1f%%\n   Mem: avg %.1f%% / max %.1f%%\n   BW: avg %.1f KB/s\n   Uptime: %.1f%% (%d samples)\n\n",
			icon, nodeName, cpuAvg, cpuMax, memAvg, memMax, bwAvg, uptimeRatio, len(metrics),
		))

		aiContext.WriteString(fmt.Sprintf(
			"Node: %s (status: %s)\n  CPU: avg=%.1f%% max=%.1f%%\n  Memory: avg=%.1f%% max=%.1f%%\n  Bandwidth: avg=%.1f KB/s\n  Uptime: %.1f%% (%d samples)\n\n",
			nodeName, node.Status, cpuAvg, cpuMax, memAvg, memMax, bwAvg, uptimeRatio, len(metrics),
		))
	}

	// --- Recent status changes ---
	for _, nodeID := range nodeIDs {
		history, err := database.GetHistory(nodeID)
		if err != nil || len(history) == 0 {
			continue
		}
		for _, h := range history {
			if h.CreatedAt < dayAgo {
				break
			}
			reason := ""
			if h.Reason != nil {
				reason = *h.Reason
			}
			aiContext.WriteString(fmt.Sprintf("Event: %s status %s→%s (%s)\n", nodeID, ptrStr(h.OldStatus), h.NewStatus, reason))
		}
	}

	// --- Links ---
	links, err := database.GetAllLinks()
	if err == nil && len(links) > 0 {
		sb.WriteString("<b>Links</b>\n")
		for _, l := range links {
			latStr := "N/A"
			if l.LatencyMs >= 0 {
				latStr = fmt.Sprintf("%.1fms", l.LatencyMs)
			}
			sb.WriteString(fmt.Sprintf("   %s → %s: %s (loss %.1f%%)\n", l.SourceNodeID, l.TargetNodeID, latStr, l.PacketLoss))
			aiContext.WriteString(fmt.Sprintf("Link: %s→%s latency=%s loss=%.1f%%\n", l.SourceNodeID, l.TargetNodeID, latStr, l.PacketLoss))
		}
		sb.WriteString("\n")
	}

	// --- Node scores ---
	scores, err := database.GetAllScores()
	if err == nil && len(scores) > 0 {
		sb.WriteString("<b>Scores</b>\n")
		for _, s := range scores {
			sb.WriteString(fmt.Sprintf("   %s: %.0f/100 (avail %.0f%%, stab %.0f)\n",
				database.GetNodeName(s.NodeID), s.CompositeScore, s.Availability, s.Stability))
		}
		sb.WriteString("\n")
	}

	// --- AI Analysis ---
	if mistralKey != "" {
		log.Println("[report] Requesting Mistral AI analysis...")
		aiContext.WriteString("\nProvide insights on these metrics: trends, warnings, anomalies, and recommendations.")

		analysis, err := CallMistral(mistralKey, aiContext.String())
		if err != nil {
			log.Printf("[report] Mistral API error: %v", err)
			sb.WriteString("<i>AI analysis unavailable</i>\n")
		} else {
			sb.WriteString("\xf0\x9f\xa4\x96 <b>AI Analysis</b>\n")
			sb.WriteString(analysis)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func statusIcon(status string) string {
	switch status {
	case "online":
		return "\xf0\x9f\x9f\xa2"
	case "degraded":
		return "\xf0\x9f\x9f\xa1"
	case "offline":
		return "\xf0\x9f\x94\xb4"
	default:
		return "\xe2\xac\x9c"
	}
}

func ptrStr(s *string) string {
	if s == nil {
		return "none"
	}
	return *s
}
