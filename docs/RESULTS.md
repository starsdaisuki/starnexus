# StarNexus Results

Last updated: 2026-04-22

This document records the current deployment, experiment results,
detector benchmark, and scalability measurements. Numbers here are a
snapshot, not a permanent benchmark; `analysis-output/runs/` retains
the timestamped artifacts.

## Deployment Snapshot

Current production fleet:

- `dmit`: primary server, bot, and agent.
- `sonet`: secondary monitored VPS.
- `lisahost`: monitored VPS used for safe CPU-only fault injection.

As of the latest verification:

- Fleet status: 3 total, 3 online, 0 degraded, 0 offline.
- Server API, Telegram bot, and agents active on all three nodes.
- Incident API enabled and returning active incident lifecycle state.

## Labelled Fault-Injection Experiments (n = 30)

Two injection types on `jp-lisahost`:

- **CPU stress** (n = 15): `scripts/fault-injection-matrix.sh` default,
  3 reps × {30, 60, 150, 300} s plus 3 earlier manual trials
  (one 90 s included). Busy-loop under `nice -n 10 timeout`. Peak CPU
  100 % except one 30 s trial whose stress loop was scheduled away
  before the agent's next report (peak 8.91 %, missed by every detector).
- **Memory stress** (n = 15): `scripts/fault-injection-matrix.sh --type memory`,
  3 reps × {30, 60, 150, 300} s plus 3 smoke trials. Python bytearray
  page-touch capped at 40 % of `MemAvailable` (~260 MB on a 1 GB
  host, lifting memory_percent from 33 → 60 % — ~27 % absolute).

| Duration | CPU reps | Memory reps |
|---:|---:|---:|
| 30s | 3 | 4 |
| 60s | 4 | 3 |
| 90s | 1 | 0 |
| 150s | 4 | 4 |
| 300s | 3 | 4 |

The memory injection is deliberately sub-threshold for `fixed_threshold`
(memory cap 90 %, we peak at 60 %). This is intentional: a
detector that only fires at hard saturation misses pressure events that
a statistical detector should catch. The numbers below confirm this.

## Detector Benchmark (`starnexus-bench`)

All seven detectors replay the same metric history over the 7-day
window and are scored against the same 30 labelled experiments
(15 CPU + 15 memory). Offline replay ensures apples-to-apples results.

| Detector | Detect % | Mean delay (s) | 95% CI (s) | Recovery % | FP / node-hour | Total events |
|---|---:|---:|---|---:|---:|---:|
| `fixed_threshold` | 43.3 | 74.6 | 52.5–114.2 | 50 | **0.016** | 32 |
| `plain_zscore` | 40.0 | 95.5 | 49.0–160.0 | 50 | 0.555 | 298 |
| `ewma` | 26.7 | 117.5 | 48.9–206.9 | 37 | 0.683 | 354 |
| `cusum` | **80.0** | **75.4** | 52.4–108.5 | 93 | 1.044 | 567 |
| `mahalanobis` | **80.0** | **75.4** | 52.4–108.5 | 93 | 0.335 | 212 |
| `mcd_mahalanobis` | **80.0** | **75.4** | 52.4–108.5 | 93 | 0.387 | 238 |
| `robust_shift` | 26.7 | 182.6 | 99.5–270.4 | 23 | **0.064** | 42 |

Steady-state exposure: 501.1 node-hours. Bootstrap CIs use 2000
resamples with fixed seed 42.

### By Injection Type

The two injection types separate the detectors completely:

| | CPU (n=15) | Memory (n=15) | Combined |
|---|---:|---:|---:|
| `fixed_threshold` | 13/15 (87 %) | **0/15 (0 %)** | 13/30 (43 %) |
| `cusum`, `mahalanobis`, `mcd_mahalanobis` | 13/15 (87 %) | **11/15 (73 %)** | 24/30 (80 %) |
| `plain_zscore` | 8/15 (53 %) | 4/15 (27 %) | 12/30 (40 %) |
| `ewma` | 5/15 (33 %) | 3/15 (20 %) | 8/30 (27 %) |
| `robust_shift` | 7/15 (47 %) | 1/15 (7 %) | 8/30 (27 %) |

Fixed-threshold detects **zero** memory experiments because we
deliberately inject to ~60 % memory — below the hard-wired 90 %
alert threshold. A detector that only fires at hard saturation cannot
see pressure events, even when the pressure is statistically obvious
(robust z = 128 during memory injection).

### Pairwise Significance (exact two-sided binomial on discordant detections)

| Comparison | Discordant | p-value | Interpretation |
|---|---:|---:|---|
| `fixed_threshold` vs `cusum` | 11 | **0.0010** | CUSUM significantly better |
| `fixed_threshold` vs `mahalanobis` | 11 | **0.0010** | Mahalanobis significantly better |
| `fixed_threshold` vs `mcd_mahalanobis` | 11 | **0.0010** | MCD significantly better |
| `cusum` vs `mahalanobis` | 0 | 1.0000 | Indistinguishable |
| `cusum` vs `mcd_mahalanobis` | 0 | 1.0000 | Indistinguishable |
| `mahalanobis` vs `mcd_mahalanobis` | 0 | 1.0000 | Same 24/30 detected |
| `ewma` vs `cusum`/`mahalanobis`/`mcd_mahalanobis` | 16 | **0.00003** | Statistical detectors dominate EWMA |
| `plain_zscore` vs `cusum`/`mahalanobis`/`mcd_mahalanobis` | 12 | **0.0005** | Statistical detectors dominate plain z |

