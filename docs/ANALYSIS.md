# StarNexus Analysis Workflow

StarNexus can export a reproducible research dataset from the production SQLite database. This is intended for statistical analysis, anomaly-method evaluation, and graduation-project reporting.

## Export Locally

From the repo root:

```bash
make analyze
```

The command writes `analysis-output/` with:

- `nodes.csv`: node metadata, map location source, status, and score.
- `metrics.csv`: raw metric time series for the selected lookback window.
- `events.csv`: status-change and anomaly events used as weak labels.
- `connection_sources.csv`: persisted ingress source summaries.
- `analytics.json`: full robust-statistics, fleet radar, and evaluation payload.
- `report.md`: compact human-readable analysis summary.

## Export From A VPS Copy

On the server host, run:

```bash
cd /root/starnexus
./starnexus-analyze -db ./starnexus.db -schema ./schema.sql -out ./analysis-output -hours 168
```

If the binary has not been deployed yet, build it locally with `make build-analyze` and upload `bin/starnexus-analyze`.

## Evaluation Interpretation

The current evaluation is intentionally labelled as a proxy evaluation:

- Statistical signals come from robust outlier detection, baseline shift, trend, and volatility.
- Events are treated as weak labels, not ground truth.
- Proxy precision means “nodes with statistical signals that also had events in the same window”.
- Proxy recall means “eventful nodes that were also flagged by statistical signals”.

For a stronger dissertation-style evaluation, add controlled fault injection:

- CPU stress with known start/end timestamps.
- Memory pressure with known start/end timestamps.
- Network loss/latency injection with known start/end timestamps.
- Synthetic ingress bursts with known source and duration.

Then measure detection delay, false positive rate, and recovery stability against those ground-truth intervals.

Start with the CPU-only wrapper in [`docs/FAULT-INJECTION.md`](FAULT-INJECTION.md). It produces a CSV polling log that can be compared against the exported `metrics.csv` and `events.csv`.

If `experiments.jsonl` is available, pass it to the analysis CLI:

```bash
./starnexus-analyze \
  -db ./starnexus.db \
  -schema ./schema.sql \
  -out ./analysis-output/with-experiments \
  -hours 24 \
  -experiments ./analysis-output/experiments.jsonl
```

This adds `experiment_evaluation.csv` and a `ground_truth` section in `analytics.json` with detection delay, recovery delay, detection rate, recovery rate, and false-positive event count outside labelled experiment windows.

The dashboard reads the server-side `experiment_labels_path` and shows the same ground-truth metrics in Experiment View. The default fault-injection wrapper appends labels to the server path automatically.
