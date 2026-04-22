# Reproducibility Guide

Last updated: 2026-04-22

Every quantitative claim in `docs/RESULTS.md` is re-derivable from the
artifacts in this repo plus a small number of commands. This document
lists each claim, the exact command that regenerates it, the
seed/configuration assumptions, and the expected wall-clock duration.

If any reported number in RESULTS.md cannot be reproduced from here,
treat that as a documentation bug and open an issue.

---

## 1. Environment Assumptions

| What | Tested version | Why it matters |
|---|---|---|
| Go | 1.22+ (CI pins 1.23) | `net/http` method routing; `math/rand/v2` |
| SQLite driver | `modernc.org/sqlite` pinned in `server/go.mod` | Pure-Go, WAL-mode with `busy_timeout=5000` |
| Python | 3.11+ | `uv`-managed; figure scripts use PEP 723 inline deps |
| `uv` | 0.11+ | Needed for `uv run scripts/*.py` |
| Node / pnpm | Node 20, pnpm 9 | Only required for the Cloudflare Pages demo |
| jq | 1.6+ | `scripts/fault-injection.sh` uses it to build JSON labels safely |
| Docker | optional | Sandbox only, not required for reproduction |
| OS | macOS arm64 (dev) / linux amd64 (CI, production) | Agent is linux-only (`/proc`) |

Clean checkout + first build takes ~2 min on an M-series Mac (`make build-all`).

---

## 2. Detector Benchmark (Table in `docs/RESULTS.md`)

### Inputs

- A SQLite backup of the production DB with at least one week of raw
  metrics. Pull with `scripts/backup-db.sh` or copy from a running
  server.
- A labels file `experiments.jsonl` with at least one labelled
  experiment window. The n=15 result used in RESULTS.md is produced by
  the matrix described in §3.

### Command

```bash
make bench          # wraps: starnexus-bench -db ./starnexus.db \
                    #                      -schema ./schema.sql \
                    #                      -out ../analysis-output/bench \
                    #                      -experiments ../analysis-output/experiments.jsonl \
                    #                      -hours 168 -seed 42
```

Or directly:

```bash
cd server && go run ./cmd/starnexus-bench \
  -db <backup.sqlite> \
  -schema ./schema.sql \
  -out ../analysis-output/bench \
  -experiments <experiments.jsonl> \
  -hours 168 \
  -seed 42
```

### Outputs

- `analysis-output/bench/benchmark.json` — full structured result
- `analysis-output/bench/benchmark.csv` — one row per detector
- `analysis-output/bench/per_experiment.csv` — one row per (detector, experiment)
- `analysis-output/bench/pairwise_tests.csv` — paired significance tests
- `analysis-output/bench/report.md` — human-readable summary

### Seeds

- `--seed 42` (default) fixes the bootstrap LCG state so the 95 % CI
  values are deterministic on identical inputs.
- Point estimates (detection %, mean delay, FP rate) are seed-independent.
- Re-run with `--seed 12345` to confirm CI widths move slightly and
  point estimates do not.

### Duration

~5 seconds on an M-series Mac for a 7-day × 3-node dataset.

---

## 3. Labelled Fault-Injection Matrix (n=15)

### Inputs

- SSH access to a test node (default `lisahost` in the wrapper).
- StarNexus server reachable over SSH for label persistence.
- `jq` installed locally.

### Command

```bash
./scripts/fault-injection-matrix.sh \
  --ssh-host lisahost \
  --node-id jp-lisahost \
  --server-ssh dmit \
  --reps 3 \
  --durations "30 60 150 300" \
  --gap 120
```

Adds 3 reps × 4 durations = 12 new labelled experiments to the server
path `/root/starnexus/analysis-output/experiments.jsonl` plus the local
copy. Combined with the three earlier manual trials, this yields n=15.

### Duration

~70 min wall-clock. Each experiment runs `nice -n 10 timeout <dur>s`
on the test node with a 120 s gap between trials.

### Safety

- CPU-only (no memory pressure, no firewall rules, no tc-netem).
- `nice` + `timeout` means the stress is low-priority and auto-cleans.
- Do not aim the matrix at a production node serving user traffic.

---

## 4. Figures (`analysis-output/figures/` and `docs/figures/`)

### Command

```bash
make figures        # wraps: uv run scripts/generate-figures.py
```

Or directly:

```bash
uv run scripts/generate-figures.py \
  --analysis analysis-output/latest \
  --bench analysis-output/bench \
  --out analysis-output/figures
```

`uv` installs matplotlib + pandas + numpy on demand via PEP 723
inline-dependency metadata; no venv setup required.

### Outputs (PNG)

- `cpu_timeseries_with_experiments.png`
- `benchmark_head_to_head.png`
- `detection_delay_box.png`
- `delay_vs_duration.png`
- `fp_vs_detection_tradeoff.png`
- `event_timeline.png`

The `docs/figures/` copies (tracked in git, referenced from README.md
and `docs/RESULTS.md`) are regenerated from the same artifacts — copy
the PNGs over manually after running the script.

### Duration

~3 s.

---

## 5. Scalability Benchmark (Table in `docs/RESULTS.md`)

### Command

