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
- **Production path uses per-metric gates.** `anomaly.policyForMetric`
  treats CPU, memory, bandwidth, and connections independently in the
  live anomaly scheduler. Cross-metric correlation is handled only by
  the `mcd_mahalanobis` detector, which is currently a benchmark-only
  surrogate; it is not yet wired into the live scheduler. The upgrade
  is gated on the same longitudinal data pipeline needed for
  reliability-score validation.
- **No changepoint detection in the live path.** The production
  baseline-shift analysis uses a recent-versus-baseline median
  comparison. A proper CUSUM-based changepoint alternative
  (`cusum` detector) is now part of the benchmark catalogue for
  evaluation but is not the default on-call detector. Adopting it live
  would require calibrating K/H against a longer false-positive study.
- **Reliability score is a heuristic, not a fitted model.** Weights
  (availability 40%, latency 30%, stability 30%) were chosen for
  interpretability. The current evaluation dataset (n=3 nodes, ≈7 days)
  is too small for statistically defensible weight tuning or
  cross-validation. Longitudinal predictive validation remains future
  work once ≥1 month of data on ≥5 nodes exists. As a stopgap,
  `scripts/validate-reliability.py` runs a weight-sensitivity sweep
  over five weight schemes (default, heavy-availability, balanced,
  latency-focused, stability-focused) and reports Kendall-tau agreement
  between node rankings. On the current fleet the ranking is stable
  under every scheme tested, which is a weak but real robustness
  claim.
- **Event classification is a heuristic.** The category and likely-cause
  labels in `event_classifications.csv` come from a hand-written
  mapping, not a trained classifier. Treat confidence scores as ordinal
  ranking, not probability.

## Evaluation Limits

- **Labelled experiments cover CPU + memory, not disk or network.**
  `fault-injection.sh` runs a nice-d busy loop with `timeout`;
  `fault-injection-memory.sh` runs a Python bytearray page-touch
  allocator capped at a fraction of `MemAvailable`. Disk-IO pressure
  and network-latency (tc-netem) injection are still unimplemented
  because rollback safety is harder to guarantee on live VPS nodes —
  tc-netem locks SSH out if the rollback cron/`at` job fails, and disk
  pressure can damage the persistent store.
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
- Promote the `mcd_mahalanobis` and `cusum` detectors from benchmark-only
  to live-scheduler integration after a longer false-positive study
  calibrates their thresholds against non-labelled fleet traffic.
- Run a longitudinal reliability-score validation once at least a month
  of data exists on ≥5 nodes.
- Train a lightweight classifier on the event-classification task using
  labelled incidents and compare against the current heuristic.
- Add a streaming read replica option for dashboards that do not need
  write access.
