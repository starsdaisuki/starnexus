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

type benchmarkBundle struct {
	GeneratedAt   int64                          `json:"generated_at"`
	WindowHours   int                            `json:"window_hours"`
	Experiments   int                            `json:"experiments"`
	Nodes         []string                       `json:"nodes"`
	BootstrapSeed uint64                         `json:"bootstrap_seed"`
	Detectors     []analytics.DetectorBenchmark  `json:"detectors"`
	Labels        []analytics.ExperimentLabel    `json:"labels"`
	PairwiseTests []analytics.PairwiseTest       `json:"pairwise_tests"`
}

func main() {
	var dbPath string
	var schemaPath string
	var outDir string
	var experimentsPath string
	var hours int
	var seed uint64

	flag.StringVar(&dbPath, "db", "./starnexus.db", "SQLite database path")
	flag.StringVar(&schemaPath, "schema", "./schema.sql", "schema.sql path")
	flag.StringVar(&outDir, "out", "./analysis-output/bench", "output directory")
	flag.StringVar(&experimentsPath, "experiments", "", "JSONL experiment labels path (required)")
	flag.IntVar(&hours, "hours", 168, "lookback window in hours")
	flag.Uint64Var(&seed, "seed", 42, "bootstrap seed (change to probe CI sensitivity)")
	flag.Parse()

	if experimentsPath == "" {
		log.Fatal("--experiments is required: supply an experiments.jsonl with at least one labelled window")
	}
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
	pointsByNode := map[string][]db.MetricPoint{}
	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		points, err := database.GetMetricPoints(node.ID, from, now.Unix())
		if err != nil {
			log.Fatalf("metric points for %s: %v", node.ID, err)
		}
		pointsByNode[node.ID] = points
		nodeIDs = append(nodeIDs, node.ID)
	}

	labels, err := analytics.LoadExperimentLabelsJSONL(experimentsPath)
	if err != nil {
		log.Fatalf("load experiments: %v", err)
	}
	if len(labels) == 0 {
		log.Fatal("no experiment labels found — benchmark needs at least one labelled window")
	}

	detectors := []analytics.Detector{
		analytics.NewFixedThresholdDetector(),
		analytics.NewPlainZScoreDetector(),
		analytics.NewEWMADetector(),
		analytics.NewCUSUMDetector(),
		analytics.NewMahalanobisDetector(),
		analytics.NewMCDMahalanobisDetector(),
		analytics.NewRobustShiftDetector(),
	}

	results := make([]analytics.DetectorBenchmark, 0, len(detectors))
	for _, detector := range detectors {
		result := analytics.RunDetectorBenchmarkSeed(detector, labels, pointsByNode, seed)
		results = append(results, result)
	}

	pairwise := analytics.BuildPairwiseTests(results)

	bundle := benchmarkBundle{
		GeneratedAt:   now.Unix(),
		WindowHours:   hours,
		Experiments:   len(labels),
		Nodes:         nodeIDs,
		BootstrapSeed: seed,
		Detectors:     results,
		Labels:        labels,
		PairwiseTests: pairwise,
	}

	if err := writeBenchJSON(filepath.Join(outDir, "benchmark.json"), bundle); err != nil {
		log.Fatalf("write benchmark.json: %v", err)
	}
	if err := writeBenchCSV(filepath.Join(outDir, "benchmark.csv"), results); err != nil {
		log.Fatalf("write benchmark.csv: %v", err)
	}
	if err := writeBenchPerExperimentCSV(filepath.Join(outDir, "per_experiment.csv"), results); err != nil {
		log.Fatalf("write per_experiment.csv: %v", err)
	}
	if err := writePairwiseCSV(filepath.Join(outDir, "pairwise_tests.csv"), pairwise); err != nil {
		log.Fatalf("write pairwise_tests.csv: %v", err)
	}
	if err := writeBenchMarkdown(filepath.Join(outDir, "report.md"), bundle); err != nil {
		log.Fatalf("write report.md: %v", err)
	}

	fmt.Printf("Benchmark written to %s\n", outDir)
	for _, result := range results {
		det := result.GroundTruth
		fmt.Printf("  %-16s detect=%.0f%% mean_delay=%.1fs fp=%.3f/node-hour events=%d\n",
			result.Name,
			det.DetectionRatePercent,
			det.MeanDetectionDelaySeconds,
			det.FalsePositiveRate,
			result.TotalEvents,
		)
	}
}

func writeBenchJSON(path string, bundle benchmarkBundle) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(bundle)
}

