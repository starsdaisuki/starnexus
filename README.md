# StarNexus - Distributed Node Monitoring System

A distributed VPS node health monitoring system with real-time world map visualization, live connection tracking, link topology, automated alerting, and AI-powered analytics.

## Live Demo

**https://starnexus-web.pages.dev** (static demo with fake data)

Real monitoring dashboard accessible via SSH tunnel to the Go server.
The repo's single frontend source of truth lives under [`web/public/`](web/public/).

## Architecture

```
┌─────────────────────────────────────────────────┐
│                    Web Frontend                  │
│  World map / Node details / Live connections     │
│  Tech: HTML + JS + Leaflet                       │
└──────────────────────┬──────────────────────────┘
                       │ HTTP API
┌──────────────────────┴──────────────────────────┐
│               Go Server (one VPS)                │
│  API + SQLite + Analytics + Static files         │
│  Port 8900 (not public, SSH tunnel only)         │
└──────────────────────┬──────────────────────────┘
            ┌──────────┼──────────┐
            │          │          │
      ┌─────┴───┐ ┌───┴─────┐ ┌─┴───────┐
      │ Agent A │ │ Agent B │ │ Agent C │  ...
      │ Tokyo   │ │ Japan   │ │ ...     │
      └─────────┘ └─────────┘ └─────────┘
            │
      ┌─────┴───────────────────────┐
      │  Telegram Bot                │
      │  Alerts + Commands + Watchdog│
      └─────────────────────────────┘
```

## Modules

| Module | Directory | Language | Status |
|--------|-----------|----------|--------|
| `web` | [`web/`](web/) | HTML/JS + Cloudflare Pages | ✅ Canonical frontend source |
| `server` | [`server/`](server/) | Go | ✅ Deployed |
| `agent` | [`agent/`](agent/) | Go | ✅ Deployed |
| `bot` | [`bot/`](bot/) | Go | ✅ Deployed |

## Features

### Server
- HTTP API (net/http) + SQLite (modernc.org/sqlite, pure Go)
- Node registration via agent self-report
- Offline detection (configurable threshold)
- Threshold alerts (CPU > 80%, memory > 90%)
- Static file serving for web frontend
- Agent binary + GeoIP DB + install script download endpoints
- Data analytics: downsampling, anomaly detection, node scoring
- AI-powered daily reports via Mistral API

### Agent
- System metrics via /proc: CPU, memory, disk, network, load, connections, uptime
- Link probing via TCP connect (replaces ICMP ping, bypasses firewall blocks)
- Live connection tracking: auto-detects xray/sing-box ports, collects per-IP byte counters with GeoIP lookup
- Per-IP cumulative byte tracking (survives TCP connection recycling)
- In-memory ring buffer (120 entries) for network outage resilience
- Auto-detect geolocation via ip-api.com on first run
- Cross-compiles to linux/amd64, single static binary (~10MB)

### Bot
- Telegram alerts on node status changes (online/degraded/offline)
- Alert debouncing (no spam for the same issue)
- Reverse heartbeat: pings server every 5 min, alerts after 3 failures
- `/status` command: node summary
- `/analytics`, `/events`, and `/node <id-or-name>` commands for operational inspection
- `/report` command: on-demand daily report with AI analysis
- Per-chat alert preferences: `/mute`, `/unmute`, `/subscribe`, `/unsubscribe`, `/daily on|off`, `/prefs`
- Multi-chat support (alerts sent to multiple Telegram users)

### Web Frontend
- Observatory dashboard with summary cards, event feed, node matrix, link diagnostics, and right-side node detail
- Dark world map (Leaflet + CartoDB Dark Matter) with fullscreen mode
- Day/night terminator line (updates every 60s)
- Animated node markers with glow effects (online/degraded/offline)
- GeoIP-estimated node positions are visually distinguished from manual / server-overridden coordinates
- Experiment View for labelled fault-injection detection and recovery delay
- Inter-node link lines with latency labels
- Live connection visualization: animated lines from client locations to nodes
  - Cloudflare CDN aggregation by /16 subnet
  - Hover tooltip with per-IP breakdown
  - Line thickness by transfer rate
