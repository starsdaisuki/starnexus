#!/usr/bin/env bash
set -euo pipefail

PRIMARY_SSH=""
NODE_SSH=""
NODE_ID=""
NODE_NAME=""
PROVIDER="Unknown"
PRIMARY_IP=""
NODE_IP=""
SERVER_URL=""
API_TOKEN=""
INSTALL_DIR="/root/starnexus"
YES=0

usage() {
  cat <<'EOF'
Usage:
  scripts/onboard-node.sh --primary <ssh-alias> --node <ssh-alias> --node-id <id> [options]

Required:
  --primary <ssh-alias>   Primary StarNexus server SSH alias.
  --node <ssh-alias>      New VPS SSH alias.
  --node-id <id>          Stable node id, e.g. sg-oracle-1.

Options:
  --node-name <name>      Display name. Default: node id.
  --provider <name>       Provider label. Default: Unknown.
  --primary-ip <ip>       Primary public IP. Auto-detected if omitted.
  --node-ip <ip>          Node public IP. Auto-detected if omitted.
  --server-url <url>      Server URL agents should use. Default: http://<primary-ip>:8900
  --api-token <token>     API token. Auto-read from primary /root/starnexus/config.yaml if omitted.
  --install-dir <path>    Remote agent install dir. Default: /root/starnexus
  --yes                   Do not prompt for confirmation.
  -h, --help              Show this help.

Example:
  scripts/onboard-node.sh --primary dmit --node sg-vps --node-id sg-vps --node-name "Singapore VPS" --provider "Oracle" --yes
EOF
}

info() { echo "==> $*"; }
ok() { echo "  ok: $*"; }
err() { echo "  error: $*" >&2; }

shell_quote() {
  printf "%q" "$1"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --primary) PRIMARY_SSH="$2"; shift 2 ;;
    --node) NODE_SSH="$2"; shift 2 ;;
    --node-id) NODE_ID="$2"; shift 2 ;;
    --node-name) NODE_NAME="$2"; shift 2 ;;
    --provider) PROVIDER="$2"; shift 2 ;;
    --primary-ip) PRIMARY_IP="$2"; shift 2 ;;
    --node-ip) NODE_IP="$2"; shift 2 ;;
    --server-url) SERVER_URL="$2"; shift 2 ;;
    --api-token) API_TOKEN="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --yes) YES=1; shift ;;
    -h|--help) usage; exit 0 ;;
    --*) err "unknown option: $1"; usage; exit 1 ;;
    *) err "unexpected argument: $1"; usage; exit 1 ;;
  esac
done

if [[ -z "$PRIMARY_SSH" || -z "$NODE_SSH" || -z "$NODE_ID" ]]; then
  err "--primary, --node, and --node-id are required"
  usage
  exit 1
fi

if [[ -z "$NODE_NAME" ]]; then
  NODE_NAME="$NODE_ID"
fi

info "Checking SSH access"
ssh "$PRIMARY_SSH" "true"
ssh "$NODE_SSH" "true"
ok "SSH access verified"

if [[ -z "$PRIMARY_IP" ]]; then
  info "Detecting primary public IP"
  PRIMARY_IP=$(ssh "$PRIMARY_SSH" "curl -fsS --connect-timeout 5 https://api.ipify.org || curl -fsS --connect-timeout 5 http://ip-api.com/line/?fields=query" 2>/dev/null)
  if [[ -z "$PRIMARY_IP" ]]; then
    err "failed to detect primary IP; pass --primary-ip"
    exit 1
  fi
fi

if [[ -z "$NODE_IP" ]]; then
  info "Detecting node public IP"
  NODE_IP=$(ssh "$NODE_SSH" "curl -fsS --connect-timeout 5 https://api.ipify.org || curl -fsS --connect-timeout 5 http://ip-api.com/line/?fields=query" 2>/dev/null)
  if [[ -z "$NODE_IP" ]]; then
    err "failed to detect node IP; pass --node-ip"
    exit 1
  fi
