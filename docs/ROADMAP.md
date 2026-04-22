# StarNexus Roadmap

Last updated: 2026-04-22

StarNexus is now a working self-hosted VPS observability platform. The next work should focus on reliability depth, evaluation quality, and operational usefulness rather than adding disconnected dashboard widgets.

## Current Position

The project already has:

- Distributed Go agent, Go server, Telegram bot, and unified web dashboard.
- Real VPS deployment across multiple nodes.
- SQLite-backed metrics, events, connection samples, and scores.
- Robust statistical analytics, anomaly detection, and reliability scoring.
- Labelled CPU fault-injection workflow and dashboard evaluation view.
- One-command node onboarding and safe agent sync scripts.

This is above a normal hobby dashboard. The main remaining gap is that alerts need a proper lifecycle, experiments need more repeated evidence, and the statistical methodology should be documented like a serious systems/statistics project.

## Target Levels

### Level A: Strong Engineering Capstone

Required capabilities:

- Stable deployment workflow.
- Reproducible local tests.
- Clear architecture, config, and data-flow documentation.
- Health checks and useful failure messages.
- Safe backup, restore, rollback, and node onboarding.

### Level B: High-End Systems Project

Required capabilities:

- Explicit alert and incident lifecycle.
- Fault-injection evaluation with repeated trials.
- False-positive and false-negative tracking.
- Root-cause and impact classification.
- Operational reliability metrics that can be explained and defended.

### Level C: CS + Statistics Hybrid Project

Required capabilities:

- Robust statistics explained in method docs.
- Evaluation against labelled experiments.
- Detection delay, recovery delay, precision, recall, and stability metrics.
- Data-quality scoring and missingness analysis.
- Exportable reports and figures.

## Execution Order

### 1. Alert And Incident Lifecycle

Add an `incidents` model separate from the existing append-only `events` stream.

Planned lifecycle:

- `open`: active issue requiring attention.
- `acknowledged`: human has seen it, but the issue is not recovered.
- `suppressed`: alerting paused until a deadline.
- `recovered`: issue is resolved and kept for history.

Implementation scope:

- Server DB table and migrations.
- Incident upsert/recover logic for status changes and anomaly detection.
- Dashboard incident panel and node detail incident context.
- Telegram commands for listing, acknowledging, and silencing incidents.

### 2. Experiment Evidence

Increase labelled fault-injection coverage after the incident layer is in place.

Minimum useful matrix:

- 30s, 90s, 150s, and 300s CPU pressure on LisaHost.
- Repeated trials for at least two durations.
- Verification that stricter anomaly thresholds still detect real pressure.
- Report detection delay, recovery delay, and false-positive events outside labelled windows.

### 3. Method And Results Docs

Create paper-style documentation from the actual system.

Deliverables:

- `docs/METHOD.md`: architecture, telemetry model, robust statistics, scoring, anomaly detection, reliability score.
- `docs/RESULTS.md`: current deployment, experiment table, detection/recovery metrics, limitations.
- Generated `analysis-output/report.md` from `starnexus-analyze`.

### 4. Operational Hardening

After incident lifecycle, evaluation, backup tooling, server health/version endpoints, startup config validation, and the agent disk-backed report queue:

- Bot/agent local health endpoints if they become useful outside systemd.
- Backup cron alerting if scheduled backups fail.
- Systemd hardening where it does not break agent observability.

### 5. Generic VPS Productization

Make the project easier to reuse on a new fleet.

Needed work:

- Dry-run mode for `scripts/onboard-node.sh`.
- Provider/location configuration templates.
- Config validation with actionable startup errors.
- Documentation for adding any VPS, not only the current three nodes.
- Optional Cloudflare Pages demo parity with the live Go dashboard API shape.

## Near-Term Priority

The highest-value remaining work is productization and richer analysis: dry-run onboarding, more repeated labelled experiments, root-cause classification, and exportable statistical figures.

## Recent Completions (2026-04-22 sprint)

The following items moved from pending to done in this sprint and
should be treated as baseline from now on:

- **Detector benchmark** (`starnexus-bench` / `make bench`): offline
  replay of fixed-threshold, plain z-score, EWMA, multivariate
  Mahalanobis, and robust-shift detectors against the same
  ground-truth labels, with bootstrap 95% CIs.
- **Statistical figures** (`scripts/generate-figures.py`,
  `make figures`): matplotlib exports for CPU time series, benchmark
  head-to-head, detection-delay distribution, FP/detection tradeoff,
  and event timeline.
- **Expanded experiment matrix** (`scripts/fault-injection-matrix.sh`):
  3 reps × 4 durations on a labelled test node, ≈70 min wall time.
- **Prometheus `/metrics` endpoint**: HTTP request counters and
  summaries, node-status gauges, incident-state gauges, served on the
  same private port.
- **End-to-end integration test** (`server/integration_test.go`):
  starts a real server on an ephemeral port and asserts the full
  report → incident → metrics pipeline.
- **Scalability benchmark** (`scripts/loadtest-local.sh`, 
  `starnexus-loadtest`): simulates 10–500 virtual agents against an
  isolated server and writes per-size JSON summaries. See
  `docs/RESULTS.md` for headline numbers.
- **Database concurrency fix**: `SetMaxOpenConns(1)` plus DSN-level
  busy_timeout pragma removed the SQLITE_BUSY storm that previously
  surfaced as ~98% HTTP 500 at 100+ agents.
- **Docker sandbox** (`docker-compose.yml`): reviewer-friendly
  `docker compose up --build` that boots a server plus three agents
  with synthetic node locations.
- **Method docs**: related-work section in `docs/METHOD.md` plus new
  `docs/LIMITATIONS.md` enumerating scope boundaries and known gaps.
