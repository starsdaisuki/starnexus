# StarNexus Architecture

Last updated: 2026-04-22

A concise architectural tour with design-choice footnotes. Intended for
reviewers who want to understand **why** the system is shaped the way it
is, not just what it does. Pairs with `docs/LIMITATIONS.md` (what we
gave up) and `docs/REPRODUCIBILITY.md` (how to re-derive every number in
`docs/RESULTS.md`).

---

## 1. Topology

Three Go services + one static-site dashboard + one optional Telegram
bot:

```
┌──────────┐   30 s reports   ┌─────────────┐    polled   ┌──────────┐
│  agent   │ ───────────────▶ │   server    │ ◀────────── │   bot    │
│ (per VPS)│    HTTPS + token │ (control    │  /status    │ (Telegram│
└──────────┘                  │   plane)    │  /incidents │  polling)│
                              └──────┬──────┘             └──────────┘
                                     │
                              ┌──────▼─────┐  static  ┌─────────────┐
                              │  SQLite    │◀────────▶│  web UI     │
                              │  (single   │  /api    │  (bundled   │
                              │   file)    │          │   with srv) │
                              └────────────┘          └─────────────┘
```

- **agent** (`agent/`): 150-line Go binary, reads `/proc` every 30 s,
  POSTs to the server. Queues to a local SQLite file when the server is
  unreachable and replays on reconnect. Linux-only; `/proc` is hard-coded.
- **server** (`server/`): single-writer Go binary. Owns the database,
  runs the analytics scheduler, serves the dashboard, and exports
  Prometheus `/metrics`. Everything the operator interacts with flows
  through this one process.
- **bot** (`bot/`): polling-based Telegram client. Calls
  `GET /api/status` and `GET /api/incidents` on a heartbeat; sends
  notifications on state changes. Deliberately simple — no webhooks.
- **web UI** (`web/public/`): vanilla JS + CSS served as static assets
  either by the Go server (production) or by Cloudflare Pages (demo).
  The same HTML/JS talks to two different backend shapes (real Go API
  vs. Pages Functions + D1 mock) and feature-detects fields.

### Why three binaries instead of one

The agent is the only component that needs to run on every monitored
node. Pushing server logic into that binary would mean shipping the
database driver, the analytics scheduler, and the HTTP stack to every
VPS — wasteful when all we need is a /proc reader.

The bot is split out because Telegram API credentials are strictly
operator data. Keeping it in its own process means the server never
holds Telegram tokens in memory and operators can rotate bot
configuration without restarting the control plane.

---

## 2. Data Plane

### Transport

- **Protocol**: HTTP/1.1 + JSON. No gRPC, no protobuf, no msgpack.
- **Auth**: static bearer token shared across agents, passed as
  `Authorization: Bearer <token>` on every agent→server request.
- **Content-Type**: `application/json` for reports, standard multipart
  form encoding for Telegram API.

We considered protobuf but the whole telemetry payload is ≈400 bytes.
Parsing cost of JSON is noise relative to the SQLite write and the
serialization-layer boundary benefits disappear when the two endpoints
are both Go anyway.

### Persistence

Single SQLite file (`starnexus.db`) opened through
`modernc.org/sqlite` (pure Go, no cgo). Configuration:

```go
SetMaxOpenConns(1)     // serialise all writes
busy_timeout = 5000    // PRAGMA: 5 s retry on lock contention
synchronous = NORMAL   // PRAGMA: fsync per transaction, not per write
journal_mode = WAL     // PRAGMA: concurrent reads during writes
```

#### Why SQLite over Postgres

- **Zero operations burden.** A VPS-monitoring product that itself needs
  an operator-managed database defeats the purpose.
- **Fleet size is bounded.** Single-digit to low-hundreds of nodes × 30 s
  cadence = ≤20 writes/s steady-state. The single-writer SQLite ceiling
  (measured 1000 writes/s on M1) is ~50× over requirement.
- **Pure-Go driver ships with the binary.** `modernc.org/sqlite` needs
  no C toolchain, no libpq, no sidecar. `go build` produces a static
  amd64 executable that drops into `/usr/local/bin`.

#### Why `SetMaxOpenConns(1)`

SQLite serialises writers regardless of pool size. A pool of 10 write
connections would produce 10× the contention without any throughput
gain — all writers block on the same file lock. A pool of 1 makes that
contention explicit: the server visibly becomes request-bound, not
mysterious-500-bound, under overload.

Reads use the same pool, which is conservative. We accept the limit
because read workloads are already tiny (dashboard polls every 30 s).