fi

if [[ -z "$SERVER_URL" ]]; then
  SERVER_URL="http://$PRIMARY_IP:8900"
fi

if [[ -z "$API_TOKEN" ]]; then
  info "Reading API token from primary"
  API_TOKEN=$(ssh "$PRIMARY_SSH" "python3 - <<'PY'
import re
from pathlib import Path
text = Path('/root/starnexus/config.yaml').read_text()
match = re.search(r'^api_token:\\s*[\"'\"']?([^\"'\"'\\n]+)', text, re.M)
print(match.group(1).strip() if match else '')
PY
")
  if [[ -z "$API_TOKEN" ]]; then
    err "failed to read API token; pass --api-token"
    exit 1
  fi
fi

echo ""
echo "StarNexus node onboarding"
echo "  Primary:   $PRIMARY_SSH ($PRIMARY_IP)"
echo "  Node:      $NODE_SSH ($NODE_IP)"
echo "  Node ID:   $NODE_ID"
echo "  Name:      $NODE_NAME"
echo "  Provider:  $PROVIDER"
echo "  Server:    $SERVER_URL"
echo "  Directory: $INSTALL_DIR"
echo ""

if [[ "$YES" -ne 1 ]]; then
  read -rp "Proceed? (y/N): " confirm
  if [[ ! "$confirm" =~ ^[yY]$ ]]; then
    echo "Aborted."
    exit 0
  fi
fi

info "Whitelisting node on primary firewall"
ssh "$PRIMARY_SSH" "
  set -e
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q 'Status: active'; then
    ufw allow from $(shell_quote "$NODE_IP") to any port 8900 comment $(shell_quote "starnexus-$NODE_ID")
  else
    iptables -C INPUT -p tcp -s $(shell_quote "$NODE_IP") --dport 8900 -j ACCEPT 2>/dev/null || iptables -I INPUT -p tcp -s $(shell_quote "$NODE_IP") --dport 8900 -j ACCEPT
    if command -v netfilter-persistent >/dev/null 2>&1; then netfilter-persistent save >/dev/null 2>&1 || true; fi
  fi
"
ok "Firewall rule installed"

info "Checking node-to-server connectivity"
ssh "$NODE_SSH" "curl -fsS --connect-timeout 8 $(shell_quote "$SERVER_URL/api/status") >/dev/null"
ok "Node can reach primary server"

info "Installing agent on node"
ssh "$NODE_SSH" "
  set -e
  curl -fsSL $(shell_quote "$SERVER_URL/install.sh") | bash -s -- \
    --server $(shell_quote "$SERVER_URL") \
    --token $(shell_quote "$API_TOKEN") \
    --node-id $(shell_quote "$NODE_ID") \
    --node-name $(shell_quote "$NODE_NAME") \
    --provider $(shell_quote "$PROVIDER") \
    --dir $(shell_quote "$INSTALL_DIR")
"
ok "Agent installer completed"

info "Verifying remote service"
ssh "$NODE_SSH" "systemctl is-active --quiet starnexus-agent"
ok "$NODE_SSH: starnexus-agent is active"

info "Waiting for dashboard report"
for _ in {1..8}; do
  if ssh "$PRIMARY_SSH" "curl -fsS http://127.0.0.1:8900/api/nodes | python3 -c 'import json,sys; d=json.load(sys.stdin); nodes=d.get(\"nodes\", d if isinstance(d, list) else []); raise SystemExit(0 if any(n.get(\"id\")==\"$NODE_ID\" for n in nodes) else 1)'"; then
    ok "Node appears in dashboard: $NODE_ID"
    exit 0
  fi
  sleep 5
done

err "agent is running, but node did not appear in dashboard yet; check journalctl -u starnexus-agent -n 30 on $NODE_SSH"
exit 1
