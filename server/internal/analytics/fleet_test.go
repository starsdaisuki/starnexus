package analytics

import (
	"testing"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func TestBuildFleetAnalyticsOrdersCriticalFirst(t *testing.T) {
	fleet := BuildFleetAnalytics(24, []FleetNodeSample{
		{
			Node: db.Node{ID: "stable-1", Name: "Stable Node"},
			Analytics: DetailAnalytics{
				RiskLevel:  "stable",
				Highlights: []string{"steady"},
			},
		},
		{
			Node: db.Node{ID: "critical-1", Name: "Critical Node"},
			Analytics: DetailAnalytics{
				RiskLevel: "critical",
				CPU: MetricAnalysis{
					Outlier: true,
					Shift: ShiftAnalysis{
						Significant: true,
					},
				},
				Highlights: []string{"cpu shift"},
			},
		},
	})

	if fleet.Critical != 1 || fleet.Stable != 1 {
		t.Fatalf("unexpected fleet counts: %#v", fleet)
	}
	if len(fleet.NodeInsights) != 2 || fleet.NodeInsights[0].NodeID != "critical-1" {
		t.Fatalf("expected critical node first, got %#v", fleet.NodeInsights)
	}
}