---

## 3. Control Plane

### Analytics scheduler

`server/internal/analytics/scheduler.go` runs every 300 s. Each tick:

1. Pulls the last 24 h of metrics per node from SQLite.
2. Calls `analytics.BuildDetailAnalytics(points, 24)` to compute per-metric
   robust z, MAD, EWMA, baseline shift, trend, and volatility.
3. Feeds the result through `policyForMetric` gates to decide whether to
   emit anomaly events.
4. Writes derived reports into the `reports` table for the dashboard to
   read.

The `BuildDetailAnalytics` output is what powers the Detector Internals
panel in the UI — the same struct is returned by
`GET /api/nodes/{id}/details`.

### Ingest path (hot)

`POST /api/report` (agent) → validate token → `UpsertReport` → insert
metric row + update node status + trigger synchronous anomaly check for
CPU/memory threshold.

The threshold path fires *inline* on every report — necessary because
the 300 s scheduler cannot catch 30-second spikes. The robust/baseline
detectors run out-of-band on the scheduler tick.

### Metrics export

`/metrics` serves zero-dependency Prometheus text exposition. Counters,
gauges, and summaries are maintained in
`server/internal/metrics/metrics.go` with atomic-float-via-bits writes.
Cardinality is capped at `MaxLabelsPerMetric = 10000` per metric vector;
overflow increments a `starnexus_metrics_dropped_labels_total` counter
instead of unbounded growth.

The refresh that rebuilds gauge values (node counts, incident counts)
from DB reads is guarded by a 20 s TTL cache. Scrapers hitting
`/metrics` five times a second do not each issue five DB queries.

---

## 4. Detector Catalogue

The benchmark driver in `cmd/starnexus-bench` replays 7 detectors against
the same metric history and labelled experiments. Each detector was
picked to span the space of "what one might use":

| Detector | Family | Role in the comparison |
|---|---|---|
| `fixed_threshold` | Static rule | Nagios/Zabbix baseline — no statistics |
| `plain_zscore` | Rolling moment | Textbook non-robust z — expected weakness |
| `ewma` | Control chart | Shewhart/EWMA school — adapts to drift |
| `cusum` | Changepoint | Page (1954) — the classical changepoint baseline |
| `mahalanobis` | Multivariate (diagonal) | Simple multivariate composite |
| `mcd_mahalanobis` | Multivariate (full cov, MCD) | Principled multivariate with cross-metric correlation |
| `robust_shift` | Robust z + shift | Production surrogate (what StarNexus does live) |

The catalogue is intentionally **not** the state of the art. It is a
**comparison set** that lets a reviewer answer "did StarNexus's choice
of detector actually do better than the obvious alternatives?"

### Why diagonal Mahalanobis first, MCD-Mahalanobis second

Diagonal Mahalanobis (`Σ_ij = 0` for `i≠j`) is estimate-efficient on
small windows (`k` samples per metric suffice; full rank needs `k²`
well-conditioned pairs). It is the correct first step when you have
four metrics and a 200-sample window.

MCD-Mahalanobis adds cross-metric correlation and a concentration step
to resist outliers in the covariance estimate. It is the principled
upgrade when you want to catch "two metrics moving together in a way
that is individually unremarkable but jointly anomalous." Computational
cost is one O(p³) matrix inverse per `RefitEvery=30` samples, which is
cheap at p=4.

Fast-MCD's full multi-start loop was rejected as over-engineering for
p=4: a single-start concentration on the coordinate-wise median
converges in 2–3 iterations on VPS load data with a trace-proportional
ridge regularisation (`cov += trace/p · 1e-3 · I`) to stay invertible
when two metrics are highly correlated (e.g. bandwidth ↔ connections
during a traffic spike).

### Why CUSUM as a changepoint baseline

The production `robust_shift` detector uses a recent-vs-baseline median
comparison. This is a pragmatic approximation, not a changepoint test.
CUSUM (Page 1954) is the canonical alternative: explicitly detect the
sample at which a process's mean changed.

Adding CUSUM to the benchmark answers "how much does it matter that
`robust_shift` isn't a proper changepoint detector?" The expected
answer is "very little, because robust_shift trades detection-delay
optimality for simplicity of the gate logic" — and the benchmark
numbers can confirm or refute it.

### Parameter defaults are documented, not tuned

