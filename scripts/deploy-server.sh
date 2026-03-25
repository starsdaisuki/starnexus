#!/usr/bin/env bash
set -euo pipefail

# StarNexus Server Deployment Script
# Deploys server + agent + bot to a VPS in one command.
#
# Usage: ./scripts/deploy-server.sh user@host
#
# Prompts for: API token, Telegram bot token, chat IDs, Mistral key,
#              node ID/name, probe targets.
# Handles: build, upload, config generation, systemd, GeoIP, firewall hint.

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <ssh-host>"
  echo "Example: $0 root@10.0.0.1"
  echo "         $0 dmit"
  exit 1
fi

SSH_HOST="$1"
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="$SCRIPT_DIR/bin"

echo "================================================"
echo " StarNexus Server Deployment"
echo " Target: $SSH_HOST"
echo "================================================"
echo ""

# --- Collect secrets ---

read -rp "API token (or press Enter to generate): " API_TOKEN
if [[ -z "$API_TOKEN" ]]; then
  API_TOKEN=$(openssl rand -hex 32)
  echo "  Generated: $API_TOKEN"
fi

read -rp "Telegram bot token (from @BotFather): " TG_TOKEN
read -rp "Telegram chat IDs (space-separated): " CHAT_IDS_RAW
read -rp "Mistral API key (optional, Enter to skip): " MISTRAL_KEY
echo ""

# Format chat IDs as YAML list
CHAT_IDS_YAML=""
for cid in $CHAT_IDS_RAW; do
  CHAT_IDS_YAML+="  - $cid"$'\n'
done

# --- Node config ---

read -rp "Node ID for this server (e.g. tokyo-dmit): " NODE_ID
read -rp "Node display name (e.g. Tokyo DMIT): " NODE_NAME
read -rp "Provider name (e.g. DMIT): " PROVIDER
read -rp "Public IP of this server: " PUBLIC_IP
read -rp "Latitude (0 for auto-detect): " LATITUDE
LATITUDE=${LATITUDE:-0}
read -rp "Longitude (0 for auto-detect): " LONGITUDE
LONGITUDE=${LONGITUDE:-0}
read -rp "SSH port of this server (for other agents to probe, e.g. 22): " SSH_PORT
SSH_PORT=${SSH_PORT:-22}
echo ""

# --- Probe target (optional) ---

read -rp "Add a probe target? (y/N): " ADD_PROBE
PROBE_YAML=""
if [[ "$ADD_PROBE" =~ ^[yY] ]]; then
  read -rp "  Target node ID: " PROBE_NODE_ID
  read -rp "  Target IP: " PROBE_HOST
  read -rp "  Target TCP port (e.g. SSH port): " PROBE_PORT
  PROBE_YAML="probe_targets:
  - node_id: \"$PROBE_NODE_ID\"
    host: \"$PROBE_HOST\"
    port: $PROBE_PORT"
fi

echo ""
echo "================================================"
echo " Building binaries..."
echo "================================================"

cd "$SCRIPT_DIR/server" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/starnexus-server" .
cd "$SCRIPT_DIR/agent"  && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/starnexus-agent" .
cd "$SCRIPT_DIR/bot"    && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$BIN_DIR/starnexus-bot" .
cd "$SCRIPT_DIR"

echo "  server: $(ls -lh "$BIN_DIR/starnexus-server" | awk '{print $5}')"
echo "  agent:  $(ls -lh "$BIN_DIR/starnexus-agent" | awk '{print $5}')"
echo "  bot:    $(ls -lh "$BIN_DIR/starnexus-bot" | awk '{print $5}')"

echo ""
echo "================================================"
echo " Uploading to $SSH_HOST..."
echo "================================================"

ssh "$SSH_HOST" "mkdir -p ~/starnexus/{web,bin}"
scp "$BIN_DIR/starnexus-server" "$BIN_DIR/starnexus-agent" "$BIN_DIR/starnexus-bot" "$SSH_HOST:~/starnexus/"
scp "$BIN_DIR/starnexus-agent" "$SSH_HOST:~/starnexus/bin/"
scp "$SCRIPT_DIR/server/schema.sql" "$SSH_HOST:~/starnexus/"
scp -r "$SCRIPT_DIR/server/web/"* "$SSH_HOST:~/starnexus/web/"

echo "  Downloading GeoIP database on server..."
ssh "$SSH_HOST" "cd ~/starnexus && curl -sSLO https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb && cp GeoLite2-City.mmdb bin/"

echo ""
echo "================================================"
echo " Writing configs..."
echo "================================================"

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

echo "  Configs written."

echo ""
echo "================================================"
echo " Creating systemd services..."
echo "================================================"

ssh "$SSH_HOST" 'cat > /etc/systemd/system/starnexus-server.service << "EOF"
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
EOF

cat > /etc/systemd/system/starnexus-agent.service << "EOF"
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
EOF

cat > /etc/systemd/system/starnexus-bot.service << "EOF"
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
EOF

systemctl daemon-reload'

echo "  Services created."

echo ""
echo "================================================"
echo " Starting services..."
echo "================================================"

ssh "$SSH_HOST" '
systemctl enable --now starnexus-server
sleep 3
systemctl enable --now starnexus-agent
sleep 1
systemctl enable --now starnexus-bot
sleep 3
echo "server: $(systemctl is-active starnexus-server)"
echo "agent:  $(systemctl is-active starnexus-agent)"
echo "bot:    $(systemctl is-active starnexus-bot)"
echo ""
echo "API test: $(curl -s http://localhost:8900/api/status)"
'

echo ""
echo "================================================"
echo " Deployment complete!"
echo "================================================"
echo ""
echo "IMPORTANT: Configure firewall on the server:"
echo "  iptables -A INPUT -p tcp -s 127.0.0.1 --dport 8900 -j ACCEPT"
echo "  iptables -A INPUT -p tcp -s OTHER_VPS_IP --dport 8900 -j ACCEPT"
echo "  iptables -A INPUT -p icmp -s OTHER_VPS_IP -j ACCEPT"
echo "  iptables -A INPUT -p tcp --dport 8900 -j DROP"
echo ""
echo "Access dashboard: ssh -L 8900:localhost:8900 $SSH_HOST"
echo "Then open: http://localhost:8900"
echo ""
echo "Install agent on other VPS:"
echo "  curl -sSL http://$PUBLIC_IP:8900/install.sh | bash -s -- \\"
echo "    --server http://$PUBLIC_IP:8900 \\"
echo "    --token $API_TOKEN \\"
echo "    --node-id <id> --node-name \"<name>\""
echo ""
echo "API token: $API_TOKEN"
echo "Bot token: $TG_TOKEN"
echo "Save these — they are not stored anywhere else."
