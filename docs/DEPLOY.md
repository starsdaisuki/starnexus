# StarNexus Deployment Guide

Complete step-by-step instructions for deploying StarNexus from scratch.

---

## 1. Build Binaries

On your Mac (or any machine with Go installed):

```bash
git clone https://github.com/starsdaisuki/starnexus.git
cd starnexus

# Build all three binaries for Linux x86_64
cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server && cd ..
cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent && cd ..
cd bot && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot && cd ..

# Verify
ls -lh bin/
# starnexus-server  ~15 MB
# starnexus-agent   ~10 MB
# starnexus-bot     ~9 MB
```

> **Note:** If `make build-all` fails with an Xcode license error on macOS, use the individual `go build` commands above instead.

---

## 2. Server Setup (Primary VPS)

### 2.1 Upload Files

```bash
SERVER=user@your-server-ip

# Create directory
ssh $SERVER "mkdir -p ~/starnexus/{web,bin}"

# Upload binaries
scp bin/starnexus-server bin/starnexus-agent bin/starnexus-bot $SERVER:~/starnexus/

# Upload server files
scp server/schema.sql $SERVER:~/starnexus/
scp -r server/web/* $SERVER:~/starnexus/web/

# Upload agent binary to bin/ (for the download endpoint)
scp bin/starnexus-agent $SERVER:~/starnexus/bin/
```

### 2.2 Download GeoIP Database

On the server:

```bash
cd ~/starnexus
curl -sSLO https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb
cp GeoLite2-City.mmdb bin/GeoLite2-City.mmdb
```

### 2.3 Server Config

Create `~/starnexus/config.yaml`:

```yaml
# Network
port: 8900                        # HTTP port (do NOT expose publicly)

# Database
db_path: "./starnexus.db"         # SQLite database file path

# Auth
api_token: "CHANGE_ME"            # Shared secret — agents and bot use this to authenticate
                                  # Generate with: openssl rand -hex 32

# Frontend
web_dir: "./web"                  # Path to frontend static files

# Downloads (for one-liner install script)
agent_binary_path: "./bin/starnexus-agent"   # Agent binary served at GET /download/agent
geoip_db_path: "./bin/GeoLite2-City.mmdb"    # GeoIP DB served at GET /download/geoip

# Monitoring
offline_threshold_seconds: 90     # Mark node offline if no report for this many seconds

# Telegram (for analytics alerts — anomaly detection, daily reports)
bot_token: "BOT_TOKEN_HERE"       # Telegram bot token (see Section 3)
bot_chat_ids:                     # Telegram chat IDs to receive alerts
  - 123456789

# AI Analysis (optional — daily report includes AI insights if set)
mistral_api_key: ""               # Mistral API key from https://console.mistral.ai
```

### 2.4 Agent Config (on the same VPS)

Create `~/starnexus/agent-config.yaml`:

```yaml
server_url: "http://127.0.0.1:8900"   # Localhost since agent runs on same VPS
api_token: "CHANGE_ME"                 # Must match server's api_token
node_id: "my-server"                   # Unique node identifier
node_name: "My Server"                 # Display name on the map
provider: "ProviderName"               # Hosting provider name
public_ip: "1.2.3.4"                   # Public IP (shown in node popup)
latitude: 35.6762                      # Map coordinates (0 = auto-detect)
longitude: 139.6503
report_interval_seconds: 30            # How often to report metrics

# Link probing (TCP connect to other nodes)
probe_targets:
  - node_id: "other-node"             # Target node ID
    host: "5.6.7.8"                   # Target IP
    port: 22                           # TCP port to probe (SSH port works well)

# Connection tracking (live visualization)
geoip_db_path: "./GeoLite2-City.mmdb"
connection_report_interval_seconds: 5

# Port labels (optional — labels proxy ports on the map)
port_labels:
  443: "VLESS+WS (CDN)"
  37981: "VLESS+Reality"
```

### 2.5 Bot Config

