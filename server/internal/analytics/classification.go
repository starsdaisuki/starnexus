package analytics

import (
	"strings"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type EventClassification struct {
	EventID     int64   `json:"event_id"`
	NodeID      string  `json:"node_id,omitempty"`
	NodeName    string  `json:"node_name,omitempty"`
	EventType   string  `json:"event_type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Category    string  `json:"category"`
	Metric      string  `json:"metric,omitempty"`
	LikelyCause string  `json:"likely_cause"`
	Confidence  float64 `json:"confidence"`
	Evidence    string  `json:"evidence"`
	CreatedAt   int64   `json:"created_at"`
}

func BuildEventClassifications(events []db.Event) []EventClassification {
	results := make([]EventClassification, 0, len(events))
	for _, event := range events {
		results = append(results, ClassifyEvent(event))
	}
	return results
}

func ClassifyEvent(event db.Event) EventClassification {
	text := strings.ToLower(event.Title)
	body := ""
	if event.Body != nil {
		body = strings.ToLower(*event.Body)
		text += " " + body
	}

	result := EventClassification{
		EventID:     event.ID,
		EventType:   event.Type,
		Severity:    event.Severity,
		Title:       event.Title,
		Category:    "unknown",
		LikelyCause: "uncategorized operational signal",
		Confidence:  0.2,
		Evidence:    shortEvidence(event.Title, body),
		CreatedAt:   event.CreatedAt,
	}
	if event.NodeID != nil {
		result.NodeID = *event.NodeID
	}
	if event.NodeName != nil {
		result.NodeName = *event.NodeName
	}

	switch {
	case strings.Contains(text, "recover") || strings.Contains(text, "healthy"):
		result.Category = "recovery"
		result.LikelyCause = "system returned to normal operating range"
		result.Confidence = 0.85
	case strings.Contains(text, "cpu"):
		result.Category = "resource_pressure"
		result.Metric = "cpu_percent"
		result.LikelyCause = "compute saturation, build job, stress test, or CPU-heavy process"
		result.Confidence = 0.8
	case strings.Contains(text, "memory") || strings.Contains(text, "mem"):
		result.Category = "resource_pressure"
		result.Metric = "memory_percent"
		result.LikelyCause = "memory pressure or resident process growth"
		result.Confidence = 0.8
	case strings.Contains(text, "bandwidth down"):
		result.Category = "network_traffic"
		result.Metric = "bandwidth_down"
		result.LikelyCause = "download spike, backup transfer, proxy traffic, or package fetch"
		result.Confidence = 0.8
	case strings.Contains(text, "bandwidth up"):
		result.Category = "network_traffic"
		result.Metric = "bandwidth_up"
		result.LikelyCause = "upload spike, backup transfer, proxy traffic, or log export"
		result.Confidence = 0.8
	case strings.Contains(text, "connection"):
		result.Category = "network_traffic"
		result.Metric = "connections"
		result.LikelyCause = "connection burst, scan, proxy fan-out, or client traffic surge"
		result.Confidence = 0.75
	case strings.Contains(text, "offline") || strings.Contains(text, "no report"):
		result.Category = "reachability"
		result.LikelyCause = "agent stopped, node unreachable, primary unreachable, or network path failure"
		result.Confidence = 0.75
	case strings.Contains(text, "degraded") || strings.Contains(text, "high resource"):
		result.Category = "resource_pressure"
		result.LikelyCause = "basic CPU or memory threshold crossed"
		result.Confidence = 0.65
	}

	return result
}

func shortEvidence(title, body string) string {
	if body == "" {
		return title
	}
	evidence := title + ": " + body
	if len(evidence) > 180 {
		return evidence[:177] + "..."
	}
	return evidence
}
