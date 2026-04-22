package analytics

import (
	"testing"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func TestClassifyEventBandwidth(t *testing.T) {
	body := "Bandwidth Down pressure is above its robust baseline"
	classification := ClassifyEvent(db.Event{
		ID:    1,
		Type:  "anomaly",
		Title: "Bandwidth Down outlier detected",
		Body:  &body,
	})

	if classification.Category != "network_traffic" || classification.Metric != "bandwidth_down" {
		t.Fatalf("unexpected classification: %+v", classification)
	}
	if classification.Confidence < 0.7 {
		t.Fatalf("expected useful confidence, got %+v", classification)
	}
}

func TestClassifyEventRecovery(t *testing.T) {
	body := "Node healthy"
	classification := ClassifyEvent(db.Event{
		ID:    2,
		Type:  "status_change",
		Title: "Node recovered",
		Body:  &body,
	})

	if classification.Category != "recovery" {
		t.Fatalf("unexpected classification: %+v", classification)
	}
}
