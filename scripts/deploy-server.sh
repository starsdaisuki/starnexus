#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# StarNexus Full Server Deployment
# Deploys server + agent + bot to a VPS in one command.
#
# Usage: ./scripts/deploy-server.sh <ssh-host>
# Example: ./scripts/deploy-server.sh root@YOUR_SERVER_IP
#          ./scripts/deploy-server.sh dmit
#
# What this does:
#   1. Prompts for all secrets and node info
#   2. Builds all three binaries (linux/amd64)
#   3. Uploads binaries, schema, web files to the VPS
#   4. Downloads GeoIP database on the VPS
#   5. Generates all config files
#   6. Sets up iptables firewall rules
#   7. Creates and starts systemd services
#   8. Verifies everything is running
# ============================================================

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <ssh-host>"
  echo ""
  echo "Examples:"
  echo "  $0 root@YOUR_SERVER_IP"
  echo "  $0 dmit                    (uses SSH config alias)"
  echo ""
  echo "This deploys the full StarNexus stack (server + agent + bot)"
  echo "to the target VPS. You will be prompted for secrets."
  exit 1
fi

SSH_HOST="$1"
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$SCRIPT_DIR/bin"

echo ""
echo "============================================================"
echo "  StarNexus Server Deployment"
echo "  Target: $SSH_HOST"
echo "============================================================"
echo ""

# ============================================================
# Step 1: Collect secrets and configuration
# ============================================================

echo "--- Secrets ---"
echo ""

read -rp "API token (Enter to auto-generate): " API_TOKEN
if [[ -z "$API_TOKEN" ]]; then
  API_TOKEN=$(openssl rand -hex 32)
  echo "  Generated: $API_TOKEN"
fi
echo ""

read -rp "Telegram bot token (from @BotFather): " TG_TOKEN
if [[ -z "$TG_TOKEN" ]]; then
  echo "  WARNING: No bot token — Telegram alerts will be disabled."
fi
echo ""

CHAT_IDS_RAW=""
if [[ -n "$TG_TOKEN" ]]; then
  read -rp "Telegram chat IDs (space-separated, from @userinfobot): " CHAT_IDS_RAW
fi

read -rp "Mistral API key (Enter to skip — AI reports disabled): " MISTRAL_KEY
echo ""

echo "--- Node info for this server ---"
echo ""

read -rp "Public IP of this server: " PUBLIC_IP
while [[ -z "$PUBLIC_IP" ]]; do
  read -rp "  Public IP is required: " PUBLIC_IP
done

read -rp "Node ID (e.g. tokyo-dmit, us-west-1): " NODE_ID
while [[ -z "$NODE_ID" ]]; do
  read -rp "  Node ID is required: " NODE_ID
done

read -rp "Display name (e.g. Tokyo DMIT): " NODE_NAME
[[ -z "$NODE_NAME" ]] && NODE_NAME="$NODE_ID"

read -rp "Provider (e.g. DMIT, Aliyun, AWS): " PROVIDER
[[ -z "$PROVIDER" ]] && PROVIDER="Unknown"

read -rp "Latitude (0 = auto-detect via ip-api.com): " LATITUDE
LATITUDE=${LATITUDE:-0}

read -rp "Longitude (0 = auto-detect): " LONGITUDE
LONGITUDE=${LONGITUDE:-0}

read -rp "SSH port of this server (default 22): " SSH_PORT
SSH_PORT=${SSH_PORT:-22}
echo ""

# Format chat IDs as YAML list
CHAT_IDS_YAML=""
for cid in $CHAT_IDS_RAW; do
  CHAT_IDS_YAML+="  - $cid"$'\n'
done

# Probe target (optional)
echo "--- Link probing (optional) ---"
read -rp "Add a probe target to another VPS? (y/N): " ADD_PROBE
PROBE_YAML=""
PROBE_VPS_IP=""
if [[ "$ADD_PROBE" =~ ^[yY] ]]; then
  read -rp "  Target node ID (e.g. jp-lisahost): " PROBE_NODE_ID
  read -rp "  Target VPS IP: " PROBE_HOST
  read -rp "  Target TCP port (e.g. SSH port, default 22): " PROBE_PORT
  PROBE_PORT=${PROBE_PORT:-22}
  PROBE_VPS_IP="$PROBE_HOST"
  PROBE_YAML="probe_targets:
  - node_id: \"$PROBE_NODE_ID\"
    host: \"$PROBE_HOST\"
    port: $PROBE_PORT"
