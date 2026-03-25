# StarNexus Deployment Guide

Deploy the full StarNexus monitoring system from scratch. This guide matches the actual running configuration.

---

## Prerequisites

- macOS or Linux with Go 1.21+ installed (for building)
- One primary VPS (runs server + agent + bot)
- One or more additional VPS (runs agent only)
- A Telegram bot token (see Section 3)
- SSH access to all VPS as root

---

## 1. Build

On your local machine:

```bash
git clone https://github.com/starsdaisuki/starnexus.git
cd starnexus

# Build all three for Linux x86_64 (static, no CGO)
cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server && cd ..
cd agent  && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent  && cd ..
cd bot    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot    && cd ..

ls -lh bin/
# starnexus-server  ~15 MB
# starnexus-agent   ~10 MB
# starnexus-bot     ~9 MB
```

---

## 2. Server Setup (Primary VPS)

### 2.1 Upload Files

```bash
SERVER=root@YOUR_SERVER_IP

ssh $SERVER "mkdir -p ~/starnexus/{web,bin}"

# Binaries
scp bin/starnexus-server bin/starnexus-agent bin/starnexus-bot $SERVER:~/starnexus/
scp bin/starnexus-agent $SERVER:~/starnexus/bin/   # for download endpoint

# Server files
scp server/schema.sql $SERVER:~/starnexus/
scp -r server/web/* $SERVER:~/starnexus/web/

# GeoIP database (for connection visualization)
ssh $SERVER "cd ~/starnexus && curl -sSLO https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb && cp GeoLite2-City.mmdb bin/"
```

### 2.2 Generate API Token

```bash
# Run on server or locally
openssl rand -hex 32
# Example output: REDACTED_API_TOKEN
```

Use this token in ALL config files below. Every component must share the same token.

### 2.3 Server Config

Create `~/starnexus/config.yaml` on the server:

```yaml
port: 8900
db_path: "./starnexus.db"
api_token: "PASTE_TOKEN_HERE"
web_dir: "./web"
offline_threshold_seconds: 90

# Download endpoints (one-liner agent install uses these)
agent_binary_path: "./bin/starnexus-agent"
geoip_db_path: "./bin/GeoLite2-City.mmdb"

# Telegram alerts from analytics (anomaly detection, daily reports)
# The server sends these directly — separate from the bot module
bot_token: "PASTE_TELEGRAM_BOT_TOKEN"
bot_chat_ids:
  - PASTE_CHAT_ID_1
  - PASTE_CHAT_ID_2

# AI daily report (optional, leave empty to disable)
mistral_api_key: ""
```

**Every field explained:**

| Field | Required | Description |
|-------|----------|-------------|
| `port` | Yes | HTTP listen port. Default 8900. Never expose publicly. |
| `db_path` | Yes | SQLite database file. Created automatically on first run. |
| `api_token` | Yes | Shared secret. Agents and bot authenticate with this. |
| `web_dir` | Yes | Path to frontend HTML/JS/CSS files. |
| `offline_threshold_seconds` | Yes | Seconds without a report before marking a node offline. |
| `agent_binary_path` | No | Path to agent binary for `GET /download/agent`. |
| `geoip_db_path` | No | Path to GeoLite2-City.mmdb for `GET /download/geoip`. |
| `bot_token` | No | Telegram bot token. Analytics alerts are sent directly by the server. |
| `bot_chat_ids` | No | List of Telegram chat IDs to receive analytics alerts. |
| `mistral_api_key` | No | Mistral AI API key for daily AI analysis. Get from console.mistral.ai. |

### 2.4 Agent Config (same VPS as server)

Create `~/starnexus/agent-config.yaml`:

```yaml
server_url: "http://127.0.0.1:8900"
api_token: "PASTE_TOKEN_HERE"
node_id: "my-server"
node_name: "My Server Name"
provider: "DMIT"
public_ip: "YOUR_SERVER_IP"
latitude: 35.6762
longitude: 139.6503
report_interval_seconds: 30

probe_targets:
  - node_id: "other-node-id"
    host: "OTHER_VPS_IP"
    port: 22

geoip_db_path: "./GeoLite2-City.mmdb"
connection_report_interval_seconds: 5
```