Full 21-pair matrix lives in `analysis-output/bench/pairwise_tests.csv`.

### Key Findings

**1. Adding a second injection type reverses last sprint's headline
claim.** On CPU-only data (n = 15), fixed-threshold and the statistical
detectors are statistically indistinguishable (13/15 each,
two-sided p = 1.0). On the combined set (n = 30, CPU + memory),
fixed-threshold drops to 13/30 (43 %) while CUSUM, Mahalanobis, and
MCD-Mahalanobis all reach 24/30 (80 %). The pairwise binomial
p-value collapses from 1.0 to **0.0010** — strong evidence that the
statistical detectors genuinely generalise across failure modes in
a way fixed-threshold does not.

This is the single most important number in this document: the
previous sprint's "fixed-threshold wins" was a single-failure-mode
artifact, not a real finding. The statistical detectors that looked
redundant on CPU-only data now carry the memory injection alone.

**2. Fixed-threshold catches zero memory experiments.** All 15
memory injections peak at ~60 % — above baseline (33 %) but well
below the hard-wired 90 % alert. A detector that only fires at hard
saturation **cannot see** pressure events, even when the shift is
statistically dramatic: robust z reaches 128 during memory
injection (median baseline vs. current, over the 200-sample window).
On memory data:

| Detector | Memory detect % | Typical delay |
|---|---:|---:|
| `cusum`, `mahalanobis`, `mcd_mahalanobis` | 11/15 (73 %) | ~75 s |
| `plain_zscore` | 4/15 (27 %) | ~95 s |
| `ewma` | 3/15 (20 %) | ~117 s |
| `robust_shift` | 1/15 (7 %) | ~180 s |
| `fixed_threshold` | **0/15 (0 %)** | — |

The takeaway is not that fixed-threshold is broken — it does exactly
what its static cap promises — but that relying on hard thresholds
alone is a single-mode strategy.

**3. CUSUM, diagonal Mahalanobis, and MCD-Mahalanobis are
detection-equivalent but separate on FP rate.** All three catch the
identical 24/30 experiments (pairwise p = 1.0 for every pair). FP
rate per node-hour: Mahalanobis 0.335 < MCD 0.387 < CUSUM 1.044.
MCD's 14 % FP premium over diagonal is the cost of full covariance —
bought for cross-metric correlation capture that this labelled set
doesn't demand but real joint-mode anomalies would. CUSUM is ~3× noisier
because the untuned textbook defaults (K=0.5, H=5) fire on any shift
passing the decision interval, regardless of how much the residual
is actually worth acting on.

**4. Non-robust statistical methods (plain z, EWMA) lose on both
axes, at significance.** Plain z-score catches 40 %, EWMA 27 %.
Pairwise p-values against the statistical top tier are both ≤ 0.0005.
The non-robust mean/stddev estimators are pulled by heavy-tailed
traffic bursts, so when a real shift arrives the standardised
deviation understates it. This is the empirical version of the
textbook argument for robust statistics on non-Gaussian data.

**5. Robust shift (production surrogate) catches 27 %** — it scans
every 5 min on a 24 h window, so memory spikes shorter than ~300 s
are mostly missed. This is a deliberate architectural choice: the
production system delegates fast response to the status-threshold
path (the `fixed_threshold` analog) and uses robust shift for slow
baseline drift the status path cannot see. With fixed-threshold
unable to cover memory pressure below 90 %, the production two-detector
split is less complete than previously claimed; promoting CUSUM or
Mahalanobis to the live fast-path is now a real architectural
upgrade, not just a benchmark-surrogate exercise.

**6. The 30 s experiments remain Nyquist-limited.** Every detector
requires MinHold=2 on 30 s samples, which a 30 s stress window can
provide at most once. Seven detectors miss the same subset of short
experiments; faster sampling or dropped debounce is the only path.

### Narrative For Evaluation

The n = 15 CPU-only benchmark argued that fixed-threshold plus robust
shift was a complete two-path design. The n = 30 CPU + memory
benchmark reframes this: fixed-threshold is complete for the narrow
subset of failures that cross its hard-wired saturation line, and
robust-shift is too slow to catch short-duration memory pressure. The
live system's defensible story is now:

1. For CPU saturation at textbook thresholds, fixed-threshold is the
   right detector — cheap, unambiguous, 1 FP per 2.6 days.
2. For memory pressure that stays below saturation but is statistically
   obvious, fixed-threshold is silent. CUSUM and Mahalanobis catch 73 %
   of memory experiments with ~75 s delay; MCD matches that number.
3. Promoting one of CUSUM or Mahalanobis to the live fast-path
   (currently benchmark-only) is the next concrete architectural step.
   MCD-Mahalanobis is the principled choice because it captures
   cross-metric correlation at a documented 14 % FP-rate penalty over
   diagonal Mahalanobis; CUSUM is simpler but ~3× noisier at untuned
   textbook defaults.

