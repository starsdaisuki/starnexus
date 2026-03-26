package analytics

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

const (
	zThreshold    = 3.0
	minDataPoints = 100 // ~50 minutes at 30s interval
)

// AnomalyAlert represents a detected anomaly to be sent as an alert.
type AnomalyAlert struct {
	NodeID   string
	NodeName string
	Metric   string
	Value    float64
	ZScore   float64
	Mean     float64
	Stddev   float64
}

func (a AnomalyAlert) String() string {
	low := a.Mean - 2*a.Stddev
	high := a.Mean + 2*a.Stddev
	if low < 0 {
		low = 0
	}
	return fmt.Sprintf(
		"\xe2\x9a\xa0\xef\xb8\x8f <b>%s</b>: %s anomaly detected (%.1f%%, Z=%.1f, normal range %.0f-%.0f%%)",
		a.NodeName, a.Metric, a.Value, a.ZScore, low, high,
	)
}

// RunAnomalyDetection checks latest metrics against 24h rolling Z-scores.
// Returns any anomalies detected.
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
		// Check minimum data points
		count, err := database.GetRawMetricCount(nodeID, dayAgo)
		if err != nil || count < minDataPoints {
			continue
		}

		metrics, err := database.GetRawMetrics(nodeID, dayAgo, now)
		if err != nil || len(metrics) < minDataPoints {
			continue
		}

		latest := metrics[len(metrics)-1]
		nodeName := database.GetNodeName(nodeID)

		// Check CPU
		if a := checkMetric(metrics, "CPU", func(m db.RawMetric) float64 { return m.CPUPercent }, latest.CPUPercent, nodeID, nodeName); a != nil {
			database.RecordStatusChange(nodeID, "", "anomaly", a.String())
			alerts = append(alerts, *a)
		}

		// Check Memory
		if a := checkMetric(metrics, "Memory", func(m db.RawMetric) float64 { return m.MemoryPercent }, latest.MemoryPercent, nodeID, nodeName); a != nil {
			database.RecordStatusChange(nodeID, "", "anomaly", a.String())
			alerts = append(alerts, *a)
		}

		// Check Bandwidth
		if a := checkMetric(metrics, "Bandwidth", func(m db.RawMetric) float64 { return m.BandwidthDown }, latest.BandwidthDown, nodeID, nodeName); a != nil {
			database.RecordStatusChange(nodeID, "", "anomaly", a.String())
			alerts = append(alerts, *a)
		}
	}

	return alerts
}

func checkMetric(metrics []db.RawMetric, name string, extract func(db.RawMetric) float64, latest float64, nodeID, nodeName string) *AnomalyAlert {
	// Calculate mean and stddev over the window (excluding latest)
	var sum, sumSq float64
	n := float64(len(metrics) - 1)
	if n < 2 {
		return nil
	}

	for _, m := range metrics[:len(metrics)-1] {
		v := extract(m)
		sum += v
		sumSq += v * v
	}

	mean := sum / n
	variance := sumSq/n - mean*mean
	if variance < 0 {
		variance = 0
	}
	stddev := math.Sqrt(variance)

	if stddev < 0.01 {
		// Near-zero variance — data is flat, skip
		return nil
	}

	z := (latest - mean) / stddev
	if math.Abs(z) >= zThreshold {
		return &AnomalyAlert{
			NodeID:   nodeID,
			NodeName: nodeName,
			Metric:   name,
			Value:    latest,
			ZScore:   z,
			Mean:     mean,
			Stddev:   stddev,
		}
	}

	return nil
}
