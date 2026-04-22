# StarNexus Deployment Guide

Deploy the full StarNexus monitoring system from scratch. No prior knowledge needed.

## What You Need

- **Your Mac** (or any computer with Go installed) — for building
- **One primary VPS** — runs the server, agent, and bot (the "brain")
- **One or more additional VPS** — runs the agent only (monitored nodes)
- **Root SSH access** to all VPS
- **A Telegram account** — for receiving alerts

## Architecture Overview

```
Your Mac (build) → scp → Primary VPS (server + agent + bot)
                                ↑
                   Other VPS (agent only) report to primary
                                ↑
                   Your browser → SSH tunnel → Primary VPS dashboard
```

---

## Step 1: Build the Binaries

On your Mac:

```bash
# Clone the repo
git clone https://github.com/starsdaisuki/starnexus.git
cd starnexus

# Build all three programs for Linux
cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server && cd ..
cd agent  && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent  && cd ..
cd bot    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot    && cd ..
```

You should now have three files in `bin/`:
```
bin/starnexus-server   (~15 MB)
bin/starnexus-agent    (~10 MB)
bin/starnexus-bot      (~9 MB)
```

> **Tip:** If you get an Xcode license error on macOS, run `sudo xcodebuild -license accept` first.

---

## Step 2: Create a Telegram Bot

You need this BEFORE deploying, because the bot token goes into the config files.

### 2a. Create the bot

1. Open Telegram on your phone or desktop
2. Search for **@BotFather** and start a chat
3. Send: `/newbot`
4. Enter a name: `StarNexus Monitor` (or anything you like)
5. Enter a username: `my_starnexus_bot` (must end in `bot`)
6. BotFather replies with a **token** like: `123456789:ABCdefGHIjklMNOpqrsTUVwxyz-EXAMPLE`
7. **Save this token** — you'll need it in Step 4

### 2b. Get your chat ID

1. Search for **@userinfobot** on Telegram and start a chat
2. Send: `/start`
3. It replies with your **ID** like: `123456789`
4. **Save this number** — you'll need it in Step 4

If you want a second person to receive alerts, have them do the same with @userinfobot.

---

## Step 3: Generate an API Token

This is a shared secret that all components use to talk to each other.

```bash
openssl rand -hex 32
```

This outputs something like: `a1b2c3d4e5f6...your-token-here...`

**Save this** — it goes into every config file.

---

## Step 4: Deploy the Primary Server

### Option A: Automated (recommended)

```bash
cd starnexus
./scripts/deploy-server.sh root@YOUR_SERVER_IP
```

This script will:
- Ask for your API token, bot token, chat IDs, and node info
- Build all binaries
- Upload everything to the server
- Download the GeoIP database
- Generate all config files
- Set up firewall rules
- Create and start systemd services
- Print verification info

### Option B: Manual

If you prefer to do it step by step:

#### 4a. Upload files

```bash
SERVER=root@YOUR_SERVER_IP

# Create directories
ssh $SERVER "mkdir -p ~/starnexus/{web,bin}"

# Upload binaries
scp bin/starnexus-server bin/starnexus-agent bin/starnexus-bot $SERVER:~/starnexus/
scp bin/starnexus-agent $SERVER:~/starnexus/bin/

# Upload server files
scp server/schema.sql $SERVER:~/starnexus/
scp -r web/public/* $SERVER:~/starnexus/web/
```

#### 4b. Download GeoIP database

SSH into the server and run:

```bash
cd ~/starnexus
curl -sSLO https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb
cp GeoLite2-City.mmdb bin/
```

This 60MB file maps IP addresses to geographic locations for the connection visualization.

#### 4c. Create server config

Create `~/starnexus/config.yaml` on the server:

