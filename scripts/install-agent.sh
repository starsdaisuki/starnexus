#!/usr/bin/env bash
set -euo pipefail

# StarNexus Agent Install Script
# Usage: curl -sSL http://<server>:8900/install.sh | bash -s -- \
#   --server http://<server>:8900 --token <token> --node-id <id> --node-name "<name>"

SERVER_URL=""
API_TOKEN=""
NODE_ID=""
NODE_NAME=""
PROVIDER=""
INSTALL_DIR="$HOME/starnexus"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server)   SERVER_URL="$2"; shift 2 ;;
    --token)    API_TOKEN="$2"; shift 2 ;;
    --node-id)  NODE_ID="$2"; shift 2 ;;
    --node-name) NODE_NAME="$2"; shift 2 ;;
    --provider) PROVIDER="$2"; shift 2 ;;
    --dir)      INSTALL_DIR="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

if [[ -z "$SERVER_URL" || -z "$API_TOKEN" || -z "$NODE_ID" ]]; then
  echo "Error: --server, --token, and --node-id are required"
  echo ""
  echo "Usage: curl -sSL http://<server>:8900/install.sh | bash -s -- \\"
  echo "  --server http://<server>:8900 \\"
  echo "  --token <api-token> \\"
  echo "  --node-id <node-id> \\"
  echo "  --node-name \"<display name>\" \\"
  echo "  --provider \"<provider name>\""
  exit 1
fi

[[ -z "$NODE_NAME" ]] && NODE_NAME="$NODE_ID"
[[ -z "$PROVIDER" ]] && PROVIDER="Unknown"

echo "==> Installing StarNexus Agent"
echo "    Server:  $SERVER_URL"
echo "    Node ID: $NODE_ID"
echo "    Name:    $NODE_NAME"
echo "    Dir:     $INSTALL_DIR"
echo ""

# Create directory
mkdir -p "$INSTALL_DIR"

# Download agent binary
echo "==> Downloading agent binary..."
curl -sSL "$SERVER_URL/download/agent" -o "$INSTALL_DIR/starnexus-agent"
chmod +x "$INSTALL_DIR/starnexus-agent"
echo "    Downloaded: $INSTALL_DIR/starnexus-agent"

# Write config (lat/lng = 0 triggers auto-detect on first run)
cat > "$INSTALL_DIR/config.yaml" << YAML
server_url: "$SERVER_URL"
api_token: "$API_TOKEN"
node_id: "$NODE_ID"
node_name: "$NODE_NAME"
provider: "$PROVIDER"
latitude: 0
longitude: 0
report_interval_seconds: 30
YAML
echo "    Config written: $INSTALL_DIR/config.yaml"

# Create systemd service
cat > /etc/systemd/system/starnexus-agent.service << UNIT
[Unit]
Description=StarNexus Agent
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/starnexus-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT
echo "    Systemd service created"

# Enable and start
systemctl daemon-reload
systemctl enable --now starnexus-agent
sleep 3

# Status check
if systemctl is-active --quiet starnexus-agent; then
  echo ""
  echo "==> StarNexus Agent installed and running!"
  echo "    Status: $(systemctl is-active starnexus-agent)"
  echo "    Logs:   journalctl -u starnexus-agent -f"
else
  echo ""
  echo "==> WARNING: Agent may not have started correctly."
  echo "    Check: journalctl -u starnexus-agent --no-pager -n 20"
fi