func writeBenchCSV(path string, results []analytics.DetectorBenchmark) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{
		"detector",
		"experiments",
		"detected",
		"detection_rate_percent",
		"mean_detection_delay_seconds",
		"detection_delay_ci_low",
		"detection_delay_ci_high",
		"detection_delay_sd",
		"recovered",
		"recovery_rate_percent",
		"mean_recovery_delay_seconds",
		"recovery_delay_ci_low",
		"recovery_delay_ci_high",
		"recovery_delay_sd",
		"steady_state_node_hours",
		"false_positive_events",
		"false_positive_rate",
		"firing_events",
		"recovery_events",
	}); err != nil {
		return err
	}

	for _, result := range results {
		gt := result.GroundTruth
		var detLo, detHi, detSD, recLo, recHi, recSD float64
		if result.BootstrapSummary != nil {
			detLo = result.BootstrapSummary.DetectionDelayCI[0]
			detHi = result.BootstrapSummary.DetectionDelayCI[1]
			detSD = result.BootstrapSummary.DetectionDelaySD
			recLo = result.BootstrapSummary.RecoveryDelayCI[0]
			recHi = result.BootstrapSummary.RecoveryDelayCI[1]
			recSD = result.BootstrapSummary.RecoveryDelaySD
		}
		if err := writer.Write([]string{
			result.Name,
			strconv.Itoa(gt.ExperimentCount),
			strconv.Itoa(gt.DetectedCount),
			fmt.Sprintf("%.2f", gt.DetectionRatePercent),
			fmt.Sprintf("%.2f", gt.MeanDetectionDelaySeconds),
			fmt.Sprintf("%.2f", detLo),
			fmt.Sprintf("%.2f", detHi),
			fmt.Sprintf("%.2f", detSD),
			strconv.Itoa(gt.RecoveredCount),
			fmt.Sprintf("%.2f", gt.RecoveryRatePercent),
			fmt.Sprintf("%.2f", gt.MeanRecoveryDelaySeconds),
			fmt.Sprintf("%.2f", recLo),
			fmt.Sprintf("%.2f", recHi),
			fmt.Sprintf("%.2f", recSD),
			fmt.Sprintf("%.3f", gt.SteadyStateNodeHours),
			strconv.Itoa(gt.FalsePositiveEventCount),
			fmt.Sprintf("%.4f", gt.FalsePositiveRate),
			strconv.Itoa(result.FiringEvents),
			strconv.Itoa(result.RecoveryEvents),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeBenchPerExperimentCSV(path string, results []analytics.DetectorBenchmark) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{
		"detector",
		"experiment_id",
		"node_id",
		"injection_type",
		"expected_metric",
		"duration_seconds",
		"detected",
		"detection_delay_seconds",
		"recovered",
		"recovery_delay_seconds",
		"peak_metric_value",
		"first_fire_score",
		"first_fire_metric",
		"first_fire_samples_in_window",
		"first_fire_detail",
	}); err != nil {
		return err
	}

	for _, result := range results {
		diagByID := map[string]analytics.DetectorDiagnostic{}
		for _, d := range result.Diagnostics {
			diagByID[d.ExperimentID] = d
		}
		for _, experiment := range result.GroundTruth.Experiments {
			diag := diagByID[experiment.ExperimentID]
			if err := writer.Write([]string{
				result.Name,
				experiment.ExperimentID,
				experiment.NodeID,
				experiment.InjectionType,
				experiment.ExpectedMetric,
				strconv.FormatInt(experiment.EndedAt-experiment.StartedAt, 10),
				strconv.FormatBool(experiment.Detected),
				strconv.FormatInt(experiment.DetectionDelaySeconds, 10),
				strconv.FormatBool(experiment.Recovered),
				strconv.FormatInt(experiment.RecoveryDelaySeconds, 10),
				fmt.Sprintf("%.2f", experiment.PeakMetricValue),
				fmt.Sprintf("%.4f", diag.Score),
				diag.Metric,
				strconv.Itoa(diag.SamplesInWindow),
				diag.Detail,
			}); err != nil {
				return err
			}
		}
	}
	return writer.Error()
}

func writePairwiseCSV(path string, tests []analytics.PairwiseTest) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{
		"detector_a",
		"detector_b",
		"experiments_paired",
		"detection_a_only",
		"detection_b_only",
		"detection_discordant",
		"detection_p_value",
		"delay_paired_count",
		"delay_mean_delta_seconds",
		"delay_p_value",
	}); err != nil {
		return err
	}
	for _, test := range tests {
		if err := writer.Write([]string{
			test.DetectorA,
			test.DetectorB,
			strconv.Itoa(test.ExperimentCount),
			strconv.Itoa(test.DetectionAOnly),
			strconv.Itoa(test.DetectionBOnly),
			strconv.Itoa(test.DetectionDiscordant),
			fmt.Sprintf("%.4f", test.DetectionPValue),
			strconv.Itoa(test.DelayPairedCount),
			fmt.Sprintf("%.2f", test.DelayMeanDeltaSec),
			fmt.Sprintf("%.4f", test.DelayPValue),
		}); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeBenchMarkdown(path string, bundle benchmarkBundle) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintf(file, "# StarNexus Detector Benchmark\n\nGenerated at: %s\n\n",
		time.Unix(bundle.GeneratedAt, 0).Format(time.RFC3339))
	fmt.Fprintf(file, "- Lookback window: %dh\n", bundle.WindowHours)
	fmt.Fprintf(file, "- Labelled experiments: %d\n", bundle.Experiments)
	fmt.Fprintf(file, "- Nodes: %d\n\n", len(bundle.Nodes))

	fmt.Fprintln(file, "## Head-to-Head Summary")
	fmt.Fprintln(file)
	fmt.Fprintln(file, "| Detector | Detect % | Mean delay (s) | 95% CI | Recovery % | Mean recovery (s) | FP/node-hour | Total events |")
	fmt.Fprintln(file, "|---|---:|---:|---|---:|---:|---:|---:|")
	for _, result := range bundle.Detectors {
		gt := result.GroundTruth
		detCI := "-"
		if result.BootstrapSummary != nil && result.BootstrapSummary.DetectionBootstrapCount > 0 {
			detCI = fmt.Sprintf("%.1f–%.1f", result.BootstrapSummary.DetectionDelayCI[0], result.BootstrapSummary.DetectionDelayCI[1])
		}
		fmt.Fprintf(file, "| `%s` | %.1f | %.1f | %s | %.1f | %.1f | %.3f | %d |\n",
			result.Name,
			gt.DetectionRatePercent,
			gt.MeanDetectionDelaySeconds,
			detCI,
			gt.RecoveryRatePercent,
			gt.MeanRecoveryDelaySeconds,
			gt.FalsePositiveRate,
			result.TotalEvents,
		)
	}

	fmt.Fprintln(file, "\n## Detector Descriptions")
	for _, result := range bundle.Detectors {
		fmt.Fprintf(file, "\n- **`%s`**: %s\n", result.Name, result.Description)
	}

	if len(bundle.PairwiseTests) > 0 {
		fmt.Fprintln(file, "\n## Pairwise Significance Tests")
		fmt.Fprintln(file)
		fmt.Fprintln(file, "Paired tests on the same experiment set. Detection p-value is a two-sided exact binomial on the discordant pairs (H0: each detector equally likely to catch a case the other misses). Delay p-value is a 5000-iteration random-sign permutation test on per-experiment delay deltas for experiments both detectors caught.")
		fmt.Fprintln(file)
		fmt.Fprintln(file, "| A | B | A-only | B-only | Detect p | Δdelay (s) | Pairs | Delay p |")
		fmt.Fprintln(file, "|---|---|---:|---:|---:|---:|---:|---:|")
		for _, test := range bundle.PairwiseTests {
			fmt.Fprintf(file, "| `%s` | `%s` | %d | %d | %.4f | %+.1f | %d | %.4f |\n",
				test.DetectorA, test.DetectorB,
				test.DetectionAOnly, test.DetectionBOnly,
				test.DetectionPValue,
				test.DelayMeanDeltaSec, test.DelayPairedCount, test.DelayPValue,
			)
		}
	}

	fmt.Fprintln(file, "\n## Interpretation Notes")
	fmt.Fprintln(file)
	fmt.Fprintln(file, "- All four detectors run as offline replays against the same metric history and the same ground-truth labels; differences come only from detection logic.")
	fmt.Fprintln(file, "- `fixed_threshold` represents classic Nagios/Zabbix static limits with no statistical component; it is the most common industry baseline.")
	fmt.Fprintln(file, "- `plain_zscore` uses non-robust mean/stddev; heavy tails inflate the stddev and mask real shifts, which is exactly why production uses robust statistics.")
	fmt.Fprintln(file, "- `ewma` is a control-chart-style detector; it tracks slow drift but chases sustained spikes because the moving average climbs with them.")
	fmt.Fprintln(file, "- `robust_shift` replays the production logic (median/MAD + baseline-shift + multi-gate policy) with a 5-minute scan step on a 24h window.")
	fmt.Fprintln(file, "- False-positive rate is normalized by steady-state node-hours (experiment windows and the 300s detection grace excluded) so it is comparable across detectors and lookback windows.")
	fmt.Fprintln(file, "- Bootstrap confidence intervals use 2000 resamples with a fixed seed; intervals widen as the experiment count shrinks and should be read as the dominant uncertainty signal at small n.")

	return nil
}
