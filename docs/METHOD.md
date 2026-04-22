# StarNexus Method

Last updated: 2026-04-22

This document describes the technical method behind StarNexus as a self-hosted VPS observability and reliability-analysis system.

## Problem Statement

Small self-hosted VPS fleets — typically operated by a single owner
running a handful of machines across providers — sit in a gap between
two ends of the monitoring spectrum. At one end, classic infrastructure
monitoring (Nagios, Zabbix) uses static thresholds that miss statistical
regime changes and generate high false-positive rates when traffic is
naturally bursty. At the other end, hosted observability platforms
(Datadog, Grafana Cloud, New Relic) offer rich statistics but are
expensive, opaque, and require trusting a third party with every
metric. StarNexus is an attempt to occupy the middle: a lightweight,
self-hosted, statistically principled monitoring platform that can be
defended methodologically — not just "it looked green on the dashboard."

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

Agents attach `collected_at` to metric reports. The server stores that timestamp in `metrics_raw.created_at`, so reports replayed after a primary outage remain aligned with the actual collection time. Current node state still uses the newest report timestamp and ignores stale replay for incident state transitions.

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

Historical replay protection:

- Agent disk queues preserve outage-window metric samples for analysis.
- Stale replayed reports do not open new degraded/offline status incidents.
- Older replayed samples do not overwrite the latest `node_metrics` snapshot.

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
- Observation exposure in node-hours.
- Steady-state exposure in node-hours after excluding labelled experiment windows plus detection grace.
- False-positive rate per node-hour.

Current fault injection is CPU-only and uses `nice + timeout` so it does not alter firewall rules, network shaping, SSH settings, or proxy services.

## Event Classification

Analysis exports include a lightweight heuristic classifier for operational events. It maps event title/body evidence into categories such as `resource_pressure`, `network_traffic`, `reachability`, and `recovery`, plus a likely cause and confidence score.

This is not a causal model. It is an explainability layer for reports and triage. For example, a bandwidth-down outlier is classified as `network_traffic` with likely causes such as backup transfer, proxy traffic, package download, or other ingress spikes.

## Detector Benchmark

StarNexus runs its production detector (robust z-score plus baseline
shift, multi-gate policy) alongside three textbook baselines
(fixed-threshold à la Nagios, plain-mean-and-stddev z-score, and
exponentially-weighted moving average) and a simple multivariate
Mahalanobis composite. All five replay the same metric history and are
scored against the same labelled experiments. Results are written to
`analysis-output/bench/` by `starnexus-bench` (or `make bench`).

The benchmark exists to give an honest, apples-to-apples comparison
instead of asserting a priori that "robust statistics are better."
Findings are documented in `docs/RESULTS.md` with 95 % bootstrap
confidence intervals on detection and recovery delays; see
`docs/LIMITATIONS.md` for caveats about sample size.

## Related Work

**Nagios / Zabbix / Prometheus Alertmanager.** Threshold-based
monitoring is the operational default. It is transparent and easy to
reason about, but it does not adapt to the node's own baseline, which
leads to either over-tight alerts on noisy nodes or late alerts on
quiet ones. The `fixed_threshold` detector in the StarNexus benchmark
is a minimal analogue and illustrates this tradeoff directly.

**Datadog / Grafana Cloud / New Relic.** Commercial observability
platforms implement adaptive thresholds, anomaly detection, and
correlation across metrics. They are well-engineered but hosted,
proprietary, and priced per-host. StarNexus deliberately limits scope
to a self-hosted single-owner fleet so the full pipeline — ingestion,
storage, detection, scoring, alerting — can be inspected and modified.

**Robust statistics for anomaly detection.** The use of median / MAD
for location and scale is a standard move in industrial process control
and outlier detection literature (e.g. the Huber and Rousseeuw family
of robust estimators). StarNexus uses the modified z-score
`0.6745·(x − median) / MAD` and gates it with minimum absolute value
and baseline-shift requirements, both of which are common in
practitioner literature for reducing false positives on skewed
distributions.

**Multivariate anomaly detection.** Full-covariance Mahalanobis
distance with Minimum Covariance Determinant (MCD) estimators is the
classical technique for multivariate robust outliers. StarNexus
currently uses only a diagonal approximation (`mahalanobis` detector in
the benchmark) to avoid the iterative trimming step; proper MCD and
full-covariance handling remain future work, flagged in
`docs/LIMITATIONS.md`.

**Changepoint detection.** CUSUM and Bayesian online changepoint
detection (Adams & MacKay 2007) are the rigorous alternatives to the
recent-vs-baseline heuristic used here. They are strictly more
principled for detecting regime shifts but are also more expensive and
introduce their own tuning burden. StarNexus currently favours the
simpler rolling-window comparison to keep the analytics layer
interpretable.

## Limitations

`docs/LIMITATIONS.md` is the authoritative list. Headline items:

- CPU-only fault injection; memory/network experiments are deferred
  because rollback safety on live VPS nodes is hard to guarantee.
- Current labelled dataset is small; bootstrap intervals on detection
  delay are wide, and the reliability score is interpretable but not
  yet validated against held-out incidents.
- SQLite write serialization caps ingestion at ≈1000 reports/sec per
  control-plane instance on commodity hardware.
