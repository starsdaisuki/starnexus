package analytics

import (
	"testing"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func TestBuildDetailAnalyticsClassifiesRisingPressure(t *testing.T) {
	points := []db.MetricPoint{
		{Timestamp: 0, CPUPercent: 20, MemoryPercent: 30, BandwidthDown: 50, Connections: 100},
		{Timestamp: 3600, CPUPercent: 28, MemoryPercent: 35, BandwidthDown: 60, Connections: 120},
		{Timestamp: 7200, CPUPercent: 42, MemoryPercent: 46, BandwidthDown: 75, Connections: 150},
		{Timestamp: 10800, CPUPercent: 64, MemoryPercent: 62, BandwidthDown: 90, Connections: 190},
		{Timestamp: 14400, CPUPercent: 88, MemoryPercent: 84, BandwidthDown: 115, Connections: 260},
	}

	analytics := BuildDetailAnalytics(points, 6)
	if analytics.RiskLevel != "critical" {
		t.Fatalf("expected critical risk level, got %q", analytics.RiskLevel)
	}
	if analytics.CPU.Trend != "rising" {
		t.Fatalf("expected rising CPU trend, got %q", analytics.CPU.Trend)
	}
	if len(analytics.Highlights) == 0 {
		t.Fatal("expected at least one highlight")
	}
}

func TestBuildDetailAnalyticsDetectsBaselineShift(t *testing.T) {
	points := make([]db.MetricPoint, 0, 24)
	for i := 0; i < 18; i++ {
		points = append(points, db.MetricPoint{
			Timestamp:     int64(i * 1800),
			CPUPercent:    20 + float64(i%2),
			MemoryPercent: 35 + float64(i%3),
			BandwidthDown: 80,
			Connections:   120,
		})
	}
	for i := 18; i < 24; i++ {
		points = append(points, db.MetricPoint{
			Timestamp:     int64(i * 1800),
			CPUPercent:    72 + float64(i-18),
			MemoryPercent: 78 + float64(i-18),
			BandwidthDown: 180,
			Connections:   260,
		})
	}

	analytics := BuildDetailAnalytics(points, 12)
	if !analytics.CPU.Shift.Significant {
		t.Fatalf("expected CPU baseline shift to be significant: %#v", analytics.CPU.Shift)
	}
	if analytics.CPU.Shift.ShiftScore <= 0 {
		t.Fatalf("expected positive CPU shift score, got %.2f", analytics.CPU.Shift.ShiftScore)
	}
	if analytics.RiskLevel == "stable" {
		t.Fatalf("expected non-stable risk level, got %q", analytics.RiskLevel)
	}
}
