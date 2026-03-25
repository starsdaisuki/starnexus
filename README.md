# StarNexus - Distributed Node Monitoring System

A distributed VPS node health monitoring system with real-time world map visualization, live connection tracking, link topology, automated alerting, and AI-powered analytics.

## Live Demo

**https://starnexus-web.pages.dev** (static demo with fake data)

Real monitoring dashboard accessible via SSH tunnel to the Go server.

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
| `web` | [`web/`](web/) | HTML/JS + Cloudflare Pages | ✅ Live demo |
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
- `/report` command: on-demand daily report with AI analysis
- Multi-chat support (alerts sent to multiple Telegram users)

### Web Frontend
- Dark world map (Leaflet + CartoDB Dark Matter)
- Day/night terminator line (updates every 60s)
- Animated node markers with glow effects (online/degraded/offline)
- Inter-node link lines with latency labels
- Live connection visualization: animated lines from client locations to nodes
  - Cloudflare CDN aggregation by /16 subnet
  - Hover tooltip with per-IP breakdown
  - Line thickness by transfer rate
- Node popup: IP, provider, CPU/memory/disk bars, bandwidth, load, uptime
- Status bar with online/degraded/offline counts
- Connection toggle button

### Analytics (automatic)
- **Anomaly detection** (every 5 min): Z-score on CPU/memory/bandwidth over 24h rolling window
- **Downsampling** (daily 03:00 UTC+8): raw → hourly (7-30d) → daily (30d+), purge old data
- **Node scoring** (daily): availability 40% + latency 30% + stability 30%
- **AI daily report** (09:00 UTC+8): metrics summary + Mistral AI analysis → Telegram

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
| Analytics | Mistral AI API, Z-score anomaly detection |
| Database | SQLite (WAL mode) |
| Deployment | systemd, iptables, SSH tunnel |

## Quick Start

```bash
# Build all binaries (linux/amd64)
make build-all

# Deploy agent to a new VPS (one-liner)
curl -sSL http://<server>:8900/install.sh | bash -s -- \
  --server http://<server>:8900 \
  --token <api-token> \
  --node-id <node-id> \
  --node-name "<display name>"

# Access web UI via SSH tunnel
ssh -L 8900:localhost:8900 <server-host>
# Then open http://localhost:8900
```

## License

MIT