fi
echo ""

# ============================================================
# Step 2: Build binaries
# ============================================================

echo "============================================================"
echo "  Building binaries (linux/amd64)..."
echo "============================================================"

mkdir -p "$BIN_DIR"
VERSION="${VERSION:-dev}"
COMMIT="$(git -C "$SCRIPT_DIR" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
SERVER_LDFLAGS="-X github.com/starsdaisuki/starnexus/server/internal/buildinfo.Version=$VERSION -X github.com/starsdaisuki/starnexus/server/internal/buildinfo.Commit=$COMMIT -X github.com/starsdaisuki/starnexus/server/internal/buildinfo.BuildTime=$BUILD_TIME"
AGENT_LDFLAGS="-X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.Version=$VERSION -X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.Commit=$COMMIT -X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.BuildTime=$BUILD_TIME"
BOT_LDFLAGS="-X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.Version=$VERSION -X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.Commit=$COMMIT -X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.BuildTime=$BUILD_TIME"
cd "$SCRIPT_DIR/server" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$SERVER_LDFLAGS" -o "$BIN_DIR/starnexus-server" . && echo "  server OK"
cd "$SCRIPT_DIR/agent"  && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$AGENT_LDFLAGS" -o "$BIN_DIR/starnexus-agent" .  && echo "  agent  OK"
cd "$SCRIPT_DIR/bot"    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$BOT_LDFLAGS" -o "$BIN_DIR/starnexus-bot" .    && echo "  bot    OK"
cd "$SCRIPT_DIR"
echo ""

# ============================================================
# Step 3: Upload files
# ============================================================

echo "============================================================"
echo "  Uploading to $SSH_HOST..."
echo "============================================================"

ssh "$SSH_HOST" "mkdir -p ~/starnexus/{web,bin}"

echo "  Uploading binaries..."
scp -q "$BIN_DIR/starnexus-server" "$BIN_DIR/starnexus-agent" "$BIN_DIR/starnexus-bot" "$SSH_HOST:~/starnexus/"
scp -q "$BIN_DIR/starnexus-agent" "$SSH_HOST:~/starnexus/bin/"

echo "  Uploading schema and web files..."
scp -q "$SCRIPT_DIR/server/schema.sql" "$SSH_HOST:~/starnexus/"
scp -qr "$SCRIPT_DIR/web/public/"* "$SSH_HOST:~/starnexus/web/"

echo "  Downloading GeoIP database on server..."
ssh "$SSH_HOST" "cd ~/starnexus && curl -sSLO https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb && cp GeoLite2-City.mmdb bin/ && echo '  GeoIP: $(ls -lh GeoLite2-City.mmdb | awk \"{print \\$5}\")'"
echo ""

# ============================================================
# Step 4: Write config files
# ============================================================

echo "============================================================"
echo "  Writing config files..."
echo "============================================================"

# Server config
ssh "$SSH_HOST" "cat > ~/starnexus/config.yaml" << YAML
port: 8900
db_path: "./starnexus.db"
api_token: "$API_TOKEN"
web_dir: "./web"
offline_threshold_seconds: 90
agent_binary_path: "./bin/starnexus-agent"
geoip_db_path: "./bin/GeoLite2-City.mmdb"
bot_token: "$TG_TOKEN"
bot_chat_ids:
$CHAT_IDS_YAML
mistral_api_key: "$MISTRAL_KEY"
YAML
echo "  config.yaml"

# Agent config
ssh "$SSH_HOST" "cat > ~/starnexus/agent-config.yaml" << YAML
server_url: "http://127.0.0.1:8900"
api_token: "$API_TOKEN"
node_id: "$NODE_ID"
node_name: "$NODE_NAME"
provider: "$PROVIDER"
public_ip: "$PUBLIC_IP"
latitude: $LATITUDE
longitude: $LONGITUDE
report_interval_seconds: 30
geoip_db_path: "./GeoLite2-City.mmdb"
connection_report_interval_seconds: 5
$PROBE_YAML
YAML
echo "  agent-config.yaml"

