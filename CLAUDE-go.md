# StarNexus Go Modules - Claude Code Development Guide

## Overview

Build the three Go modules for StarNexus: **Server**, **Agent**, and **Bot**.
The Web module (Cloudflare Pages) is already deployed at https://starnexus-web.pages.dev with fake data.

After these modules are done, we will also modify the Web module to connect to the real Go Server API.

## Reference Documents (read before starting)

- `starnexus-plan.md` — Full system architecture, feature list, data flow, design decisions
- `starnexus-status-page-plan.md` — D1 schema (reuse the same table structure for SQLite), API response formats, frontend design specs

**Read both documents fully before writing any code.**

---

## Architecture

```
┌─────────────────────────────────────┐
│           Go Server (one VPS)       │
│  HTTP API + SQLite + Static Files   │
│  Port 8900                          │
└──────────┬──────────────────────────┘
     ┌─────┼─────┐
     │     │     │
  Agent  Agent  Agent    (each VPS, including the server's own VPS)
     │
  Telegram Bot             (runs on same VPS as server, or separate)
```

- Agents push metrics to Server via HTTP POST
- Server stores in SQLite, serves API for the Web frontend
- Bot polls Server for alerts, sends to Telegram
- Web frontend is served as static files by the Go Server

---

## Technical Decisions (do NOT change)

| Decision | Choice | Reason |
|----------|--------|--------|
| SQLite driver | `modernc.org/sqlite` (pure Go) | No CGO needed, cross-compile from Mac to Linux just works |
| HTTP framework | `net/http` standard library | Simple enough for ~10 routes, no external framework needed |
| Config format | YAML | Already used in the plan, `gopkg.in/yaml.v3` |
| Agent → Server auth | Bearer token in HTTP header | Simple, matches the existing Web module API design |
| Cross-compile | `GOOS=linux GOARCH=amd64 go build` | Both VPS are Linux x86_64 |
| Process management | systemd | Standard, auto-restart on crash |

---

## Project Structure

Each module has its own `go.mod` to keep dependencies separate (agent should be minimal, no SQLite dependency).

```
starnexus/
├── server/
│   ├── go.mod
│   ├── go.sum
│   ├── main.go              # Entry point
│   ├── config.yaml           # Server config (port, token, DB path)
│   ├── config.yaml.example   # Template without secrets
│   ├── internal/
│   │   ├── config/           # Config loading
│   │   ├── db/               # SQLite init, migrations, queries
│   │   ├── api/              # HTTP handlers (nodes, links, status, report)
│   │   └── alert/            # Alert logic (offline detection, threshold checks)
│   ├── schema.sql            # Table creation
│   └── web/                  # Frontend static files (copied from web/ module later)
├── agent/
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   ├── config.yaml
│   ├── config.yaml.example
│   └── internal/
│       ├── config/
│       ├── collector/        # System metrics collection (CPU, memory, disk, network)
│       ├── reporter/         # HTTP POST to server with retry + buffer queue
│       └── probe/            # Ping/TCPing to other nodes
├── bot/
│   ├── go.mod
│   ├── go.sum
│   ├── main.go
│   ├── config.yaml
│   ├── config.yaml.example
│   └── internal/
│       ├── config/
│       ├── telegram/         # Bot API client, send messages, handle commands
│       └── monitor/          # Poll server API, detect state changes, reverse heartbeat
├── web/                      # Existing Cloudflare Pages module (already done)
├── scripts/
│   └── install-agent.sh      # One-liner install script for agent
├── Makefile                  # Build all, cross-compile, deploy shortcuts
├── README.md
└── starnexus-plan.md         # Reference docs
```

---

## Development Order (strict)

### Phase 1: Server

Build the server first — agent and bot need it to exist.

**config.yaml:**
```yaml
port: 8900
db_path: "./starnexus.db"
api_token: "CHANGE_ME"           # Shared secret with agents
web_dir: "./web"                  # Path to frontend static files (optional for now)
offline_threshold_seconds: 90     # Mark node offline if no report for this long
```

**Database:**
Reuse the same schema from `starnexus-status-page-plan.md` (nodes, node_metrics, links, status_history tables). Use `modernc.org/sqlite`.

**API routes (same as the Web module, but real data):**

Public (no auth):
```
GET  /api/nodes          → All nodes + latest metrics (JOIN)
GET  /api/nodes/:id      → Single node detail
GET  /api/links          → All link info
GET  /api/status         → Summary counts
GET  /api/history/:id    → Status change history for a node
```

Agent endpoints (Bearer token auth):
```
POST /api/report         → Agent reports metrics
POST /api/nodes          → Register new node (agent self-registration)
```

Static files:
```
GET  /                   → Serve web/index.html
GET  /css/*              → Serve web/css/
GET  /js/*               → Serve web/js/
```

**POST /api/report request body:**
```json
{
  "node_id": "tokyo-dmit",
  "name": "Tokyo DMIT",
  "provider": "DMIT",
  "latitude": 35.6762,
  "longitude": 139.6503,
  "metrics": {
    "cpu_percent": 12.5,
    "memory_percent": 45.2,
    "disk_percent": 33.0,
    "bandwidth_up": 150.3,
    "bandwidth_down": 2048.7,
    "load_avg": 0.35,
    "connections": 128,
    "uptime_seconds": 2592000
  }
}
```

On receiving a report:
1. Upsert into `nodes` table (update status to "online", update last_seen)
2. Upsert into `node_metrics` table
3. Check thresholds → if CPU > 80% or memory > 90%, record in status_history

**Offline detection:**
- Run a goroutine every 30 seconds
- For each node: if `now - last_seen > offline_threshold_seconds`, set status to "offline"
- Record status change in status_history

