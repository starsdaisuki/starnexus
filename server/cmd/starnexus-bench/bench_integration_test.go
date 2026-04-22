package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

// TestBenchCLIEndToEnd exercises the starnexus-bench binary end-to-end
// against a synthetic DB + labels. Catches three classes of regression
// the unit tests miss: CSV writers producing well-formed output, JSON
// bundle staying parseable after struct-field edits, and the bootstrap
// seed flag actually propagating to the seed parameter.
func TestBenchCLIEndToEnd(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	schemaPath := filepath.Join(repoRoot, "server", "schema.sql")

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "starnexus.db")
	labelsPath := filepath.Join(tmp, "experiments.jsonl")
	outDir := filepath.Join(tmp, "out")

	database, err := db.Open(dbPath, schemaPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	nodeID := "node-a"
	now := time.Now().Unix()
	spikeStart := now - 600
	spikeEnd := spikeStart + 120

	// Replay a one-hour baseline of low CPU plus a 120 s spike via the
	// real UpsertReport path — same insertion logic an agent would use,
	// so metric_raw rows populate with the correct timestamps.
	for ts := now - 3600; ts <= now; ts += 30 {
		cpu := 5.0
		if ts >= spikeStart && ts <= spikeEnd {
			cpu = 95.0
		}
		req := &db.ReportRequest{
			CollectedAt: ts,
			NodeID:      nodeID,
			Name:        "Integration Node",
			Provider:    "test",
			Latitude:    35,
			Longitude:   139,
		}
		req.Metrics.CPUPercent = cpu
		req.Metrics.MemoryPercent = 30
		req.Metrics.DiskPercent = 20
		req.Metrics.BandwidthUp = 1
		req.Metrics.BandwidthDown = 2
		req.Metrics.LoadAvg = 0.5
		req.Metrics.Connections = 5
		req.Metrics.UptimeSeconds = 86400
		if _, err := database.UpsertReport(req); err != nil {
			t.Fatalf("upsert report ts=%d: %v", ts, err)
		}
	}
	database.Close()

	label := map[string]any{
		"experiment_id":      "int-test-1",
		"node_id":            nodeID,
		"injection_type":     "cpu_stress",
		"expected_metric":    "cpu_percent",
		"expected_direction": "increase",
		"started_at":         spikeStart,
		"ended_at":           spikeEnd,
		"duration_seconds":   120,
		"ssh_host":           "localhost",
		"notes":              "integration test",
	}
	data, _ := json.Marshal(label)
	if err := os.WriteFile(labelsPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write labels: %v", err)
	}

	// Invoke the CLI via `go run` so the command line plumbing (flag
	// parsing, CSV writers, markdown renderer) is exercised too.
	cmd := exec.Command("go", "run", ".",
		"-db", dbPath,
		"-schema", schemaPath,
		"-out", outDir,
		"-experiments", labelsPath,
		"-hours", "2",
		"-seed", "7",
	)
	cmd.Dir = filepath.Join(repoRoot, "server", "cmd", "starnexus-bench")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		t.Fatalf("run bench CLI: %v\nstderr: %s", err, stderr.String())
	}

	for _, name := range []string{"benchmark.json", "benchmark.csv", "per_experiment.csv", "pairwise_tests.csv", "report.md"} {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("missing output %s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("output %s is empty", name)
		}
	}

	raw, err := os.ReadFile(filepath.Join(outDir, "benchmark.json"))
	if err != nil {
		t.Fatalf("read benchmark.json: %v", err)
	}
	var bundle struct {
		BootstrapSeed uint64 `json:"bootstrap_seed"`
		Experiments   int    `json:"experiments"`
		Detectors     []struct {
			Name        string `json:"name"`
			GroundTruth struct {
				DetectedCount        int     `json:"detected_count"`
				DetectionRatePercent float64 `json:"detection_rate_percent"`
			} `json:"ground_truth"`
		} `json:"detectors"`
		PairwiseTests []struct {
			DetectorA string `json:"detector_a"`
			DetectorB string `json:"detector_b"`
		} `json:"pairwise_tests"`
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("parse benchmark.json: %v", err)
	}
	if bundle.BootstrapSeed != 7 {
		t.Fatalf("expected bootstrap_seed=7 to propagate, got %d", bundle.BootstrapSeed)
	}
	if bundle.Experiments != 1 {
		t.Fatalf("expected 1 experiment, got %d", bundle.Experiments)
	}
	if len(bundle.Detectors) != 5 {
		t.Fatalf("expected 5 detectors in output, got %d", len(bundle.Detectors))
	}
	if len(bundle.PairwiseTests) != 10 { // C(5,2)
		t.Fatalf("expected 10 pairwise tests, got %d", len(bundle.PairwiseTests))
	}

	var fixedThresh struct {
		Detected int
		Rate     float64
	}
	for _, d := range bundle.Detectors {
		if d.Name == "fixed_threshold" {
			fixedThresh.Detected = d.GroundTruth.DetectedCount
			fixedThresh.Rate = d.GroundTruth.DetectionRatePercent
		}
	}
	if fixedThresh.Detected != 1 || fixedThresh.Rate < 99 {
		t.Fatalf("fixed_threshold must detect the 120s spike at 95%% CPU, got detected=%d rate=%.1f",
			fixedThresh.Detected, fixedThresh.Rate)
	}

	report, err := os.ReadFile(filepath.Join(outDir, "report.md"))
	if err != nil {
		t.Fatalf("read report.md: %v", err)
	}
	if !strings.Contains(string(report), "Pairwise Significance Tests") {
		t.Fatalf("report.md missing pairwise section; first 400 bytes:\n%s", string(report[:min(400, len(report))]))
	}
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if strings.HasSuffix(dir, "/server") || strings.HasSuffix(dir, "server") {
				return filepath.Dir(dir), nil
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "server")); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