Create `~/starnexus/bot-config.yaml`:

```yaml
telegram_token: "BOT_TOKEN_HERE"       # Telegram bot token (see Section 3)
chat_ids:                              # Chat IDs that can send commands and receive alerts
  - 123456789
  - 987654321
server_url: "http://127.0.0.1:8900"   # Localhost since bot runs on same VPS
api_token: "CHANGE_ME"                 # Must match server's api_token
poll_interval_seconds: 30              # How often to check for status changes
heartbeat_interval_seconds: 300        # Reverse heartbeat interval (5 min)
```

### 2.6 Firewall Rules

**IMPORTANT:** Do NOT open port 8900 to the public. Only allow known VPS IPs.

```bash
# Allow localhost (for agent + bot on same machine)
# If using iptables:
iptables -A INPUT -p tcp -s 127.0.0.1 --dport 8900 -j ACCEPT

# Allow other VPS IPs (one rule per VPS)
iptables -A INPUT -p tcp -s OTHER_VPS_IP --dport 8900 -j ACCEPT

# Allow ICMP from other VPS (for ping probing, if used)
iptables -A INPUT -p icmp -s OTHER_VPS_IP -j ACCEPT

# Block all other access to port 8900
iptables -A INPUT -p tcp --dport 8900 -j DROP

# Save rules (Debian/Ubuntu)
iptables-save > /etc/iptables.rules
```

If using ufw instead of iptables:

```bash
ufw allow from OTHER_VPS_IP to any port 8900
# Do NOT run: ufw allow 8900  (this opens it to everyone)
```

### 2.7 Systemd Services

Create three service files:

**`/etc/systemd/system/starnexus-server.service`**

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

**`/etc/systemd/system/starnexus-agent.service`**

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

**`/etc/systemd/system/starnexus-bot.service`**

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

Start and enable all three:

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

## 3. Telegram Bot Setup

### 3.1 Create Bot

1. Open Telegram, search for **@BotFather**
2. Send `/newbot`
3. Choose a name (e.g. "StarNexus Monitor")
4. Choose a username (e.g. "starnexus_monitor_bot")
5. Copy the token (looks like `123456789:ABCdefGHIjklMNOpqrsTUVwxyz`)

### 3.2 Get Your Chat ID

1. Open Telegram, search for **@userinfobot**
2. Send `/start`
3. It replies with your user ID (a number like `REDACTED_CHAT_ID_1`)
4. To add another user: have them message the bot, then add their chat ID to the `chat_ids` list

### 3.3 Test

After the bot is running, send `/status` to your bot in Telegram. It should reply with a node summary.

Send `/report` for an on-demand daily report with AI analysis (requires `mistral_api_key` in server config).

---

## 4. Agent on Other VPS (One-Liner Install)

On any new VPS you want to monitor:

```bash
curl -sSL http://SERVER_IP:8900/install.sh | bash -s -- \
  --server http://SERVER_IP:8900 \
  --token YOUR_API_TOKEN \
  --node-id "new-node" \
  --node-name "New Node" \
  --provider "ProviderName"
```

This automatically:
1. Downloads the agent binary from the server
2. Downloads GeoLite2-City.mmdb for connection tracking
3. Creates `config.yaml` (lat/lng auto-detected via ip-api.com)
4. Creates and starts a systemd service

**Before running:** Make sure the new VPS's IP is allowed in the server's firewall (see Section 2.6).

### Verify

```bash
systemctl is-active starnexus-agent
journalctl -u starnexus-agent -n 20

# On the server, check the node appeared:
curl -s http://localhost:8900/api/nodes
```

### Add Link Probing

After install, edit `~/starnexus/config.yaml` on the new VPS to add probe targets:

```yaml
probe_targets:
  - node_id: "my-server"
    host: "SERVER_IP"
    port: 22        # SSH port of the target
```

Then restart: `systemctl restart starnexus-agent`

---

## 5. Heartbeat Watchdog (Secondary VPS)

