package db

import "testing"

func TestDownsampleMetricPointsRetainsLastPoint(t *testing.T) {
	points := make([]MetricPoint, 0, 10)
	for i := 0; i < 10; i++ {
		points = append(points, MetricPoint{
			Timestamp: int64(i + 1),
		})
	}

	downsampled := DownsampleMetricPoints(points, 4)
	if len(downsampled) > 5 {
		t.Fatalf("expected downsampled size <= 5, got %d", len(downsampled))
	}

	if downsampled[len(downsampled)-1].Timestamp != 10 {
		t.Fatalf("expected final point to be preserved, got %d", downsampled[len(downsampled)-1].Timestamp)
	}
}
