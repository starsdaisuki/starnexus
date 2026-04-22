# StarNexus Project Status

This document summarizes the current state of StarNexus after the observability, analytics, deployment, and Telegram bot upgrades.

## Current Position

StarNexus is now a working distributed VPS observability system, not just a dashboard prototype. It has:

- Production Go server on a primary VPS.
- Go agents running on monitored VPS nodes.
- Canonical web frontend under `web/public/`.
- Historical metric persistence in SQLite.
- Live connection sampling with GeoIP enrichment.
- Link diagnostics between nodes.
- Event stream separated from status history.
- Robust statistical analytics and reliability scoring.
- Controlled fault-injection labels for evaluation.
- Telegram command bot with per-chat alert preferences.
- Operational scripts for server deploy, agent sync, node onboarding, analysis export, and fault injection.

The project is strongest as a small-scale self-hosted VPS monitoring and reliability-analysis platform. It is no longer only "can display node status"; it can collect, persist, analyze, evaluate, and explain node behavior.

## Major Changes In The Latest Upgrade

### Frontend Consolidation

The old duplicated `server/web` frontend was removed. `web/public/` is now the single source of truth for both the Go server dashboard and the Cloudflare Pages demo.

Why this matters:

- Fewer inconsistent UI bugs.
- Easier deployment.
- One place to maintain dashboard logic and styling.

### Dashboard Upgrade

The dashboard now includes:

- Fleet summary cards.
- Fullscreen world map.
- Animated node markers.
- Location-source distinction: manual, GeoIP, and server override.
- Event feed.
- Fleet radar.
- Operational reliability ledger.
- Experiment View.
- Node matrix.
- Link diagnostics.
- Ingress hotspots.
- Node detail panel with time-series charts, statistical summaries, events, history, links, live ingress, and persisted source summaries.

The map marker coordinates are no longer only "handwritten dots" conceptually. Agents report `location_source`, and the server can override coordinates centrally with `node_locations_path`.

### Backend API Upgrade

New and expanded endpoints:

- `GET /api/dashboard`: dashboard-wide payload including nodes, links, scores, events, hot sources, fleet analytics, reliability analytics, and ground-truth experiment evaluation.
- `GET /api/nodes/{id}/details`: node-specific history, metrics, links, connections, events, and analytics.
- `GET /api/events`: recent events.

The server now records:

- `events`: anomaly, status, and operational event stream.
- `connection_samples`: persisted ingress summaries.
- `metrics_raw`: historical metric window used for analytics.
- `location_source`: coordinate provenance.

### Statistical Analytics

The analytics layer now includes:

- Mean, median, min, max, p95, standard deviation.
- MAD and robust z-score.
- Slope per hour.
- Trend classification.
- Volatility classification.
- Baseline shift detection.
- EWMA deviation.
- Fleet-level radar.
- Node-level detail analytics.
- Operational reliability score.

Important distinction:

- `operational incidents` are status-change problems such as degraded/offline.
- `statistical signals` are anomaly-detection warnings outside labelled experiments.
- `experiment signals` are signals during labelled fault-injection windows.

This avoids over-counting every anomaly as a real incident.

### Fault Injection And Evaluation

The project includes a safe CPU-only fault-injection wrapper:

```bash
scripts/fault-injection.sh --ssh-host lisahost --node-id jp-lisahost --duration 150
```

It:

- Runs a low-priority CPU stress process with `nice`.
- Uses `timeout` so it exits automatically.
- Polls dashboard state.
- Writes CSV logs.
- Appends JSONL ground-truth labels locally and on the server.
- Feeds the dashboard Experiment View and analysis CLI.

Current live experiment baseline:

- 3 LisaHost CPU-only experiments.
- Detection rate: 100%.
- Mean detection delay: about 34 seconds.
- Mean recovery delay: about 24 seconds.

### Telegram Bot Upgrade

The bot now supports:

- `/status`: fleet status and nodes.
- `/analytics`: reliability, anomaly, and experiment summary.
- `/incidents`: active incident lifecycle state.
- `/ack <id>`: acknowledge a specific incident.
- `/silence <id> [30m|2h|1d]`: suppress one incident without muting the whole chat.
- `/events`: recent events.
- `/node <id-or-name>`: node detail summary.
- `/report`: daily AI report.
- `/mute [30m|2h|1d]`: pause proactive alerts for this chat.
- `/unmute`: resume proactive alerts.
- `/subscribe`: enable proactive alerts.
- `/unsubscribe`: disable proactive alerts for this chat.
- `/daily on|off`: toggle 09:00 UTC+8 analytics summary.
- `/prefs`: show per-chat preferences.

Preferences are persisted in `starnexus-bot-state.json` in the bot working directory.

### Deployment And Operations

Operational scripts now include:

- `scripts/deploy-server.sh`: primary deployment.
- `scripts/manage-node.sh`: interactive node management.
- `scripts/sync-agent.sh`: safe agent binary sync to existing nodes.
- `scripts/onboard-node.sh`: one-command new VPS onboarding.
- `scripts/fault-injection.sh`: labelled CPU-only experiments.
- `scripts/backup-db.sh`: consistent remote SQLite backup with optional local retention.
- `scripts/restore-db.sh`: guarded restore with service stop/start and API verification.
- `scripts/install-backup-cron.sh`: remote daily backup cron on the primary VPS.

Agents now have a disk-backed metric report queue:

- Default path: `./agent-queue.jsonl`.
- Default capacity: 2880 reports, about 24 hours at a 30-second interval.
- Queued reports keep original `collected_at` timestamps for historical analysis.
- Historical replay does not overwrite newer current metrics or create fresh status incidents.

Existing agent update:

```bash
./scripts/sync-agent.sh sonet lisahost
```

New node onboarding:

```bash
./scripts/onboard-node.sh \
  --primary dmit \
  --node sg-vps \
  --node-id sg-vps \
  --node-name "Singapore VPS" \
  --provider "Oracle" \
  --yes
```

## Live Deployment State

At the time this status document was written:

- Primary server: `dmit`.
- Monitored nodes: `tokyo-dmit`, `tokyo-sonet`, `jp-lisahost`.
- Server service: active.
- Bot service: active.
- Agents: active on all three nodes.
- Dashboard reports 3 nodes online.

The dashboard is accessed through an SSH tunnel to the Go server, with port `8900` kept private.

## Project Level Assessment

### Practical Engineering Level

For a personal VPS monitoring project, StarNexus is now above the typical hobby-dashboard level. It has real agents, real deployment, real persistence, real alerting, and real evaluation hooks.

It is closer to a compact production observability system than a static visualization demo.

### Graduation-Project Potential

This can support a strong undergraduate graduation project if framed correctly:

- Distributed system architecture.
- Time-series monitoring.
- Robust statistics for anomaly detection.
- Fault-injection evaluation.
- Reliability scoring.
- Real-world deployment on VPS nodes.
- Human-facing dashboard and Telegram operations interface.

The strongest academic angle is not "I made a dashboard"; it is:

> A self-hosted distributed VPS observability platform with robust statistical anomaly detection, labelled fault-injection evaluation, and operational reliability scoring.

That framing is defensible.

### "985 Level" Assessment

If "985 level" means a polished undergraduate engineering project from a strong university, the current project is already in that range for implementation depth and real deployment.

However, for a truly high-scoring academic submission, it still needs stronger formal evaluation and presentation:

- More labelled experiments.
- Clear comparison against baseline methods.
- Quantitative tables and charts.
- Better explanation of false positives and detection delay.
- Methodology document with assumptions and limitations.
- Cleaner packaging for new environments.

So the honest assessment is:

- Engineering prototype: strong.
- Real-world personal tool: already useful.
- Graduation project: viable.
- Polished top-tier submission: close, but needs more evaluation and write-up.

## Remaining Weak Spots

### Alert Calibration

The anomaly thresholds were made stricter, but they still need real-world calibration over several days.

Open questions:

- Do CPU-only experiments still trigger reliably after threshold tuning?
- Are bandwidth and connection signals too strict for small nodes?
- Should different node classes have different thresholds?

### Experiment Coverage

Current labelled experiments are CPU-only and limited to LisaHost.

Needed for stronger evaluation:

- More durations: 30s, 60s, 150s, 300s.
- More nodes.
- Memory pressure experiments on disposable nodes only.
- Safe network-latency experiments on test ports only.
- Synthetic connection burst experiments.

### Multi-Node Generalization

The code is mostly generic, but the operational defaults still assume:

- One primary server.
- SSH-accessible VPS nodes.
- Root/systemd deployment.
- Linux `/proc` metrics.
- SQLite on the primary server.

That is acceptable for this project, but should be documented as a scope boundary.

### Security Hardening

Port `8900` should stay private. Further hardening can include:

- Token rotation workflow.
- Stronger install-script validation.
- Optional mTLS or WireGuard overlay.
- Least-privilege systemd users.
- Backup failure alerting.

### Data Model Limits

SQLite is appropriate for this scale. If the system grows to many nodes or high-frequency samples, future work could add:

- Retention policy configuration.
- More efficient time-series aggregation.
- Export to Prometheus/OpenTelemetry.
- Optional PostgreSQL backend.

## Recommended Next Work

### 1. Evaluation Expansion

Add a repeatable experiment suite:

- `cpu_30s`, `cpu_60s`, `cpu_150s`, `cpu_300s`.
- Repeat each 3-5 times.
- Export detection delay and recovery delay.
- Generate a Markdown or CSV result table.

This gives the project strong evidence.

Also include non-experiment steady-state windows so false-positive rate can be reported per node-hour, not just as a raw event count.

### 2. Dashboard Analysis Polish

Add:

- Reliability trend over time.
- Signal distribution chart.
- Detection-delay chart in Experiment View.
- Per-node event density.
- Export button for analysis artifacts.

### 3. Telegram Bot Operations

Add:

- `/silence <node> <duration>`.
- `/watch <node>`.
- `/summary 24h`.
- Alert grouping to reduce spam.
- Incident lifecycle messages: opened, updated, recovered.

### 4. New Node UX

Improve `scripts/onboard-node.sh` with:

- Dry-run mode.
- Probe-target auto-generation.
- Optional exact coordinate input.
- Rollback on install failure.
- Support for non-root install.

### 5. Academic Write-Up

Add or keep expanding:

- `docs/METHOD.md`: architecture, metrics, robust statistics, reliability score.
- `docs/RESULTS.md`: experiment results and interpretation.
- `docs/LIMITATIONS.md`: scope, assumptions, risks.
- `docs/ROADMAP.md`: maintained execution plan.

## Suggested Project Pitch

Short version:

> StarNexus is a self-hosted distributed VPS observability platform. It combines lightweight Go agents, a private Go/SQLite control plane, a live geographic dashboard, Telegram operations, robust statistical anomaly detection, and labelled fault-injection evaluation.

More academic version:

> This project studies practical reliability monitoring for small distributed VPS fleets. It implements end-to-end telemetry collection, historical persistence, robust anomaly detection using median/MAD and baseline-shift analysis, operational reliability scoring, and ground-truth evaluation through controlled fault injection.

## Current Verdict

StarNexus is in a good state. The next improvement should not be another random dashboard widget. The highest-value direction is measurable reliability:

- More labelled experiments.
- Better result tables.
- Cleaner thresholds.
- Stronger docs.
- More robust onboarding.

That path makes the project both more useful day to day and easier to defend as a serious technical project.