```yaml
# Port to listen on. NEVER expose this to the internet.
port: 8900

# SQLite database file. Created automatically on first run.
db_path: "./starnexus.db"

# Shared secret. All agents and the bot use this to authenticate.
# Generate with: openssl rand -hex 32
api_token: "a1b2c3d4e5f6...your-token-here..."

# Frontend files directory.
web_dir: "./web"

# Optional: override node coordinates from one central file.
# This is useful when you want exact rack / PoP map positions instead of GeoIP estimates.
node_locations_path: "./node-locations.yaml"

# Optional: labelled fault-injection experiments shown in the dashboard Experiment View.
experiment_labels_path: "./analysis-output/experiments.jsonl"

# How long (seconds) before marking a node as offline.
# If an agent doesn't report within this time, the node turns red.
offline_threshold_seconds: 90

# Path to agent binary. The server serves this at GET /download/agent
# so that the one-liner install script can download it.
agent_binary_path: "./bin/starnexus-agent"

# Path to GeoIP database. Served at GET /download/geoip
# so agents can download it during install.
geoip_db_path: "./bin/GeoLite2-City.mmdb"

# Telegram bot token. The SERVER uses this to send analytics alerts
# (anomaly detection, daily reports). This is SEPARATE from the bot module.
bot_token: "123456789:ABCdefGHIjklMNOpqrsTUVwxyz-EXAMPLE"

# Telegram chat IDs to receive analytics alerts.
bot_chat_ids:
  - 123456789
  - 987654321

# Mistral AI API key for the AI-powered daily report.
# Get from https://console.mistral.ai — leave empty to disable.
mistral_api_key: ""
```

#### 4d. Create agent config

Create `~/starnexus/agent-config.yaml` on the server:

```yaml
# Server URL. Use localhost because agent runs on the same VPS.
server_url: "http://127.0.0.1:8900"

# Must match the server's api_token exactly.
api_token: "a1b2c3d4e5f6...your-token-here..."

# Unique node ID. Used in the database and API.
node_id: "tokyo-dmit"

# Display name shown on the map.
node_name: "Tokyo DMIT"

# Hosting provider name shown in the node popup.
provider: "DMIT"

# Public IP address. Shown in the node popup card.
public_ip: "10.0.0.1"

# Map coordinates. Set both to 0 for auto-detection via ip-api.com.
latitude: 35.6762
longitude: 139.6503

# How often to collect and report metrics (seconds).
report_interval_seconds: 30

# GeoIP database for connection geolocation.
geoip_db_path: "./GeoLite2-City.mmdb"

# How often to report live proxy connections (seconds).
connection_report_interval_seconds: 5

# Link probing: TCP connect to other nodes to measure latency.
# Uses TCP handshake (not ICMP ping) so it works through firewalls.
probe_targets:
  - node_id: "jp-lisahost"      # Must match the other node's node_id
    host: "10.0.0.2"        # IP of the other node
    port: 22                 # TCP port to connect to (SSH port works)

# Optional: label proxy ports on the map visualization.
# port_labels:
#   443: "VLESS+WS (CDN)"
#   37981: "VLESS+Reality"
```

#### 4e. Create bot config

Create `~/starnexus/bot-config.yaml` on the server:

```yaml
# Telegram bot token. Same as in server config.
telegram_token: "123456789:ABCdefGHIjklMNOpqrsTUVwxyz-EXAMPLE"

# Chat IDs that can send commands to the bot AND receive alerts.
# Only these users can interact with the bot.
chat_ids:
  - 123456789
  - 987654321

# Server URL. Localhost because bot runs on the same VPS.
server_url: "http://127.0.0.1:8900"

# Must match server's api_token.
api_token: "a1b2c3d4e5f6...your-token-here..."

# How often the bot checks for node status changes (seconds).
poll_interval_seconds: 30

# Reverse heartbeat: bot pings the server every N seconds.
# If 3 consecutive pings fail, sends alert to Telegram.
heartbeat_interval_seconds: 300
```

#### 4f. Set up firewall

**Critical: port 8900 must NOT be accessible from the internet.**

```bash
# Allow localhost (agent + bot on same machine)
iptables -I INPUT -p tcp -s 127.0.0.1 --dport 8900 -j ACCEPT

# Allow each other VPS that runs an agent (one command per VPS):
iptables -I INPUT -p tcp -s 10.0.0.2 --dport 8900 -j ACCEPT
iptables -I INPUT -p icmp -s 10.0.0.2 -j ACCEPT

# Block everyone else
iptables -A INPUT -p tcp --dport 8900 -j DROP
```

> **Important:** The ACCEPT rules must come BEFORE the DROP rule. Use `-I` (insert at top) for ACCEPT and `-A` (append at bottom) for DROP.

#### 4g. Create systemd services

Create `/etc/systemd/system/starnexus-server.service`:
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