# Bot config
ssh "$SSH_HOST" "cat > ~/starnexus/bot-config.yaml" << YAML
telegram_token: "$TG_TOKEN"
chat_ids:
$CHAT_IDS_YAML
server_url: "http://127.0.0.1:8900"
api_token: "$API_TOKEN"
poll_interval_seconds: 30
heartbeat_interval_seconds: 300
YAML
echo "  bot-config.yaml"
echo ""

# ============================================================
# Step 5: Firewall
# ============================================================

echo "============================================================"
echo "  Configuring firewall..."
echo "============================================================"

# Build firewall commands
FW_CMDS="iptables -C INPUT -p tcp -s 127.0.0.1 --dport 8900 -j ACCEPT 2>/dev/null || iptables -I INPUT -p tcp -s 127.0.0.1 --dport 8900 -j ACCEPT"

if [[ -n "$PROBE_VPS_IP" ]]; then
  FW_CMDS="$FW_CMDS; iptables -C INPUT -p tcp -s $PROBE_VPS_IP --dport 8900 -j ACCEPT 2>/dev/null || iptables -I INPUT -p tcp -s $PROBE_VPS_IP --dport 8900 -j ACCEPT"
  FW_CMDS="$FW_CMDS; iptables -C INPUT -p icmp -s $PROBE_VPS_IP -j ACCEPT 2>/dev/null || iptables -I INPUT -p icmp -s $PROBE_VPS_IP -j ACCEPT"
fi

# Add DROP rule at the end (only if not already present)
FW_CMDS="$FW_CMDS; iptables -C INPUT -p tcp --dport 8900 -j DROP 2>/dev/null || iptables -A INPUT -p tcp --dport 8900 -j DROP"

ssh "$SSH_HOST" "$FW_CMDS" 2>/dev/null || true
echo "  iptables rules applied:"
ssh "$SSH_HOST" "iptables -L INPUT -n | grep 8900"
echo ""

# ============================================================
# Step 6: Systemd services
# ============================================================

echo "============================================================"
echo "  Creating systemd services..."
echo "============================================================"

ssh "$SSH_HOST" 'cat > /etc/systemd/system/starnexus-server.service << "SVC"
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
SVC

cat > /etc/systemd/system/starnexus-agent.service << "SVC"
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
SVC

cat > /etc/systemd/system/starnexus-bot.service << "SVC"
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
SVC

systemctl daemon-reload'
echo "  Created: starnexus-server, starnexus-agent, starnexus-bot"
echo ""

# ============================================================
# Step 7: Start and verify
# ============================================================

echo "============================================================"
echo "  Starting services..."
echo "============================================================"

ssh "$SSH_HOST" '
systemctl enable --now starnexus-server
sleep 3
systemctl enable --now starnexus-agent
sleep 1
systemctl enable --now starnexus-bot
sleep 3

echo ""
echo "  Service status:"
echo "    server: $(systemctl is-active starnexus-server)"
echo "    agent:  $(systemctl is-active starnexus-agent)"
echo "    bot:    $(systemctl is-active starnexus-bot)"
echo ""
echo "  API test:"
echo "    $(curl -s http://localhost:8900/api/status)"
'

# ============================================================
# Done
# ============================================================

echo ""
echo "============================================================"
echo "  Deployment complete!"
echo "============================================================"
echo ""
echo "  Dashboard:  ssh -L 8900:localhost:8900 $SSH_HOST"
echo "              then open http://localhost:8900"
echo ""
echo "  Agent install on other VPS:"
echo "    1. On THIS server, whitelist the new VPS IP:"
echo "       ssh $SSH_HOST \"iptables -I INPUT -p tcp -s NEW_VPS_IP --dport 8900 -j ACCEPT\""
echo ""
echo "    2. On the new VPS, run:"
echo "       curl -sSL http://$PUBLIC_IP:8900/install.sh | bash -s -- \\"
echo "         --server http://$PUBLIC_IP:8900 \\"
echo "         --token $API_TOKEN \\"
echo "         --node-id NEW_NODE_ID \\"
echo "         --node-name \"New Node Name\" \\"
echo "         --provider \"Provider\""
echo ""
echo "  Secrets (save these!):"
echo "    API token: $API_TOKEN"
if [[ -n "$TG_TOKEN" ]]; then
  echo "    Bot token: $TG_TOKEN"
fi
echo ""
echo "  Logs:"
echo "    journalctl -u starnexus-server -f"
echo "    journalctl -u starnexus-agent -f"
echo "    journalctl -u starnexus-bot -f"
