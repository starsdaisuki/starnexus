package analytics

import "testing"

func TestBuildNodeAlertsIgnoresLowPressureOutliers(t *testing.T) {
	detail := DetailAnalytics{
		RiskLevel: "critical",
		CPU: MetricAnalysis{
			Label:   "CPU",
			Current: 9,
			Median:  1,
			RobustZ: 12,
			Outlier: true,
		},
	}

	alerts := buildNodeAlerts("node-a", "Node A", detail)
	if len(alerts) != 0 {
		t.Fatalf("expected no alert for low absolute CPU outlier, got %+v", alerts)
	}
}

func TestBuildNodeAlertsDetectsActionableCPUOutlier(t *testing.T) {
	detail := DetailAnalytics{
		RiskLevel: "critical",
		CPU: MetricAnalysis{
			Label:   "CPU",
			Current: 96,
			Median:  12,
			RobustZ: 9,
			Outlier: true,
		},
	}

	alerts := buildNodeAlerts("node-a", "Node A", detail)
	if len(alerts) != 1 {
		t.Fatalf("expected one alert, got %+v", alerts)
	}
	if alerts[0].Severity != "critical" {
		t.Fatalf("expected critical severity, got %s", alerts[0].Severity)
	}
}

func TestBuildNodeAlertsRequiresMeaningfulShift(t *testing.T) {
	detail := DetailAnalytics{
		RiskLevel: "elevated",
		Memory: MetricAnalysis{
			Label: "Memory",
			Shift: ShiftAnalysis{
				Significant:    true,
				ShiftScore:     6,
				BaselineMedian: 40,
				RecentMedian:   45,
				DeltaPercent:   12,
			},
		},
	}

	alerts := buildNodeAlerts("node-a", "Node A", detail)
	if len(alerts) != 0 {
		t.Fatalf("expected no alert for below-threshold memory shift, got %+v", alerts)
	}
}

func TestBuildNodeAlertsDetectsActionableMemoryShift(t *testing.T) {
	detail := DetailAnalytics{
		RiskLevel: "critical",
		Memory: MetricAnalysis{
			Label: "Memory",
			Shift: ShiftAnalysis{
				Significant:    true,
				ShiftScore:     5,
				BaselineMedian: 68,
				RecentMedian:   86,
				DeltaPercent:   26,
			},
		},
	}

	alerts := buildNodeAlerts("node-a", "Node A", detail)
	if len(alerts) != 1 {
		t.Fatalf("expected one alert, got %+v", alerts)
	}
	if alerts[0].Metric != "Memory" || alerts[0].Kind != "baseline_shift" {
		t.Fatalf("unexpected alert: %+v", alerts[0])
	}
}
