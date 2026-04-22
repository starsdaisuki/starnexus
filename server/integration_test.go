package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/starsdaisuki/starnexus/server/internal/api"
	"github.com/starsdaisuki/starnexus/server/internal/db"
	"github.com/starsdaisuki/starnexus/server/internal/locations"
)

// TestEndToEndPipeline starts a real server, simulates a fake agent
// reporting metrics, and asserts the full pipeline: node registration,
// status change, incident lifecycle, events stream, and metrics
// endpoint. Catches regressions spanning db, api, and observability
// layers in one pass.
func TestEndToEndPipeline(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "starnexus.db")
	schemaPath := resolveSchemaPath(t)

	database, err := db.Open(dbPath, schemaPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	nodeLocations, err := locations.Load("")
	if err != nil {
		t.Fatalf("locations: %v", err)
	}

	token := "test-token-abc"
	server := api.New(database, token, "", "", "", "", nodeLocations)
	ts := httptest.NewServer(server)
	defer ts.Close()

	client := ts.Client()

	postReport := func(t *testing.T, cpu, memory float64) {
		t.Helper()
		req := map[string]any{
			"node_id":         "node-a",
			"name":            "Test Node A",
			"provider":        "Test",
			"public_ip":       "203.0.113.5",
			"latitude":        35.0,
			"longitude":       139.0,
			"location_source": "manual",
			"metrics": map[string]any{
				"cpu_percent":    cpu,
				"memory_percent": memory,
				"disk_percent":   20.0,
				"bandwidth_up":   1.0,
				"bandwidth_down": 2.0,
				"load_avg":       0.5,
				"connections":    12,
				"uptime_seconds": 86400,
			},
		}
		body, _ := json.Marshal(req)
		request, err := http.NewRequest(http.MethodPost, ts.URL+"/api/report", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(request)
		if err != nil {
			t.Fatalf("post report: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("report status %d", resp.StatusCode)
		}
	}

	postReport(t, 5.0, 40.0)

	nodes, err := database.GetAllNodes()
	if err != nil {
		t.Fatalf("get nodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node-a" || nodes[0].Status != "online" {
		t.Fatalf("expected one online node-a, got %+v", nodes)
	}

	postReport(t, 95.0, 50.0)

	nodes, _ = database.GetAllNodes()
	if nodes[0].Status != "degraded" {
		t.Fatalf("expected degraded after high cpu, got %s", nodes[0].Status)
	}

	events, err := database.GetRecentEvents(10)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	foundDegraded := false
	for _, event := range events {
		if event.Type == "status_change" && strings.Contains(event.Title, "degraded") {
			foundDegraded = true
			break
		}
	}
	if !foundDegraded {
		t.Fatalf("expected Node degraded status_change event, got %+v", events)
	}

	active, err := database.GetActiveIncidents(20)
	if err != nil {
		t.Fatalf("incidents: %v", err)
	}
	foundIncident := false
	for _, incident := range active {
		if incident.Type == "node_degraded" && incident.Status == "open" {
			foundIncident = true
		}
	}
	if !foundIncident {
		t.Fatalf("expected open node_degraded incident, got %+v", active)
	}

	postReport(t, 3.0, 40.0)
	nodes, _ = database.GetAllNodes()
	if nodes[0].Status != "online" {
		t.Fatalf("expected online after cpu drop, got %s", nodes[0].Status)
	}
	active, _ = database.GetActiveIncidents(20)
	for _, incident := range active {
		if incident.Type == "node_degraded" && incident.Status == "open" {
			t.Fatalf("expected node_degraded incident to be recovered, still open: %+v", incident)
		}
	}

	resp, err := client.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(body)

	for _, expected := range []string{
		"starnexus_http_requests_total",
		"starnexus_uptime_seconds",
		`starnexus_nodes_total{status="online"}`,
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("metrics missing %q; full body:\n%s", expected, truncate(text, 500))
		}
	}
}

func resolveSchemaPath(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"schema.sql", "../schema.sql"} {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.Size() > 0 {
			return abs
		}
	}
	t.Fatalf("schema.sql not found relative to cwd")
	return ""
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("... (%d bytes truncated)", len(s)-max)
}