Create `/etc/systemd/system/starnexus-agent.service`:
```ini
[Unit]
Description=StarNexus Agent
After=network.target starnexus-server.service

[Service]
Type=simple
User=root
WorkingDirectory=/root/starnexus
ExecStart=/root/starnexus/starnexus-agent agent-config.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Create `/etc/systemd/system/starnexus-bot.service`:
```ini
[Unit]
Description=StarNexus Telegram Bot
After=network.target starnexus-server.service

[Service]
Type=simple
User=root
WorkingDirectory=/root/starnexus
ExecStart=/root/starnexus/starnexus-bot bot-config.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Start everything:
```bash
systemctl daemon-reload
systemctl enable --now starnexus-server
sleep 3
systemctl enable --now starnexus-agent
systemctl enable --now starnexus-bot
```

Verify:
```bash
systemctl is-active starnexus-server starnexus-agent starnexus-bot
# Should print: active active active

./starnexus-server --check-config ./config.yaml
./starnexus-agent --check-config ./agent-config.yaml
./starnexus-bot --check-config ./bot-config.yaml

curl -s http://localhost:8900/api/status
# Should print: {"total":1,"online":1,...}

curl -s http://localhost:8900/api/health
./starnexus-server --version
./starnexus-agent --version
./starnexus-bot --version
```

---

## Step 5: Add More VPS Nodes

For each additional VPS you want to monitor:

### Option A: One-command onboarding

If your VPS entries already exist in `~/.ssh/config`, run this from your local repo checkout:

```bash
./scripts/onboard-node.sh \
  --primary dmit \
  --node sg-vps \
  --node-id sg-vps \
  --node-name "Singapore VPS" \
  --provider "Oracle" \
  --yes
```

The script will:
- Detect the primary and new node public IPs
- Read the API token from the primary server config unless `--api-token` is provided
- Whitelist the new node for port `8900` on the primary server
- Run the served `/install.sh` agent installer on the new node
- Verify `starnexus-agent` is active
- Wait until the node appears in `/api/nodes`

Use `scripts/onboard-node.sh --help` for all options.

### Option B: Manual onboarding

### 5a. Whitelist the new VPS on the server

SSH into the **primary server** and run:

```bash
# Replace NEW_VPS_IP with the actual IP of the new VPS
iptables -I INPUT -p tcp -s NEW_VPS_IP --dport 8900 -j ACCEPT
iptables -I INPUT -p icmp -s NEW_VPS_IP -j ACCEPT
```

### 5b. Install the agent on the new VPS

SSH into the **new VPS** and run:

```bash
curl -sSL http://SERVER_IP:8900/install.sh | bash -s -- \
  --server http://SERVER_IP:8900 \
  --token YOUR_API_TOKEN \
  --node-id "new-node" \
  --node-name "New Node Name" \
  --provider "ProviderName"
```

Replace:
- `SERVER_IP` — your primary server's IP
- `YOUR_API_TOKEN` — the API token from Step 3
- `new-node` — a unique ID for this node (lowercase, no spaces)
- `New Node Name` — what appears on the map
- `ProviderName` — hosting provider (e.g. Aliyun, AWS)

This script automatically:
1. Downloads the agent binary from the server
2. Downloads the GeoIP database
3. Creates config.yaml (latitude/longitude auto-detected)
4. Creates a systemd service
5. Starts the agent

### 5c. Verify

```bash
# On the new VPS:
systemctl is-active starnexus-agent
journalctl -u starnexus-agent -n 10

# On the primary server:
curl -s http://localhost:8900/api/nodes
# The new node should appear in the list
```

### 5d. Update existing agents safely

For existing nodes, use the local sync helper instead of manually stopping and copying binaries:

```bash
# From your local repo checkout:
./scripts/sync-agent.sh sonet lisahost
```

The script:
- Builds a Linux amd64 agent locally
- Uploads to `/root/starnexus/starnexus-agent.new`
- Backs up the old remote binary as `starnexus-agent.prev.<timestamp>`
- Restarts only `starnexus-agent`
- Leaves remote `config.yaml`, `agent-config.yaml`, GeoIP data, and proxy services unchanged

Useful options:

```bash
./scripts/sync-agent.sh --install-dir /opt/starnexus --service starnexus-agent vps-alias
./scripts/sync-agent.sh --binary ./bin/starnexus-agent vps-alias
./scripts/sync-agent.sh --no-build vps-alias
```

### 5e. Add link probing (optional)

To measure latency between nodes, edit `~/starnexus/config.yaml` on the new VPS:

```yaml
probe_targets:
  - node_id: "tokyo-dmit"        # The server's node_id
    host: "10.0.0.1"       # The server's IP
    port: 22                     # Server's SSH port
```

Then restart: `systemctl restart starnexus-agent`

Also add a reverse probe on the server's `agent-config.yaml`:

```yaml
probe_targets:
  - node_id: "new-node"
    host: "NEW_VPS_IP"
    port: 22
```

Then restart the server's agent: `systemctl restart starnexus-agent`

---

## Step 6: Heartbeat Watchdog (Recommended)

The bot runs on the primary server. If that server dies, the bot dies too and can't alert you. The heartbeat watchdog solves this.

Deploy this on a **different VPS** (not the primary server).

### 6a. Create the script

Create `/root/starnexus/heartbeat.sh` on the secondary VPS:

```bash
#!/bin/bash
set -uo pipefail

# Change these values:
TARGET_URL="http://10.0.0.1:8900/api/status"   # Primary server URL
BOT_TOKEN="YOUR_TELEGRAM_BOT_TOKEN"                   # Same bot token
CHAT_IDS="123456789 987654321"                      # Space-separated chat IDs

FAIL_FILE="/tmp/starnexus-heartbeat-fails"

send_telegram() {
  for cid in $CHAT_IDS; do
    curl -sSL -X POST "https://api.telegram.org/bot${BOT_TOKEN}/sendMessage" \
      -d chat_id="$cid" -d parse_mode=HTML -d text="$1" >/dev/null 2>&1
  done
}

FAILS=0
[ -f "$FAIL_FILE" ] && FAILS=$(cat "$FAIL_FILE" 2>/dev/null || echo 0)

if curl -sSf --connect-timeout 10 --max-time 10 "$TARGET_URL" >/dev/null 2>&1; then
  if [ "$FAILS" -ge 3 ]; then
    send_telegram "🟢 Server recovered"
  fi
  echo 0 > "$FAIL_FILE"
else
  FAILS=$((FAILS + 1))
  echo "$FAILS" > "$FAIL_FILE"
  if [ "$FAILS" -eq 3 ]; then
    send_telegram "🔴 Server unreachable! (3 consecutive failures)"
  fi
fi
```

### 6b. Set up

```bash
chmod +x /root/starnexus/heartbeat.sh

# Test it
/root/starnexus/heartbeat.sh
cat /tmp/starnexus-heartbeat-fails
# Should print: 0

# Add cron job (runs every 5 minutes)
(crontab -l 2>/dev/null | grep -v starnexus-heartbeat; \
 echo "*/5 * * * * /root/starnexus/heartbeat.sh # starnexus-heartbeat") | crontab -

# Verify cron
crontab -l | grep starnexus
```

How it works:
- Every 5 minutes, curls the server's `/api/status` endpoint
- If it fails 3 times in a row (15 minutes), sends a Telegram alert
- When the server comes back, sends a recovery alert
- Counter resets on every successful check

---

## Step 7: Access the Dashboard

### SSH tunnel

Port 8900 is firewalled. Access the web UI through an SSH tunnel:

```bash
ssh -L 8900:localhost:8900 root@SERVER_IP
```

Then open **http://localhost:8900** in your browser.

### Make it permanent

Add to `~/.ssh/config` on your Mac:

```
Host starnexus
    HostName SERVER_IP
    User root
    LocalForward 8900 localhost:8900
```

Now just run `ssh starnexus` and open http://localhost:8900.

### What you'll see

- Dark world map with glowing node markers
- Lines between nodes showing latency
- Animated connection lines from client IPs to your proxy nodes
- Click a node for CPU/memory/disk/bandwidth details
- Status bar showing online/degraded/offline counts
- "Conns" button to toggle live connection visualization

---

## Maintenance

### Update all binaries

```bash
# On your Mac:
cd starnexus
cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server && cd ..
cd agent  && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent  && cd ..
cd bot    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot    && cd ..

# Stop, upload, restart on the server:
ssh SERVER "systemctl stop starnexus-server starnexus-agent starnexus-bot"
scp bin/starnexus-server bin/starnexus-agent bin/starnexus-bot SERVER:~/starnexus/
scp bin/starnexus-agent SERVER:~/starnexus/bin/
ssh SERVER "systemctl start starnexus-server && sleep 2 && systemctl start starnexus-agent && systemctl start starnexus-bot"

# Update agent on other VPS without changing config:
./scripts/sync-agent.sh OTHER_VPS
```