Deploy on a VPS **other than** the server to detect if the server goes down.

Create `/root/starnexus/heartbeat.sh`:

```bash
#!/bin/bash
set -uo pipefail

TARGET_URL="http://SERVER_IP:8900/api/status"
FAIL_FILE="/tmp/starnexus-heartbeat-fails"
BOT_TOKEN="YOUR_BOT_TOKEN"
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

Set up:

```bash
chmod +x /root/starnexus/heartbeat.sh

# Test
/root/starnexus/heartbeat.sh && cat /tmp/starnexus-heartbeat-fails

# Add cron (every 5 minutes)
(crontab -l 2>/dev/null; echo "*/5 * * * * /root/starnexus/heartbeat.sh") | crontab -
```

---

## 6. Accessing the Dashboard

The web UI is **not** exposed publicly. Access via SSH tunnel:

```bash
ssh -L 8900:localhost:8900 user@SERVER_IP
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

Then just: `ssh starnexus` and open http://localhost:8900.

---

## 7. Maintenance

### Update Binaries

On your Mac:

```bash
cd starnexus

# Rebuild
cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server && cd ..
cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent && cd ..
cd bot && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot && cd ..

# Deploy to server
ssh SERVER "systemctl stop starnexus-server starnexus-agent starnexus-bot"
scp bin/starnexus-server bin/starnexus-agent bin/starnexus-bot SERVER:~/starnexus/
ssh SERVER "systemctl start starnexus-server && sleep 2 && systemctl start starnexus-agent && systemctl start starnexus-bot"

# Update agent on other VPSes
ssh OTHER_VPS "systemctl stop starnexus-agent"
scp bin/starnexus-agent OTHER_VPS:~/starnexus/
ssh OTHER_VPS "systemctl start starnexus-agent"
```

### Backup Database

```bash
scp SERVER:~/starnexus/starnexus.db ./backup-$(date +%Y%m%d).db
```

### Add a New VPS Node

1. Whitelist the new VPS IP in the server's firewall
2. Run the one-liner install on the new VPS (see Section 4)
3. Optionally add probe targets to the new node's config
4. Optionally add the new VPS's IP as a probe target on existing nodes

### Check Logs

```bash
# Server
journalctl -u starnexus-server -f

# Agent
journalctl -u starnexus-agent -f

# Bot
journalctl -u starnexus-bot -f

# Last 50 lines
journalctl -u starnexus-server --no-pager -n 50
```

### Remove a Node

```bash
# On the VPS being removed
systemctl disable --now starnexus-agent

# On the server — delete from database
TOKEN="your-api-token"
curl -X DELETE http://localhost:8900/api/nodes/NODE_ID \
  -H "Authorization: Bearer $TOKEN"
```

---

## Automatic Features (no manual intervention needed)

| Feature | Schedule | What it does |
|---------|----------|--------------|
| Metrics collection | Every 30s | Agent collects CPU, memory, disk, network, load, connections, uptime |
| Connection tracking | Every 5s | Agent detects proxy ports, collects active connections with GeoIP |
| Link probing | Every 30s | Agent TCP-pings other nodes, measures latency |
| Offline detection | Every 30s | Server marks nodes offline if no report within threshold |
| Anomaly detection | Every 5 min | Server Z-score analysis on CPU/memory/bandwidth (24h window) |
| Downsampling | Daily 03:00 UTC+8 | Raw → hourly (7-30d) → daily (30d+), purge old data |
| Node scoring | Daily 03:00 UTC+8 | Availability + latency + stability → composite score |
| AI daily report | Daily 09:00 UTC+8 | Metrics summary + Mistral AI analysis → Telegram |
| Heartbeat watchdog | Every 5 min | Secondary VPS checks server, alerts on 3 failures |
| Bot polling | Every 30s | Detects status changes, sends Telegram alerts |
| Reverse heartbeat | Every 5 min | Bot pings server, alerts if unreachable |
