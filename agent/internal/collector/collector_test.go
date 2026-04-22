package collector

import (
	"math"
	"testing"
)

func TestCalculateCPUPercentTreatsIOWaitAsIdle(t *testing.T) {
	before := &cpuSample{idle: 100, total: 200}
	after := &cpuSample{idle: 180, total: 300}

	got := calculateCPUPercent(before, after)
	if math.Abs(got-20) > 0.001 {
		t.Fatalf("expected 20%% usage, got %.2f", got)
	}
}

func TestCalculateCPUPercentClampsInvalidDeltas(t *testing.T) {
	before := &cpuSample{idle: 100, total: 200}
	after := &cpuSample{idle: 90, total: 190}

	if got := calculateCPUPercent(before, after); got != 0 {
		t.Fatalf("expected invalid delta to clamp to 0, got %.2f", got)
	}
}

func TestMedian3SuppressesSingleIntervalSpike(t *testing.T) {
	if got := median3(1, 100, 2); got != 2 {
		t.Fatalf("expected median 2, got %.2f", got)
	}
}