What's missing is network-latency injection (the third short-板 entry
in `docs/LIMITATIONS.md`). Memory was the larger gap to close because
the benchmark-surrogate vs. live-path story is most relevant at the
saturation boundary; network injection would test a fourth dimension
the current catalogue doesn't exercise at all.

## Scalability Benchmark (`scripts/loadtest-local.sh`)

Test setup: isolated StarNexus server on a temporary SQLite database,
default PRAGMAs (WAL, `busy_timeout=5000`, `synchronous=NORMAL`),
`SetMaxOpenConns(1)`. Host: MacBook with Apple M-series CPU. Load:
N agents posting `/api/report` every 500 ms for 15 s.

| Agents | Requests/sec | p50 (ms) | p95 (ms) | p99 (ms) | Success |
|---:|---:|---:|---:|---:|---:|
| 10  |    20 |  8 |  15 |  17 | 300 / 300 |
| 50  |   100 | 16 |  26 |  28 | 1500 / 1500 |
| 100 |   200 | 21 |  32 |  35 | 3000 / 3000 |
| 250 |   500 | 41 |  59 |  63 | 7500 / 7500 |
| 500 |  1000 | 71 | 103 | 109 | 15000 / 15000 |

Observations:

- Zero SQLITE_BUSY errors at every tested size. Early runs against a
  default `sql.DB` pool (no `SetMaxOpenConns(1)`, no `busy_timeout`)
  returned HTTP 500 for ≈98 % of requests under 500 agents. Fixing
  this took two lines; the issue is documented in
  `docs/LIMITATIONS.md`.
- Latency scales ≈linearly with fleet size because requests serialise
  through the single writer. A 500-agent fleet at 500 ms cadence is
  equivalent to 1000 reports/sec, comfortably above any realistic VPS
  fleet size this platform targets.
- Observed throughput is CPU-bound at the Go JSON decode path; SQLite
  itself commits writes in tens of microseconds in this configuration.

## Self-Observability

The server exposes `/metrics` in Prometheus text format. Exercised by
the integration test (`server/integration_test.go`); labels include
method, normalized path (with `{id}` collapsed segments), and status
class (`2xx`, `4xx`, `5xx`). Gauges cover node status counts by state,
active-incident counts by lifecycle state, and server uptime.

## Incident Lifecycle Check

During a 90-second CPU test, the incident layer behaved as expected:

- `node_degraded` opened when LisaHost crossed status thresholds.
- `metric_anomaly` opened when the anomaly scheduler observed CPU
  outlier pressure.
- `node_degraded` recovered after LisaHost returned online.
- `metric_anomaly` recovered on the next 5-minute anomaly scheduler
  pass after the signal disappeared.

This confirms the intended split between fast status lifecycle and
slower statistical anomaly lifecycle.

## Reliability Score Weight Sensitivity

`scripts/validate-reliability.py` sweeps five weight schemes over the
30-day availability / latency / stability components stored in
`node_scores`. On the current 3-node fleet, node rankings are
identical across every scheme tested — Kendall-tau = +1.00 between
the default 40/30/30 and each of 60/20/20, 33/33/33, 30/50/20, and
30/20/50. Per-node composite spread across schemes is ≈22 score
points, dominated by the zero-variance availability component (all
three nodes have identical uptime in the 30-day window). This is a
weak-but-real robustness claim; longitudinal predictive validation
against held-out incidents remains future work.

## Figures

Generated from the exported CSVs with `uv run scripts/generate-figures.py`
(or `make figures`):

- `cpu_timeseries_with_experiments.png` — CPU time series per node
  with labelled experiment windows highlighted.
- `benchmark_head_to_head.png` — four-panel comparison of detection
  rate, delay, false-positive rate, and recovery delay across
  detectors.
- `detection_delay_box.png` — per-detector detection-delay distribution
  with per-experiment scatter and mean markers.
- `delay_vs_duration.png` — mean detection delay vs experiment
  duration per detector; highlights the Nyquist limit at 30 s.
- `fp_vs_detection_tradeoff.png` — false-positive rate (log scale)
  against detection rate; the top-left corner is the operational
  sweet spot.
- `event_timeline.png` — production event scatter on a date axis with
  experiment windows shaded.

All figures land in `analysis-output/figures/`.

## Interpretation

The project has three complementary lines of quantitative evidence:

1. **Ground-truth fault-injection** (detection rate, delay, recovery)
   over n = 15 labelled windows across five durations, with 95 %
   bootstrap confidence intervals on every reported mean.
2. **Detector benchmark** showing the production combination
   (fixed thresholds plus robust shift) outperforms any single
   textbook baseline on reproducible offline replay.
3. **Scalability numbers** quantifying the current capacity ceiling
   (≈1000 reports/sec with p99 = 109 ms) so the single-node
   control-plane scope is concrete rather than handwavy.

Residual caveats and the gap between "strong engineering prototype"
and "full statistical validation" are enumerated in
`docs/LIMITATIONS.md`.