- Node detail panel: trends, historical events, status history, ingress summary, and statistical highlights
- Connection toggle button

### Analytics (automatic)
- **Anomaly detection** (every 5 min): calibrated robust outlier + baseline-shift detection over a 24h rolling window
- **Reliability ledger**: separates operational incidents, statistical signals, and labelled experiment signals
- **Downsampling** (daily 03:00 UTC+8): raw → hourly (7-30d) → daily (30d+), purge old data
- **Node scoring** (daily): availability 40% + latency 30% + stability 30%
- **AI daily report** (09:00 UTC+8): metrics summary + Mistral AI analysis → Telegram
- **Research export**: `make analyze` writes CSV/JSON/Markdown datasets for statistical evaluation and reporting

### Deployment
- One-liner agent install: `curl -sSL http://<server>:8900/install.sh | bash -s -- --server ... --token ... --node-id ...`
- Heartbeat watchdog on secondary VPS (cron-based, independent of bot)
- systemd services for all modules
- Firewall: port 8900 restricted to known VPS IPs only, web UI via SSH tunnel

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Server | Go (net/http) + SQLite (modernc.org/sqlite) |
| Agent | Go, /proc metrics, GeoIP (oschwald/geoip2-golang) |
| Bot | Go, Telegram Bot API |
| Web | Leaflet, vanilla JS, Cloudflare Pages (demo) |
| Analytics | Robust statistics, MAD/median outlier detection, baseline-shift analysis, Mistral AI API |
| Database | SQLite (WAL mode) |
| Deployment | systemd, iptables, SSH tunnel |

## Quick Start

```bash
# Build all binaries (linux/amd64)
make build-all

# Deploy primary server to a VPS
./scripts/deploy-server.sh <ssh-host>

# Deploy agent to a new VPS (one-liner)
curl -sSL http://<server>:8900/install.sh | bash -s -- \
  --server http://<server>:8900 \
  --token <api-token> \
  --node-id <node-id> \
  --node-name "<display name>"

# Or onboard a new SSH-configured VPS from your local machine
./scripts/onboard-node.sh --primary <server-ssh-alias> --node <new-vps-alias> --node-id <node-id> --yes

# Access web UI via SSH tunnel
ssh -L 8900:localhost:8900 <server-host>
# Then open http://localhost:8900
```

## Node Management

Manage monitored nodes from your local machine:

```bash
./scripts/manage-node.sh add         # Add a node (auto: firewall, agent, probe, panel security)
./scripts/manage-node.sh remove      # Remove a node (auto: uninstall, clean DB, firewall, probe)
./scripts/manage-node.sh update-ip   # Node changed IP? Update firewall + probe, keep data
./scripts/manage-node.sh list        # Show all nodes and link status
./scripts/manage-node.sh reconfig    # Switch to a different primary server
```

Primary server config is saved to `~/.starnexus.env` on first run — no repeated prompts.

## Analysis Workflow

Run `make analyze` to export `nodes.csv`, `metrics.csv`, `events.csv`, `connection_sources.csv`, `analytics.json`, and `report.md` into `analysis-output/`. See [`docs/ANALYSIS.md`](docs/ANALYSIS.md) for how to interpret the proxy evaluation and extend it with controlled fault injection.

CPU-only labelled experiments can be run with `scripts/fault-injection.sh`; labels are appended to `analysis-output/experiments.jsonl` and shown in the dashboard Experiment View when `experiment_labels_path` points to that file.

For the current project status, recent upgrade summary, level assessment, and recommended next work, see [`docs/PROJECT-STATUS.md`](docs/PROJECT-STATUS.md).

## License

MIT