**Every field explained:**

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `server_url` | Yes | — | Server API URL. Use `http://127.0.0.1:8900` when on same VPS. |
| `api_token` | Yes | — | Must match server's `api_token`. |
| `node_id` | Yes | — | Unique identifier (e.g. "tokyo-dmit"). Used in API and DB. |
| `node_name` | Yes | — | Display name on the map (e.g. "Tokyo DMIT"). |
| `provider` | No | — | Hosting provider name shown in popup. |
| `public_ip` | No | auto | Public IP shown in node popup. Auto-detected if empty. |
| `latitude` | No | 0 | Map latitude. Set to 0 for auto-detect via ip-api.com. |
| `longitude` | No | 0 | Map longitude. Set to 0 for auto-detect. |
| `report_interval_seconds` | No | 30 | Metrics report interval. |
| `probe_targets` | No | [] | List of nodes to TCP-probe for link latency. |
| `probe_targets[].node_id` | Yes | — | Target node's ID (must match their `node_id`). |
| `probe_targets[].host` | Yes | — | Target IP address. |
| `probe_targets[].port` | No | 22 | TCP port to connect to (SSH port works well). |
| `geoip_db_path` | No | ./GeoLite2-City.mmdb | MaxMind GeoIP database for connection geolocation. |
| `connection_report_interval_seconds` | No | 5 | Live connection report interval. |
| `port_labels` | No | {} | Map of port → display name (e.g. `443: "VLESS+WS"`). |
| `proxy_processes` | No | [xray, sing-box, x-ui, 3x-ui] | Process names to detect proxy ports. |

### 2.5 Bot Config

Create `~/starnexus/bot-config.yaml`:

```yaml
telegram_token: "PASTE_TELEGRAM_BOT_TOKEN"
chat_ids:
  - PASTE_CHAT_ID_1
  - PASTE_CHAT_ID_2
server_url: "http://127.0.0.1:8900"
api_token: "PASTE_TOKEN_HERE"
poll_interval_seconds: 30
heartbeat_interval_seconds: 300
```

**Every field explained:**

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `telegram_token` | Yes | — | Bot token from @BotFather. |
| `chat_ids` | Yes | — | List of Telegram user IDs. Only these users can send commands. Alerts go to all. |
| `server_url` | Yes | — | Server API URL. Use localhost when on same VPS. |
| `api_token` | Yes | — | Must match server's `api_token`. |
| `poll_interval_seconds` | No | 30 | How often to check for node status changes. |
| `heartbeat_interval_seconds` | No | 300 | Reverse heartbeat interval (alerts if server unreachable). |

### 2.6 Firewall

**Port 8900 must NOT be public.** Only allow known VPS IPs and localhost.

```bash
# iptables (most VPS)
iptables -A INPUT -p tcp -s 127.0.0.1 --dport 8900 -j ACCEPT
iptables -A INPUT -p tcp -s OTHER_VPS_IP --dport 8900 -j ACCEPT
iptables -A INPUT -p icmp -s OTHER_VPS_IP -j ACCEPT
iptables -A INPUT -p tcp --dport 8900 -j DROP

# Save (Debian/Ubuntu)
iptables-save > /etc/iptables.rules

# Or with ufw:
ufw allow from OTHER_VPS_IP to any port 8900
# NEVER run: ufw allow 8900
```

### 2.7 Systemd Services

Create three files on the server:

**/etc/systemd/system/starnexus-server.service**
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

**/etc/systemd/system/starnexus-agent.service**
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

**/etc/systemd/system/starnexus-bot.service**
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
sleep 2
systemctl enable --now starnexus-agent
systemctl enable --now starnexus-bot

# Verify
systemctl is-active starnexus-server starnexus-agent starnexus-bot
curl -s http://localhost:8900/api/status
```

---

## 3. Telegram Bot

### Create the bot

1. Open Telegram, message **@BotFather**
2. Send `/newbot`, pick a name and username
3. Copy the token: `123456789:ABCdefGHIjklMNOpqrsTUVwxyz`

### Get your chat ID

1. Message **@userinfobot** on Telegram
2. Send `/start` — it replies with your numeric user ID
3. Add each user's ID to the `chat_ids` list in bot and server configs

### Test

After services are running, send `/status` to your bot. It should reply with a node summary. Send `/report` for a daily report with AI analysis (requires `mistral_api_key`).

---

## 4. Agent on Additional VPS (One-Liner)

**First:** whitelist the new VPS IP in the server's firewall (Section 2.6).

Then on the new VPS:

```bash
curl -sSL http://SERVER_IP:8900/install.sh | bash -s -- \
  --server http://SERVER_IP:8900 \
  --token YOUR_API_TOKEN \
  --node-id "new-node" \
  --node-name "New Node Name" \
  --provider "ProviderName"
```

This downloads the agent binary + GeoIP DB, writes config (lat/lng auto-detected), creates systemd service, and starts it.

### Verify

```bash
systemctl is-active starnexus-agent
journalctl -u starnexus-agent -n 10
```

### Add Link Probing

Edit `~/starnexus/config.yaml` on the new VPS:

```yaml
probe_targets:
  - node_id: "server-node-id"
    host: "SERVER_IP"
    port: 22    # SSH port of target (TCP connect, not ICMP)