**Test the server:**
```bash
cd server
go run main.go
# Then: curl http://localhost:8900/api/status
# Should return {"total":0,"online":0,...}
```

### Phase 2: Agent

**config.yaml:**
```yaml
server_url: "http://SERVER_IP:8900"
api_token: "CHANGE_ME"
node_id: "tokyo-dmit"
node_name: "Tokyo DMIT"
provider: "DMIT"
latitude: 35.6762
longitude: 139.6503
report_interval_seconds: 30
```

**Metrics collection (Linux /proc):**
- CPU: read `/proc/stat`, calculate usage between two samples (1s apart)
- Memory: read `/proc/meminfo` (MemTotal, MemAvailable)
- Disk: use `syscall.Statfs` on "/"
- Network: read `/proc/net/dev`, calculate delta between two samples
- Load average: read `/proc/loadavg`
- Connections: read `/proc/net/sockstat` (TCP inuse)
- Uptime: read `/proc/uptime`

**Buffer queue:**
- In-memory ring buffer, capacity 120 (1 hour at 30s intervals)
- On HTTP POST failure: store report in buffer
- On next successful POST: flush buffer (batch send)

**Main loop:**
```
every 30 seconds:
  collect all metrics
  POST to server
  if fail: buffer the report
  if success and buffer not empty: flush buffer
```

**Cross-compile and test:**
```bash
cd agent
GOOS=linux GOARCH=amd64 go build -o starnexus-agent
# scp to VPS, run manually first to verify
```

### Phase 3: Bot

**config.yaml:**
```yaml
telegram_token: "BOT_TOKEN_HERE"
chat_id: 123456789                # Your Telegram user ID
server_url: "http://SERVER_IP:8900"
api_token: "CHANGE_ME"
poll_interval_seconds: 30         # Check server for status changes
heartbeat_interval_seconds: 300   # Reverse heartbeat every 5 min
```

**Features (MVP only, keep it simple):**
- Poll `/api/status` and `/api/nodes` every 30s
- Detect status changes (online→offline, online→degraded, etc.)
- Send Telegram alert on status change
- Alert debouncing: don't spam for the same issue
- Reverse heartbeat: ping server's `/api/status` every 5 min, alert if 3 consecutive failures
- Commands: `/status` → reply with node summary

**Do NOT implement for now:**
- `/restart` remote command
- `/map` screenshot
- Complex interactive commands

### Phase 4: Web Integration

After server + agent + bot are working:

1. Copy the frontend files from `web/public/` to `server/web/`
2. Modify the frontend JS: change API_BASE from `/api` to point to the Go server
3. Remove the fake timestamp logic (Go server returns real data)
4. The Cloudflare Pages demo keeps running with fake data (public showcase)
5. The Go server serves the real monitoring page (access via SSH tunnel)

---

## Deployment

### Server deployment

```bash
cd server
GOOS=linux GOARCH=amd64 go build -o starnexus-server
scp starnexus-server config.yaml schema.sql <user>@<server-ip>:~/starnexus/
ssh <server-ip>
cd ~/starnexus
./starnexus-server  # Test manually first
```

systemd unit file (`/etc/systemd/system/starnexus-server.service`):
```ini
[Unit]
Description=StarNexus Server
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/root/starnexus
ExecStart=/root/starnexus/starnexus-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**IMPORTANT: open port 8900 in ufw** (the VPS has been hardened with ufw, all ports blocked by default):
```bash
ufw allow 8900/tcp
```

### Agent deployment

Same pattern: cross-compile, scp, systemd. On EVERY VPS including the server's VPS.

### Bot deployment

Same pattern. Runs on the same VPS as server.

---

## Install script (scripts/install-agent.sh)

Creates a one-liner for deploying agent to a new VPS:
```bash
curl -sSL http://<server-ip>:8900/install.sh | bash -s -- \
  --server http://<server-ip>:8900 \
  --token <api-token> \
  --node-id <node-id> \
  --node-name "<name>" \
  --lat <latitude> \
  --lng <longitude>
```

The script should:
1. Download the agent binary from the server
2. Write config.yaml
3. Create systemd service
4. Start the service

---

## Makefile

```makefile
.PHONY: build-server build-agent build-bot build-all

build-server:
	cd server && GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server

build-agent:
	cd agent && GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent

build-bot:
	cd bot && GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot

build-all: build-server build-agent build-bot
```

---

## Verification Checklist

### Server
- [ ] Starts without error, listens on port 8900
- [ ] `GET /api/status` returns valid JSON
- [ ] `POST /api/report` with valid token stores data
- [ ] `POST /api/report` without token returns 401
- [ ] `GET /api/nodes` returns reported node data
- [ ] Offline detection marks nodes correctly after threshold

### Agent
- [ ] Cross-compiles to linux-amd64 without error
- [ ] Collects CPU, memory, disk, network metrics on Linux
- [ ] Successfully POSTs to server
- [ ] Buffer queue works when server is unreachable
- [ ] Flushes buffer when server comes back

### Bot
- [ ] Connects to Telegram API
- [ ] `/status` command returns node summary
- [ ] Sends alert when node goes offline
- [ ] Reverse heartbeat detects server downtime
- [ ] Does not spam repeated alerts for same issue

---

## Do NOT

- Do not use CGO or `go-sqlite3` — use `modernc.org/sqlite` only
- Do not use any HTTP framework (gin, echo, fiber) — use `net/http`
- Do not hardcode tokens, IPs, or secrets in source code — use config.yaml
- Do not write config.yaml with real secrets to git — only commit config.yaml.example
- Do not modify the web/ (Cloudflare Pages) module's deployed version
- Do not implement features marked as "Phase 3" or "Phase 4" in starnexus-plan.md unless explicitly asked
- Do not write any Chinese in code, comments, or output — everything in English
