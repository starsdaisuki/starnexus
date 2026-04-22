# StarNexus Limitations

Last updated: 2026-04-22

This document names the scope boundaries, assumptions, and known
failure modes of StarNexus. It is deliberately blunt so reviewers and
operators do not have to reconstruct caveats from the rest of the
codebase.

## Scope Boundaries

StarNexus targets **small self-hosted VPS fleets** (single-digit to
low-hundreds of nodes) operated by a single owner. The following are
explicitly out of scope:

- Multi-tenant deployments with per-tenant isolation.
- Regulated-industry compliance (HIPAA, PCI-DSS, SOX).
- High-availability control plane — the server is a single-writer
  process with no failover.
- Horizontal scale-out beyond a single SQLite instance.
- User authentication and authorization for the dashboard — access is
  gated only by SSH tunnel and API token, not per-user identity.
- Synthetic transaction monitoring or external probe fleets.
- Long-term metric retention beyond the hourly/daily aggregates (raw
  samples are purged after seven days by the downsampling scheduler).

## Data Model Limits

- **SQLite write serialization.** Every write goes through a single
  pooled connection (`SetMaxOpenConns(1)` in `server/internal/db/db.go`)
  to avoid SQLITE_BUSY storms. Measured ceiling on a 2020-era M1 mac is
  ≈1000 reports/sec with p99 latency 109 ms (see `docs/RESULTS.md`).
  Beyond that, reports queue.
- **No retention policy configuration.** Raw → hourly → daily retention
  is hard-coded in the downsample scheduler. Changing it requires a code
  edit.
- **Single database file.** There is no schema sharding, no partitioning
  by time, and no write-path buffering. A corrupted SQLite file requires
  the backup restore tool.
- **WAL checkpointing is not actively managed.** The database file can
  grow if a reader holds an open transaction during a flood of writes.

## Analytics and Anomaly Detection Limits

- **Robust anomaly detector misses sub-five-minute bursts.** The
  scheduler runs every 300 s on a 24 h rolling window. A 30–60 s spike
  leaves median/MAD unchanged and rarely crosses the
  `MinDelta`/`MinDeltaPercent` gates. The production design compensates
  with the status-threshold path (`cpu > 80` or `memory > 90`), which
  fires on the next 30 s report. The baseline comparison in
  `analysis-output/bench/` documents this division of labour explicitly.
- **Per-metric gates are independent.** `anomaly.policyForMetric` treats
  CPU, memory, bandwidth, and connections independently. The Mahalanobis
  detector in the benchmark offers a multivariate alternative but uses
  only a diagonal covariance approximation; correlated-metric anomalies
  are still handled heuristically.
- **No changepoint detection.** Baseline-shift analysis uses a simple
  recent-versus-baseline median comparison. This is a pragmatic
  approximation, not a CUSUM or Bayesian changepoint test; it is cheap
  but can miss multi-step regime changes.
- **Reliability score is a heuristic, not a fitted model.** Weights
  (availability 40%, latency 30%, stability 30%) were chosen for
  interpretability. The current evaluation dataset (n=3 nodes, ≈7 days)
  is too small for statistically defensible weight tuning or
  cross-validation. Weight sensitivity analysis and longitudinal
  predictive validation remain future work.
- **Event classification is a heuristic.** The category and likely-cause
  labels in `event_classifications.csv` come from a hand-written
  mapping, not a trained classifier. Treat confidence scores as ordinal
  ranking, not probability.

## Evaluation Limits

- **Labelled experiments are CPU-only.** `fault-injection.sh` runs a
  nice-d busy loop with `timeout`. Memory pressure, disk pressure, and
  network-latency fault injection are not implemented because rollback
  safety on live VPS nodes is harder to guarantee.
- **Small and node-biased experiment set.** Current labels are all on
  `jp-lisahost`. Results generalize to other nodes only insofar as
  steady-state metric distributions are similar. Detection rate on a
  noisier node might be lower.
- **Proxy false-positive counting.** FP rate excludes the labelled
  experiment window plus 300 s detection grace. Real-world anomalies
  outside labelled windows are not categorized; they count as FPs even
  if they correspond to real events the experiment framework did not
  capture.
- **Detection-delay timer starts at experiment start, not metric
  change.** Some detectors could theoretically have sub-30-second
  delays, but agent reports arrive at 30 s cadence, so any detector is
  bounded below by the report interval.

## Security Limits

- **Static API token.** Every agent uses the same bearer token. Token
  rotation requires editing all agent configs. mTLS or per-agent
  identities are future work.
- **TLS is not served by the server.** The dashboard is accessed via
  SSH tunnel; direct exposure of port 8900 is not recommended. The
  `/metrics` endpoint is intentionally placed behind the same gate, not
  on a separate port.
- **Install script downloads over plain HTTP.** The one-liner
  `curl http://<server>:8900/install.sh | bash` is convenient but
  assumes the operator trusts the network path. Verify the bundled
  agent binary checksum before trusting a new install host.

## Deployment and Operations Limits

- **Linux-only agent.** `/proc` parsing is hard-coded. macOS and BSD are
  unsupported.
- **Single primary server.** `scripts/manage-node.sh` and the Telegram
  bot both assume one control plane. Federation across multiple
  StarNexus servers is not implemented.
- **No structured logging.** Server output is human-readable text
  written with `log.Printf`. Log aggregation into external systems
  requires parsing the plain-text lines.
- **Docker sandbox is for reviewers, not production.** The bundled
  `docker-compose.yml` uses a static token and an ephemeral volume. It
  is deliberately insecure to keep setup friction low for evaluation.

## Known Short-Term Risks

- **Downsampling window assumption.** The daily scheduler purges raw
  samples older than 7 days. If an analysis run is not triggered inside
  that window, comparison against experiments in older windows relies
  on the hourly aggregate, which loses sub-hour resolution.
- **Agent disk queue replay.** Replayed reports preserve `collected_at`
  but do not re-trigger anomaly detection on replay. A long outage
  followed by a replay produces historical metrics without the
  corresponding incident timeline.
- **Bot state file is local.** `starnexus-bot-state.json` is not
  replicated. Losing the bot VPS disk discards per-chat preferences.

## What We Would Do With More Time

Not a roadmap, just a candid list of work that would sharpen the
evaluation without changing the core architecture:

- Expand the fault-injection matrix to memory and network-latency
  experiments on an isolated test node.
- Replace the heuristic baseline-shift detector with CUSUM or Bayesian
  online changepoint.
- Run a longitudinal reliability-score validation once at least a month
  of data exists on ≥5 nodes.
- Train a lightweight classifier on the event-classification task using
  labelled incidents and compare against the current heuristic.
- Add a streaming read replica option for dashboards that do not need
  write access.
