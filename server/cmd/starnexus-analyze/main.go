package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/analytics"
	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type analysisBundle struct {
	GeneratedAt    int64                      `json:"generated_at"`
	WindowHours    int                        `json:"window_hours"`
	FleetAnalytics analytics.FleetAnalytics   `json:"fleet_analytics"`
	Evaluation     analytics.EvaluationReport `json:"evaluation"`
	NodeAnalytics  []nodeAnalytics            `json:"node_analytics"`
}

type nodeAnalytics struct {
	Node      db.Node                   `json:"node"`
	Score     *db.NodeScore             `json:"score,omitempty"`
	Analytics analytics.DetailAnalytics `json:"analytics"`
}

func main() {
	var dbPath string
	var schemaPath string
	var outDir string
	var experimentsPath string
	var hours int

	flag.StringVar(&dbPath, "db", "./starnexus.db", "SQLite database path")
	flag.StringVar(&schemaPath, "schema", "./schema.sql", "schema.sql path")
	flag.StringVar(&outDir, "out", "./analysis-output", "output directory")
	flag.StringVar(&experimentsPath, "experiments", "", "optional JSONL experiment labels path")
	flag.IntVar(&hours, "hours", 168, "lookback window in hours")
	flag.Parse()

	if hours <= 0 {
		log.Fatal("hours must be positive")
	}

	database, err := db.Open(dbPath, schemaPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	now := time.Now()
	from := now.Add(-time.Duration(hours) * time.Hour).Unix()

	nodes, err := database.GetAllNodes()
	if err != nil {
		log.Fatalf("get nodes: %v", err)
	}
	scores, err := database.GetAllScores()
	if err != nil {
		log.Fatalf("get scores: %v", err)
	}
	scoreByNode := map[string]*db.NodeScore{}
	for i := range scores {
		scoreByNode[scores[i].NodeID] = &scores[i]
	}

	fleetSamples := make([]analytics.FleetNodeSample, 0, len(nodes))
	nodeDetails := make([]nodeAnalytics, 0, len(nodes))
	pointsByNode := make(map[string][]db.MetricPoint, len(nodes))

	if err := writeNodesCSV(filepath.Join(outDir, "nodes.csv"), nodes, scoreByNode); err != nil {
		log.Fatalf("write nodes.csv: %v", err)
	}
	if err := writeMetricsCSV(filepath.Join(outDir, "metrics.csv"), database, nodes, from, now.Unix()); err != nil {
		log.Fatalf("write metrics.csv: %v", err)
	}

	for _, node := range nodes {
		points, err := database.GetMetricPoints(node.ID, from, now.Unix())
		if err != nil {
			log.Fatalf("get metric points for %s: %v", node.ID, err)
		}
		pointsByNode[node.ID] = points
		detail := analytics.BuildDetailAnalytics(points, hours)
		fleetSamples = append(fleetSamples, analytics.FleetNodeSample{
			Node:      node,
			Score:     scoreByNode[node.ID],
			Analytics: detail,
		})
		nodeDetails = append(nodeDetails, nodeAnalytics{
			Node:      node,
			Score:     scoreByNode[node.ID],
			Analytics: detail,
		})
	}

	events, err := database.GetEventsSince(from, 5000)
	if err != nil {
		log.Fatalf("get events: %v", err)
	}
	if err := writeEventsCSV(filepath.Join(outDir, "events.csv"), events); err != nil {
		log.Fatalf("write events.csv: %v", err)
	}

	sources, err := database.GetConnectionHighlights(from, 5000)
	if err != nil {
		log.Fatalf("get connection highlights: %v", err)
	}
	if err := writeSourcesCSV(filepath.Join(outDir, "connection_sources.csv"), sources); err != nil {
		log.Fatalf("write connection_sources.csv: %v", err)
	}

	fleet := analytics.BuildFleetAnalytics(hours, fleetSamples)
	evaluation := analytics.BuildEvaluationReport(hours, fleetSamples, events)
	if experimentsPath != "" {
		labels, err := analytics.LoadExperimentLabelsJSONL(experimentsPath)
		if err != nil {
			log.Fatalf("load experiments: %v", err)
		}
		groundTruth := analytics.BuildGroundTruthEvaluation(labels, events, pointsByNode)
		evaluation.GroundTruth = &groundTruth
		if err := writeExperimentsCSV(filepath.Join(outDir, "experiment_evaluation.csv"), groundTruth.Experiments); err != nil {
			log.Fatalf("write experiment_evaluation.csv: %v", err)
		}
	}
	bundle := analysisBundle{
		GeneratedAt:    now.Unix(),
		WindowHours:    hours,
		FleetAnalytics: fleet,
		Evaluation:     evaluation,
		NodeAnalytics:  nodeDetails,
	}

	if err := writeJSON(filepath.Join(outDir, "analytics.json"), bundle); err != nil {
		log.Fatalf("write analytics.json: %v", err)
	}
	if err := writeMarkdownReport(filepath.Join(outDir, "report.md"), bundle); err != nil {
		log.Fatalf("write report.md: %v", err)
	}

	fmt.Printf("Analysis dataset written to %s\n", outDir)
	fmt.Printf("%s\n", evaluation.Notes[len(evaluation.Notes)-1])
}

func writeNodesCSV(path string, nodes []db.Node, scoreByNode map[string]*db.NodeScore) error {
	file, writer, err := createCSV(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()

	if err := writer.Write([]string{"node_id", "name", "provider", "ip_address", "status", "latitude", "longitude", "location_source", "last_seen", "composite_score"}); err != nil {
		return err
	}
	for _, node := range nodes {
		score := ""
		if item := scoreByNode[node.ID]; item != nil {
			score = fmt.Sprintf("%.4f", item.CompositeScore)
		}
		if err := writer.Write([]string{
			node.ID,
			node.Name,
			stringPtr(node.Provider),
			stringPtr(node.IPAddress),
			node.Status,
			fmt.Sprintf("%.6f", node.Latitude),
			fmt.Sprintf("%.6f", node.Longitude),
			stringPtr(node.LocationSource),
			int64Ptr(node.LastSeen),
			score,
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeMetricsCSV(path string, database *db.DB, nodes []db.Node, from, to int64) error {
	file, writer, err := createCSV(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()

	if err := writer.Write([]string{"node_id", "timestamp", "cpu_percent", "memory_percent", "disk_percent", "bandwidth_up", "bandwidth_down", "load_avg", "connections"}); err != nil {
		return err
	}
	for _, node := range nodes {
		points, err := database.GetMetricPoints(node.ID, from, to)
		if err != nil {
			return err
		}
		for _, point := range points {
			if err := writer.Write([]string{
				node.ID,
				strconv.FormatInt(point.Timestamp, 10),
				fmt.Sprintf("%.6f", point.CPUPercent),
				fmt.Sprintf("%.6f", point.MemoryPercent),
				fmt.Sprintf("%.6f", point.DiskPercent),
				fmt.Sprintf("%.6f", point.BandwidthUp),
				fmt.Sprintf("%.6f", point.BandwidthDown),
				fmt.Sprintf("%.6f", point.LoadAvg),
				strconv.Itoa(point.Connections),
			}); err != nil {
				return err
			}
		}
	}
	return writer.Error()
}

func writeEventsCSV(path string, events []db.Event) error {
	file, writer, err := createCSV(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()

	if err := writer.Write([]string{"id", "node_id", "node_name", "type", "severity", "title", "body", "created_at"}); err != nil {
		return err
	}
	for _, event := range events {
		if err := writer.Write([]string{
			strconv.FormatInt(event.ID, 10),
			stringPtr(event.NodeID),
			stringPtr(event.NodeName),
			event.Type,
			event.Severity,
			event.Title,
			stringPtr(event.Body),
			strconv.FormatInt(event.CreatedAt, 10),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeSourcesCSV(path string, sources []db.ConnectionSummary) error {
	file, writer, err := createCSV(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()

	if err := writer.Write([]string{"node_id", "node_name", "source_ip", "country", "city", "protocol", "local_port", "is_cloudflare", "sample_count", "peak_rate_bps", "avg_rate_bps", "latest_total_bytes", "last_seen"}); err != nil {
		return err
	}
	for _, source := range sources {
		if err := writer.Write([]string{
			stringPtr(source.NodeID),
			stringPtr(source.NodeName),
			source.SourceIP,
			source.SourceCountry,
			source.SourceCity,
			source.Protocol,
			strconv.Itoa(source.LocalPort),
			strconv.FormatBool(source.IsCloudflare),
			strconv.Itoa(source.SampleCount),
			fmt.Sprintf("%.6f", source.PeakRateBPS),
			fmt.Sprintf("%.6f", source.AvgRateBPS),
			strconv.FormatUint(source.LatestTotalBytes, 10),
			strconv.FormatInt(source.LastSeen, 10),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeExperimentsCSV(path string, experiments []analytics.ExperimentEvaluation) error {
	file, writer, err := createCSV(path)
	if err != nil {
		return err
	}
	defer file.Close()
	defer writer.Flush()

	if err := writer.Write([]string{"experiment_id", "node_id", "injection_type", "expected_metric", "started_at", "ended_at", "detected", "detection_type", "detection_severity", "first_detection_at", "detection_delay_seconds", "recovered", "first_recovery_at", "recovery_delay_seconds", "peak_metric_value"}); err != nil {
		return err
	}
	for _, experiment := range experiments {
		if err := writer.Write([]string{
			experiment.ExperimentID,
			experiment.NodeID,
			experiment.InjectionType,
			experiment.ExpectedMetric,
			strconv.FormatInt(experiment.StartedAt, 10),
			strconv.FormatInt(experiment.EndedAt, 10),
			strconv.FormatBool(experiment.Detected),
			experiment.DetectionType,
			experiment.DetectionSeverity,
			optionalUnix(experiment.FirstDetectionAt),
			optionalInt64(experiment.DetectionDelaySeconds),
			strconv.FormatBool(experiment.Recovered),
			optionalUnix(experiment.FirstRecoveryAt),
			optionalInt64(experiment.RecoveryDelaySeconds),
			fmt.Sprintf("%.6f", experiment.PeakMetricValue),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeJSON(path string, value any) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeMarkdownReport(path string, bundle analysisBundle) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	eval := bundle.Evaluation
	_, err = fmt.Fprintf(file, `# StarNexus Analysis Report

Generated at: %s

## Fleet Summary

%s

## Evaluation Proxy

- Window: %dh
- Nodes: %d
- Mean coverage: %.1f%%
- Total statistical signals: %d
- Events: %d total, %d anomaly, %d status-change
- Signal/event proxy precision: %.1f%%
- Signal/event proxy recall: %.1f%%

## Node Summaries

`, time.Unix(bundle.GeneratedAt, 0).Format(time.RFC3339), bundle.FleetAnalytics.Summary, eval.WindowHours, eval.NodeCount, eval.MeanCoveragePercent, eval.TotalSignals, eval.EventCount, eval.AnomalyEventCount, eval.StatusEventCount, eval.SignalEventAgreement.ProxyPrecisionPercent, eval.SignalEventAgreement.ProxyRecallPercent)
	if err != nil {
		return err
	}

	for _, node := range eval.NodeSummaries {
		if _, err := fmt.Fprintf(file, "- `%s`: %s, %.0f%% coverage, %d signal(s), CPU shift %.1f%%, memory shift %.1f%%, connection shift %.1f%%\n", node.NodeID, node.RiskLevel, node.CoveragePercent, node.SignalCount, node.CPUShiftPercent, node.MemShiftPercent, node.ConnShiftPercent); err != nil {
			return err
		}
	}

	if eval.GroundTruth != nil {
		gt := eval.GroundTruth
		if _, err := fmt.Fprintf(file, "\n## Ground-Truth Experiments\n\n- Experiments: %d\n- Detection rate: %.1f%%\n- Status detections: %d\n- Anomaly detections: %d\n- Mean detection delay: %.1fs\n- Recovery rate: %.1f%%\n- Mean recovery delay: %.1fs\n- False-positive events outside labelled windows: %d (%d status, %d anomaly)\n\n", gt.ExperimentCount, gt.DetectionRatePercent, gt.StatusDetectionCount, gt.AnomalyDetectionCount, gt.MeanDetectionDelaySeconds, gt.RecoveryRatePercent, gt.MeanRecoveryDelaySeconds, gt.FalsePositiveEventCount, gt.FalsePositiveStatusCount, gt.FalsePositiveAnomalyCount); err != nil {
			return err
		}
		for _, experiment := range gt.Experiments {
			if _, err := fmt.Fprintf(file, "- `%s`: detected=%t via=%s delay=%ds recovered=%t recovery=%ds peak_%s=%.1f\n", experiment.ExperimentID, experiment.Detected, experiment.DetectionType, experiment.DetectionDelaySeconds, experiment.Recovered, experiment.RecoveryDelaySeconds, experiment.ExpectedMetric, experiment.PeakMetricValue); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprint(file, "\n## Method Notes\n\n"); err != nil {
		return err
	}
	for _, note := range eval.Notes {
		if _, err := fmt.Fprintf(file, "- %s\n", note); err != nil {
			return err
		}
	}
	return nil
}

func createCSV(path string) (*os.File, *csv.Writer, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return file, csv.NewWriter(file), nil
}

func stringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64Ptr(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func optionalUnix(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func optionalInt64(value int64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}