```

Restart: `systemctl restart starnexus-agent`

Also add a reverse probe on the server's `agent-config.yaml` pointing back to this new VPS.

### Add Port Labels (optional)

```yaml
port_labels:
  443: "VLESS+WS (CDN)"
  37981: "Reality"
```

---

## 5. Heartbeat Watchdog

Deploy on a VPS **different from** the server. This detects if the server itself goes down — the bot can't alert if the server hosting it is dead.

Create `/root/starnexus/heartbeat.sh`:

```bash
#!/bin/bash
set -uo pipefail

TARGET_URL="http://SERVER_IP:8900/api/status"
FAIL_FILE="/tmp/starnexus-heartbeat-fails"
BOT_TOKEN="PASTE_TELEGRAM_BOT_TOKEN"
CHAT_IDS="CHAT_ID_1 CHAT_ID_2"

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

```bash
chmod +x /root/starnexus/heartbeat.sh

# Test
/root/starnexus/heartbeat.sh && cat /tmp/starnexus-heartbeat-fails

# Add cron (every 5 minutes)
(crontab -l 2>/dev/null | grep -v starnexus-heartbeat; \
 echo "*/5 * * * * /root/starnexus/heartbeat.sh # starnexus-heartbeat") | crontab -
```

---

## 6. Accessing the Dashboard

Port 8900 is not public. Use an SSH tunnel:

```bash
ssh -L 8900:localhost:8900 root@SERVER_IP
```

Then open **http://localhost:8900** in your browser.

### Persistent SSH Config

Add to `~/.ssh/config`:

```
Host starnexus
    HostName SERVER_IP
    User root
    LocalForward 8900 localhost:8900
```

Then: `ssh starnexus` → open http://localhost:8900

---

## 7. Maintenance

### Update Binaries

```bash
# Rebuild locally
cd starnexus/server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server && cd ..
cd agent  && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent  && cd ..
cd bot    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot    && cd ..

# Deploy to server (must stop first — can't overwrite running binaries)
ssh SERVER "systemctl stop starnexus-server starnexus-agent starnexus-bot"
scp bin/starnexus-server bin/starnexus-agent bin/starnexus-bot SERVER:~/starnexus/
scp bin/starnexus-agent SERVER:~/starnexus/bin/  # update download copy too
ssh SERVER "systemctl start starnexus-server && sleep 2 && systemctl start starnexus-agent && systemctl start starnexus-bot"

# Update agent on other VPS
ssh OTHER "systemctl stop starnexus-agent"
scp bin/starnexus-agent OTHER:~/starnexus/
ssh OTHER "systemctl start starnexus-agent"
```

### Backup Database

```bash
scp SERVER:~/starnexus/starnexus.db ./backup-$(date +%Y%m%d).db
```

### Add a New Node

1. Whitelist new VPS IP in server firewall
2. Run the one-liner install (Section 4)
3. Add probe targets on both sides
4. Restart agents on both sides

### Remove a Node

```bash
# On the VPS
systemctl disable --now starnexus-agent

# On the server — remove from database
curl -X DELETE http://localhost:8900/api/nodes/NODE_ID \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Check Logs

```bash
journalctl -u starnexus-server -f     # server log (live)
journalctl -u starnexus-agent -f      # agent log (live)
journalctl -u starnexus-bot -f        # bot log (live)
journalctl -u starnexus-agent -n 50   # last 50 lines
```

### Update GeoIP Database

The database is updated periodically by the upstream provider. Re-download:

```bash
ssh SERVER "cd ~/starnexus && curl -sSLO https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb && cp GeoLite2-City.mmdb bin/"
systemctl restart starnexus-agent  # on each VPS with the agent
```

---

## Automatic Features Reference

| Feature | Schedule | Description |
|---------|----------|-------------|
| Metrics collection | Every 30s | CPU, memory, disk, network, load, connections, uptime |
| Connection tracking | Every 5s | Detects proxy ports, collects per-IP bytes with GeoIP |
| Link probing | Every 30s | TCP connect to other nodes, measures handshake latency |
| Offline detection | Every 30s | Server marks nodes offline if no report within threshold |
| Anomaly detection | Every 5 min | Z-score on CPU/memory/bandwidth (24h window, |Z|>3 alerts) |
| Downsampling | Daily 03:00 UTC+8 | raw→hourly (7-30d), hourly→daily (30d+), purge old data |
| Node scoring | Daily 03:00 UTC+8 | Availability 40% + latency 30% + stability 30% |
| AI daily report | Daily 09:00 UTC+8 | Metrics + AI analysis via Mistral → Telegram |
| Bot polling | Every 30s | Detects status changes, sends Telegram alerts |
| Reverse heartbeat | Every 5 min | Bot pings server, alerts after 3 consecutive failures |
| Cron heartbeat | Every 5 min | Independent watchdog on secondary VPS |
