# StarNexus Method

Last updated: 2026-04-21

This document describes the technical method behind StarNexus as a self-hosted VPS observability and reliability-analysis system.

## System Model

StarNexus uses a lightweight distributed architecture:

- `agent`: runs on each VPS and reports local metrics, link probes, location, and live connection samples.
- `server`: receives reports, persists telemetry in SQLite, computes analytics, serves APIs, and hosts the dashboard.
- `web`: reads server APIs and visualizes topology, incidents, metrics, links, reliability, and experiments.
- `bot`: provides Telegram operations commands, summaries, proactive alerts, and incident actions.

The production deployment is intentionally private. The Go server listens on the primary VPS, while dashboard access is normally done through an SSH tunnel.

## Telemetry Model

StarNexus separates operational data into several layers:

- Node inventory: node id, name, provider, IP address, map coordinates, coordinate source, status, and last seen time.
- Current metrics: CPU, memory, disk, bandwidth, load, connection count, uptime.
- Raw time series: per-report metric samples stored in `metrics_raw`.
- Aggregates: hourly and daily tables used for longer windows.
- Links: pairwise probe latency, packet loss, and path status.
- Connection samples: sampled ingress sources with GeoIP metadata and transfer-rate estimates.
- Events: append-only audit stream for status changes, anomaly detections, and operational signals.
- Incidents: actionable lifecycle objects with `open`, `acknowledged`, `suppressed`, and `recovered` states.

The split between `events` and `incidents` is deliberate. Events preserve historical evidence; incidents represent the current operational problem that a human can acknowledge or suppress.

## Metric Analytics

For each node, StarNexus builds a rolling detail analysis over the selected time window. The current dashboard commonly uses 24 hours.

Per-metric analysis includes:

- Mean, median, p95, standard deviation, and median absolute deviation.
- Robust z-score based on median and MAD.
- Slope per hour for directional trend.
- Volatility classification.
- Recent-vs-baseline shift using median comparisons.
- Coverage percentage to avoid over-trusting sparse telemetry.

The robust z-score is preferred over a plain z-score because VPS metrics often contain bursts, long tails, and non-normal distributions.

## Anomaly Detection

Anomaly detection runs on the server every 5 minutes after a node has enough raw samples.

The current policy is intentionally conservative:

- CPU outlier requires high robust z-score, high absolute CPU, and meaningful delta from baseline.
- Memory outlier requires high absolute memory pressure and a robust shift.
- Connection and bandwidth anomalies require large absolute values, not just statistical surprise.
- Low-load statistical outliers are ignored because they are rarely actionable.
- Repeated signals are folded into the same active incident by fingerprint.

Detection produces:

- An append-only `events` row when a new anomaly signal is first observed.
- An `incidents` row or update for the actionable lifecycle object.

When a signal is no longer present on a later anomaly run, the corresponding metric incident is marked `recovered`.

## Status And Incident Lifecycle

Status changes are generated from direct node reports and offline detection:

- `online`: node reports within the expected window and does not exceed basic pressure thresholds.
- `degraded`: node reports but crosses basic CPU or memory pressure thresholds.
- `offline`: node has not reported within the configured threshold.

Incident lifecycle:

- `open`: active issue requiring attention.
- `acknowledged`: operator has seen it, but it is not resolved.
- `suppressed`: issue is intentionally silenced until a deadline.
- `recovered`: issue has resolved and remains available as history.

Status incidents and metric anomaly incidents are separate. For example, a CPU stress test can open both `node_degraded` and `metric_anomaly`; recovery can occur at different times because node status is evaluated on every report, while anomaly recovery waits for the next anomaly scheduler run.

## Reliability Score

The reliability layer computes a 24-hour fleet and node report. It combines:

- Availability proxy.
- Data coverage.
- Stability estimate from statistical risk and volatility.
- Event pressure from operational incidents and statistical signals.
- Staleness penalty when reports are missing.

The score is not a formal SLA. It is an operational index designed to rank nodes and highlight where to inspect first.

## Ground-Truth Evaluation

Fault-injection labels are stored in JSONL format with known start and end timestamps.

For each labelled experiment, StarNexus measures:

- Whether a detection event occurred inside the detection window.
- Whether the first detection came from `status_change` or `anomaly`.
- First detection timestamp and detection delay.
- Whether a recovery event occurred after the experiment ended.
- Recovery delay.
- Peak metric value during the experiment.
- False-positive detection events outside labelled windows.
- False-positive breakdown by status events and anomaly events.

Current fault injection is CPU-only and uses `nice + timeout` so it does not alter firewall rules, network shaping, SSH settings, or proxy services.

## Limitations

Current limitations:

- The labelled experiment dataset is still small.
- CPU experiments are tested on LisaHost only.
- False positives are event-based and include historical anomaly events in the dashboard window.
- The reliability score is an interpretable heuristic, not yet a fitted statistical model.
- Network-loss and memory-pressure experiments are not implemented yet because they are riskier on live VPS nodes.

These limitations are acceptable for the current operational system, but they should be addressed before presenting the project as a mature statistical evaluation.