Each detector's constructor exposes its parameters as struct fields. The
defaults in `Newxxx()` are the values used in `docs/RESULTS.md`. They
were set from textbook or prior-art recommendations (CUSUM K=0.5 H=5;
Mahalanobis composite ≥4.2σ matching χ²_{0.998, 4}) — *not* tuned on
the labelled set. This avoids the "trained on the test" fallacy.

### Comparability contract

Every detector returns `[]SyntheticEvent` via the same `Detector`
interface. `ToDBEvent()` projects each synthetic event onto the same
`db.Event` struct the live evaluator consumes, so `BuildGroundTruthEvaluation`
scores all seven detectors with identical code paths.

Per-experiment diagnostics (score at firing, metric that tripped,
samples in the window at fire time) are emitted in `benchmark.json` and
`per_experiment.csv` so a reviewer can audit *why* a detector fired —
not just that it fired.

---

## 5. Scheduler Contract

The anomaly scheduler promises three properties that the rest of the
system relies on:

1. **At-least-once.** Every 300 s tick, every node with ≥
   `minDataPoints` samples in the last 24 h is evaluated. Missing a tick
   (because the server restarted, say) means double-work on the next
   tick, not skipped nodes.
2. **Idempotent writes.** The scheduler only inserts *new* events. An
   already-open anomaly will not produce a second row — state
   transitions (firing → recovered) are edge-triggered on the DB state,
   not re-emitted every tick.
3. **No silent failure.** Scheduler errors log at WARN and increment
   `starnexus_scheduler_errors_total`. The operator sees them via
   `/metrics` or the Prometheus exporter.

The inline threshold path (fires on every /report) is deliberately
simple: if CPU > 80 or memory > 90 in the last two consecutive samples,
open an `anomaly` event. This is the only place StarNexus knowingly
mirrors Nagios.

---

## 6. Deployment Topologies

| Topology | Who it's for | How |
|---|---|---|
| **Bare binary** | Single operator, one control-plane VPS | `make build-all`, `scp bin/*` to VPS, `./starnexus-server` |
| **docker-compose** | Reviewers, local evaluators | `docker-compose up`, bundled SQLite volume + 3 agents in containers |
| **Cloudflare Pages** | Public demo, no cold paths | Static `web/public/` + Pages Functions + D1 mock |
| **Bot split** | Operators who want mobile alerts | Bot runs on separate VPS, polls server over SSH tunnel |

The first three share the same `web/public/` frontend. The demo
detects absence of real endpoints (e.g. `/metrics`) and hides
control-plane-only panels. This is why the frontend is vanilla JS +
fetch: no hydration, no build step, no reason for the Pages demo to
diverge from the production UI.

### Reproducing the live demo

```bash
cd web
pnpm install
pnpm run deploy   # wrangler pages deploy public
```

The `pages_build_output_dir = "public"` in `web/wrangler.toml` tells
wrangler where the assets are *and* implicitly which sibling
`functions/` directory to auto-detect for Pages Functions. Using
`wrangler pages deploy .` **breaks** this because wrangler then treats
the entire repo root as static assets and skips the functions
directory — the demo gives an `API sync failed` banner.
`docs/REPRODUCIBILITY.md` §9 has the full walkthrough including the
D1 schema bootstrap commands.

---

## 7. Trust Boundaries

| Boundary | Defence |
|---|---|
| agent → server | Bearer token on every request; TLS termination expected at the SSH tunnel or reverse proxy |
| server → SQLite | Parameterised queries only; no string-concat SQL anywhere |
| bot → server | Server-side polling only; bot never exposes a listening socket |
| web → server | Same-origin; server serves both API and assets |
| operator → server | SSH tunnel + token. No password auth, no web-session auth |

The install script (`curl http://<server>:8900/install.sh | bash`)
downloads over plain HTTP — documented in `docs/LIMITATIONS.md §Security`.
For reproducible deployments, prefer building from source via `make
build-agent` and scp'ing the binary.

---

## 8. What This Architecture Is Not

- **It is not a metrics warehouse.** Prometheus, InfluxDB, and
  TimescaleDB all beat SQLite at long-term storage and aggregation.
  StarNexus's retention is pragmatic (7 d raw + aggregates), not
  competitive.
- **It is not a distributed system.** One server process, one writer,
  one SQLite file. Horizontal scale-out is an explicit non-goal.
- **It is not an incident management platform.** There is no paging
  policy engine, no on-call rotation, no escalation ladders. The bot
  notifies a chat; the rest is a human operator.

The design point is "smallest thing that gives a single operator
honest visibility into a small fleet, with a statistically defensible
evaluation story." Everything bigger is a different product.
