# StarNexus Results

Last updated: 2026-04-21

This document records the current deployment and experiment results. Numbers here are a snapshot, not a permanent benchmark.

## Deployment Snapshot

Current production fleet:

- `dmit`: primary server, bot, and agent.
- `sonet`: secondary monitored VPS.
- `lisahost`: monitored VPS used for safe CPU-only fault injection.

As of the latest verification:

- Fleet status: 3 total, 3 online, 0 degraded, 0 offline.
- Server API: active.
- Telegram bot service: active.
- Agents on `dmit`, `sonet`, and `lisahost`: active.
- Incident API: enabled and returning active incident lifecycle state.

## CPU Fault-Injection Experiments

The current labelled experiment set contains three LisaHost CPU-only trials.

| Experiment | Duration | Detected | Detection Delay | Recovered | Recovery Delay | Peak CPU | Detection Event |
|---|---:|---:|---:|---:|---:|---:|---|
| `jp-lisahost-20260421T045458Z-cpu` | 150s | yes | 27s | yes | 27s | 100% | `Node degraded` |
| `jp-lisahost-20260421T062448Z-cpu` | 60s | yes | 37s | yes | 37s | 100% | `Node degraded` |
| `jp-lisahost-20260421T153933Z-cpu` | 90s | yes | 37s | yes | 7s | 100% | `Node degraded` |

Aggregate dashboard evaluation:

- Experiments: 3.
- Detection rate: 100%.
- Recovery rate: 100%.
- Status-threshold detections: 3.
- Anomaly-first detections: 0.
- Mean detection delay: 33.7 seconds.
- Mean recovery delay: 23.7 seconds.
- False-positive event count outside labelled windows: 31 in the active dashboard window, split into 2 status events and 29 anomaly events.

## Incident Lifecycle Check

During the 90-second CPU test, the new incident layer behaved as expected:

- `node_degraded` opened when LisaHost crossed status thresholds.
- `metric_anomaly` opened when the anomaly scheduler observed CPU outlier pressure.
- `node_degraded` recovered after LisaHost returned online.
- `metric_anomaly` recovered on the next 5-minute anomaly scheduler pass after the signal disappeared.

This confirms the intended split between fast status lifecycle and slower statistical anomaly lifecycle.

## Interpretation

The current result is strong for an operational prototype:

- The system detects obvious CPU pressure quickly.
- Recovery is visible without manual intervention.
- Experiment labels are connected to dashboard evaluation.
- The incident lifecycle is now operationally actionable instead of being only an event feed.

The result is not yet a full statistical validation:

- `n=3` is too small for a defensible performance claim.
- All labelled trials are CPU-only.
- All labelled trials are on one VPS.
- Detection is currently dominated by status-threshold events, not only robust anomaly detection.
- The false-positive count is still high because historical anomaly/status events outside labelled windows are counted in the dashboard window.

## Next Evaluation Work

Recommended next trials:

- Repeat 60s, 90s, 150s, and 300s CPU experiments on LisaHost.
- Add at least one second node after confirming it will not affect user traffic.
- Separate threshold-based detection from anomaly-based detection in `RESULTS.md`.
- Add a table for incident lifecycle timing: opened, acknowledged/suppressed if tested, recovered.
- Keep network and memory fault injection out of the default workflow until rollback safety is stronger.