```bash
./scripts/loadtest-local.sh                 # defaults: 15s × {10,50,100,250,500} agents
DURATION=45s INTERVAL=250ms ./scripts/loadtest-local.sh  # longer trial
```

Boots an isolated StarNexus server on a temp SQLite DB (with
`SetMaxOpenConns(1)` and `busy_timeout=5000` PRAGMAs) and hammers it
from `starnexus-loadtest` at increasing virtual-agent counts.

### Outputs

- `analysis-output/loadtest/loadtest-<N>-agents.json` — per-size
  JSON with p50/p95/p99 latency and success rate
- `web/public/data/loadtest.json` — aggregate snapshot served by the
  Cloudflare Pages demo

### Duration

`len(sizes) × (duration + a few seconds for bootstrap)`. Default
5 sizes × 15 s = ~90 seconds.

---

## 6. Reliability-Score Weight Sensitivity

### Command

```bash
uv run scripts/validate-reliability.py \
  --db <backup.sqlite> \
  --out analysis-output/weight-sensitivity.csv
```

### Outputs

- Console table: per-node composite scores under five weight schemes.
- CSV: same data in long form.
- Kendall-tau rank-agreement between each alternative scheme and the
  default 40/30/30.

### Duration

<1 second.

---

## 7. End-to-End Integration Test

### Command

```bash
cd server && go test -run TestEndToEndPipeline -v
```

Boots a real server on an ephemeral port, simulates an agent reporting
three sequential metric payloads (low → high → low), asserts the full
chain: node online → degraded → incident opens → recovery → incident
recovers → `/metrics` exposes the expected gauges.

### Bench CLI Integration Test

```bash
cd server && go test -run TestBenchCLIEndToEnd -timeout 60s -v ./cmd/starnexus-bench
```

Synthesises one hour of baseline metrics + one 120 s CPU spike, feeds
it through the real `starnexus-bench` binary, asserts each output
artifact is well-formed and that the bootstrap seed flag propagates.

### Durations

Pipeline test ~1 s; bench CLI test ~3 s (most of it `go run` compile).

---

## 8. Full Regression

The commands run by `.github/workflows/ci.yml` on every push:

```bash
# Per-module (server, agent, bot):
go vet ./...
go test -race -timeout 120s ./...
go build ./...

# Scripts
shellcheck -S warning scripts/*.sh

# Web
cd web && pnpm install --frozen-lockfile && pnpm exec tsc --noEmit

# Python
python -m py_compile scripts/generate-figures.py
python -m py_compile scripts/validate-reliability.py
```

Local equivalent:

```bash
make check       # runs all of the above on the developer's machine
```

### Duration

~90 seconds end-to-end.

---

## 9. Live Demo Deployment

### Command

```bash
cd web && pnpm install && pnpm run deploy   # wrangler pages deploy public
```

The `pages_build_output_dir = "public"` setting in `web/wrangler.toml`
tells wrangler to deploy the static assets from `public/` and
auto-detect the sibling `functions/` directory for Pages Functions.

Using `wrangler pages deploy .` **breaks** this auto-detection because
wrangler then treats the repo root as static assets. The correct
pattern is `wrangler pages deploy public`.

### D1 schema bootstrapping

On first deploy, the remote D1 database also needs schema + seed:

```bash
cd web
pnpm exec wrangler d1 execute starnexus-db --remote --file=schema.sql
pnpm exec wrangler d1 execute starnexus-db --remote --file=seed-data.sql
```

Subsequent deploys only re-run this if columns change; `CREATE TABLE
IF NOT EXISTS` is idempotent but ALTER TABLE migrations must be
applied explicitly.

### Duration

~30 s for code upload, D1 propagation typically <10 s.

---

## Random Sources of Truth

- **Go bootstrap RNG**: `nextLCG` in `server/internal/analytics/detectors.go`, deterministic given `--seed`.
- **Permutation tests**: `randomSignPermutationPValue` in `server/internal/analytics/significance.go`, seeded from the same LCG.
- **Load-test synthetic traffic**: `math/rand/v2` seeded per-agent with `(agent_index+1, 7)` — deterministic across runs for the same agent count.
- **Figures**: jitter in `detection_delay_box.png` uses `numpy.default_rng(42+idx)` — deterministic.

## What's **Not** Deterministic

- Matrix experiments (§3) use real CPU stress on a live VPS — detection
  delays depend on actual scheduler behavior. Rerunning produces
  numerically different delay values. The aggregate conclusions
  (detection rate, relative detector ordering) are stable across runs.
- Cloudflare D1 has no exact-timestamp control — the demo's "dynamic"
  timestamps (`last_seen`) are chosen by the Pages Functions at fetch
  time, not stored.
- The analytics scheduler on a live server fires every 5 minutes. If
  you run `make export-analysis` in the middle of a scheduler run, the
  exported snapshot will include that run's side effects.

## If Something Doesn't Match

1. `git log --oneline -20` — make sure you're at the commit the claim
   was made against. RESULTS.md and README.md both cite a "last
   updated" date.
2. Check `docs/dev-log-*.md` for context on what the sprint changed.
3. Re-run with `--seed` changed to confirm point estimates are
   seed-independent.
4. Open an issue with the exact command, expected output, and observed
   output. Include the output of `make check` and `git rev-parse HEAD`.