### Telegram bot commands

The bot accepts commands only from `chat_ids` in `bot-config.yaml`.

```text
/status              Fleet status and nodes
/analytics           Reliability, anomaly, and experiment summary
/events              Recent events
/node <id-or-name>   Node detail summary
/report              Daily AI report
/mute [30m|2h|1d]    Pause proactive alerts for this chat
/unmute              Resume proactive alerts
/subscribe           Enable proactive alerts
/unsubscribe         Disable proactive alerts for this chat
/daily on|off        Toggle the 09:00 UTC+8 analytics summary
/prefs               Show this chat's alert preferences
```

Preferences are stored in `starnexus-bot-state.json` in the bot working directory. Commands still work while a chat is muted or unsubscribed; only proactive alerts and daily summaries are filtered.

### Backup and restore database

```bash
scripts/backup-db.sh --host dmit
scripts/restore-db.sh --host dmit --backup backups/starnexus-db-dmit-YYYYMMDDTHHMMSSZ.sqlite.gz
```

`backup-db.sh` uses SQLite `.backup` on the remote host so the snapshot is consistent even while the server is running. `restore-db.sh` stops `starnexus-server` and `starnexus-bot`, saves the current database as `starnexus.db.pre-restore.<timestamp>`, restores the backup, removes stale WAL/SHM sidecars, restarts services, and verifies `/api/status`.

### Remove a node

```bash
# On the VPS being removed:
systemctl disable --now starnexus-agent

# On the server:
curl -X DELETE http://localhost:8900/api/nodes/NODE_ID \
  -H "Authorization: Bearer YOUR_API_TOKEN"
```

### Check logs

```bash
# Live logs:
journalctl -u starnexus-server -f
journalctl -u starnexus-agent -f
journalctl -u starnexus-bot -f

# Last 50 lines:
journalctl -u starnexus-server --no-pager -n 50
```

### Update GeoIP database

```bash
ssh SERVER "cd ~/starnexus && curl -sSLO https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb && cp GeoLite2-City.mmdb bin/"
ssh SERVER "systemctl restart starnexus-agent"
```

---

## Automatic Features

These run without any manual intervention:

| What | When | Description |
|------|------|-------------|
| Metrics collection | Every 30s | CPU, memory, disk, network, load, connections, uptime |
| Connection tracking | Every 5s | Detects proxy ports, maps client IPs with GeoIP |
| Link probing | Every 30s | TCP connect to other nodes, measures latency |
| Offline detection | Every 30s | Marks nodes red if no report within 90 seconds |
| Anomaly detection | Every 5 min | Z-score analysis, alerts if metrics spike (|Z| > 3) |
| Downsampling | Daily 3am (UTC+8) | Compresses old data: raw → hourly → daily |
| Node scoring | Daily 3am (UTC+8) | Rates each node: availability + latency + stability |
| AI daily report | Daily 9am (UTC+8) | Sends metrics + AI analysis to Telegram |
| Bot status check | Every 30s | Detects when nodes go online/offline/degraded |
| Bot heartbeat | Every 5 min | Bot checks if server is alive |
| Cron heartbeat | Every 5 min | Independent check from secondary VPS |

---

## Troubleshooting

**Agent shows "GeoIP DB not found"**
```bash
cd ~/starnexus
curl -sSLO https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb
systemctl restart starnexus-agent
```

**Agent can't connect to server**
- Check firewall: `iptables -L INPUT -n | grep 8900`
- Make sure the agent VPS IP is whitelisted
- Test: `curl -s http://SERVER_IP:8900/api/status` from the agent VPS

**Bot not sending messages**
- Verify bot token: `curl https://api.telegram.org/botYOUR_TOKEN/getMe`
- Verify chat ID: send `/status` to your bot on Telegram
- Check logs: `journalctl -u starnexus-bot -n 20`

**Dashboard shows no data**
- Check agent is reporting: `journalctl -u starnexus-agent -n 10`
- Check server received data: `curl http://localhost:8900/api/nodes`
- Wait 30 seconds after starting the agent for the first report

**Link shows N/A**
- Make sure both agents have each other in `probe_targets`
- The target VPS must not block TCP connections on the probe port
- Check: `nc -zv TARGET_IP TARGET_PORT` from the probing VPS
