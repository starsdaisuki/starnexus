#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# StarNexus Node Manager
# Add, remove, or update monitored nodes from your local machine.
#
# Usage:
#   ./scripts/manage-node.sh add
#   ./scripts/manage-node.sh remove
#   ./scripts/manage-node.sh update-ip
#   ./scripts/manage-node.sh list
#
# On first run, saves primary server info to ~/.starnexus.env
# so you never have to type it again.
#
# Prerequisites:
#   - SSH config aliases for your VPS (e.g. "dmit", "sonet")
#   - Primary server already deployed via deploy-server.sh
# ============================================================

# shellcheck disable=SC2034  # reserved for future relative-path resolution
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$HOME/.starnexus.env"

PRIMARY_SSH=""
PRIMARY_IP=""
PRIMARY_SSH_PORT=""
PRIMARY_NODE_ID=""
API_TOKEN=""

# ============================================================
# Helpers
# ============================================================

info()  { echo -e "\033[0;34m==>\033[0m $*"; }
ok()    { echo -e "\033[0;32m  ✓\033[0m $*"; }
warn()  { echo -e "\033[0;33m  !\033[0m $*"; }
err()   { echo -e "\033[0;31m  ✗\033[0m $*" >&2; }

ask() {
  local prompt="$1" var="$2" default="${3:-}"
  if [[ -n "$default" ]]; then
    read -rp "$prompt [$default]: " val
    eval "$var=\"\${val:-$default}\""
  else
    read -rp "$prompt: " val
    eval "$var=\"\$val\""
  fi
}

ask_required() {
  local prompt="$1" var="$2"
  local val=""
  while [[ -z "$val" ]]; do
    read -rp "$prompt: " val
  done
  eval "$var=\"\$val\""
}

# Find agent config file on a remote host (handles both naming conventions)
remote_agent_config() {
  local host="$1"
  ssh "$host" "
    if [ -f /root/starnexus/agent-config.yaml ]; then
      echo /root/starnexus/agent-config.yaml
    else
      echo /root/starnexus/config.yaml
    fi
  " 2>/dev/null
}

# Read a YAML value from remote file: remote_yaml_val <ssh-host> <file> <key>
remote_yaml_val() {
  ssh "$1" "grep '^$3:' $2 2>/dev/null | head -1 | sed 's/^[^:]*: *\"\\{0,1\\}\\([^\"]*\\)\"\\{0,1\\}/\\1/'" 2>/dev/null || true
}

# ============================================================
# Primary server detection (with caching to ~/.starnexus.env)
# ============================================================

save_env() {
  cat > "$ENV_FILE" << EOF
# StarNexus primary server config (auto-generated)
PRIMARY_SSH="$PRIMARY_SSH"
PRIMARY_IP="$PRIMARY_IP"
PRIMARY_SSH_PORT="$PRIMARY_SSH_PORT"
PRIMARY_NODE_ID="$PRIMARY_NODE_ID"
API_TOKEN="$API_TOKEN"
EOF
  chmod 600 "$ENV_FILE"
}

load_or_detect_primary() {
  # Try loading saved config
  if [[ -f "$ENV_FILE" ]]; then
    # shellcheck source=/dev/null
    source "$ENV_FILE"
    if [[ -n "$PRIMARY_SSH" && -n "$API_TOKEN" ]]; then
      # Verify it's still reachable
      if ssh "$PRIMARY_SSH" "curl -sf --connect-timeout 3 http://localhost:8900/api/status" &>/dev/null; then
        ok "Primary: $PRIMARY_SSH ($PRIMARY_IP)"
        return 0
      else
        warn "Saved primary ($PRIMARY_SSH) unreachable, reconfiguring..."
      fi
    fi
  fi

  # First time setup or reconfigure
  echo ""
  info "First-time setup: configure primary server"
  ask_required "Primary server SSH alias (e.g. dmit)" PRIMARY_SSH

  info "Reading primary server config..."
  local agent_cfg
  agent_cfg=$(remote_agent_config "$PRIMARY_SSH")

  PRIMARY_IP=$(remote_yaml_val "$PRIMARY_SSH" "$agent_cfg" "public_ip")
  if [[ -z "$PRIMARY_IP" ]]; then
    ask_required "Primary server public IP" PRIMARY_IP
  else
    ok "Primary IP: $PRIMARY_IP"
  fi

  PRIMARY_NODE_ID=$(remote_yaml_val "$PRIMARY_SSH" "$agent_cfg" "node_id")
  ok "Primary node ID: $PRIMARY_NODE_ID"

  PRIMARY_SSH_PORT=$(ssh "$PRIMARY_SSH" "ss -tlnp | grep sshd | grep '0.0.0.0' | head -1 | sed 's/.*:\\([0-9]*\\) .*/\\1/'" 2>/dev/null || echo "22")
  ok "Primary SSH port: $PRIMARY_SSH_PORT"

  API_TOKEN=$(ssh "$PRIMARY_SSH" "grep api_token /root/starnexus/config.yaml 2>/dev/null | head -1 | sed 's/.*: *\"\\(.*\\)\"/\\1/'" 2>/dev/null) || true
  if [[ -z "$API_TOKEN" ]]; then
    ask_required "API token" API_TOKEN
  else
    ok "API token: ${API_TOKEN:0:8}..."
  fi

  save_env
  ok "Saved to $ENV_FILE (won't ask again)"
}

# ============================================================
# LIST
# ============================================================

cmd_list() {
  load_or_detect_primary

  echo ""
  info "Nodes:"
  ssh "$PRIMARY_SSH" "curl -s http://localhost:8900/api/nodes" | python3 -c "
import sys, json
data = json.load(sys.stdin)
nodes = data.get('nodes', data) if isinstance(data, dict) else data
print(f'  {\"ID\":<20s} {\"Name\":<20s} {\"IP\":<18s} {\"Status\":<10s}')
print(f'  {\"─\"*20} {\"─\"*20} {\"─\"*18} {\"─\"*10}')
for n in nodes:
    print(f'  {n[\"id\"]:<20s} {n[\"name\"]:<20s} {n.get(\"ip_address\",\"?\"):<18s} {n[\"status\"]:<10s}')
" 2>/dev/null

  echo ""
  info "Links:"
  ssh "$PRIMARY_SSH" "curl -s http://localhost:8900/api/links" | python3 -c "
import sys, json
data = json.load(sys.stdin)
links = data.get('links', data) if isinstance(data, dict) else data
if not links:
    print('  (none)')
else:
    for l in links:
        ms = l['latency_ms']
        lat = f'{ms:.1f}ms' if ms >= 0 else 'timeout'
        print(f'  {l[\"source_node_id\"]:<18s} -> {l[\"target_node_id\"]:<18s} {lat:<10s} {l[\"status\"]}')
" 2>/dev/null
}

# ============================================================
# ADD NODE
# ============================================================

cmd_add() {
  echo ""
  echo "============================================================"
  echo "  Add a new monitored node"
  echo "============================================================"
  echo ""

  load_or_detect_primary

  echo ""
  info "New node info"
  ask_required "SSH alias for the new node (from ~/.ssh/config)" NODE_SSH
  ask_required "Node ID (lowercase, no spaces, e.g. tokyo-sonet)" NODE_ID
  ask "Display name (e.g. Tokyo So-net)" NODE_NAME "$NODE_ID"
  ask "Provider name" PROVIDER "Unknown"

  # Auto-detect IP and SSH port from the new node
  info "Detecting node IP and SSH port..."
  NODE_IP=$(ssh "$NODE_SSH" "curl -s http://ip-api.com/json/ | python3 -c \"import sys,json; print(json.load(sys.stdin)['query'])\"" 2>/dev/null) || true
  if [[ -n "$NODE_IP" ]]; then
    ok "Detected IP: $NODE_IP"
  else
    ask_required "Node public IP" NODE_IP
  fi

  NODE_SSH_PORT=$(ssh "$NODE_SSH" "ss -tlnp | grep sshd | grep '0.0.0.0' | head -1 | sed 's/.*:\([0-9]*\) .*/\1/'" 2>/dev/null) || true
  if [[ -n "$NODE_SSH_PORT" ]]; then
    ok "Detected SSH port: $NODE_SSH_PORT"
  else
    ask "SSH port on this node" NODE_SSH_PORT "22"
  fi

  echo ""
  echo "  Summary:"
  echo "    Node:     $NODE_ID ($NODE_NAME)"
  echo "    IP:       $NODE_IP"
  echo "    SSH port: $NODE_SSH_PORT"
  echo "    Provider: $PROVIDER"
  echo "    Primary:  $PRIMARY_SSH ($PRIMARY_IP)"
  echo ""
  read -rp "  Proceed? (Y/n): " CONFIRM
  [[ "$CONFIRM" =~ ^[nN] ]] && { echo "Aborted."; exit 0; }

  # --- Step 1: Whitelist on primary server ---
  echo ""
  info "Step 1: Whitelist $NODE_IP on primary server firewall..."

  if ssh "$PRIMARY_SSH" "command -v ufw &>/dev/null && ufw status | grep -q 'Status: active'" 2>/dev/null; then
    ssh "$PRIMARY_SSH" "
      ufw allow from $NODE_IP to any port 8900 comment 'starnexus-$NODE_ID'
      ufw allow from $NODE_IP proto icmp comment 'starnexus-$NODE_ID-icmp'
    " 2>/dev/null
    ok "UFW rules added"
  else
    ssh "$PRIMARY_SSH" "
      iptables -C INPUT -p tcp -s $NODE_IP --dport 8900 -j ACCEPT 2>/dev/null || iptables -I INPUT -p tcp -s $NODE_IP --dport 8900 -j ACCEPT
      iptables -C INPUT -p icmp -s $NODE_IP -j ACCEPT 2>/dev/null || iptables -I INPUT -p icmp -s $NODE_IP -j ACCEPT
      if command -v netfilter-persistent &>/dev/null; then netfilter-persistent save 2>/dev/null; fi
    " 2>/dev/null
    ok "iptables rules added and saved"
  fi

  # Test connectivity
  info "Testing connectivity: $NODE_SSH -> primary:8900..."
  if ssh "$NODE_SSH" "curl -sf --connect-timeout 5 http://$PRIMARY_IP:8900/api/status" &>/dev/null; then
    ok "Connection OK"
  else
    err "Cannot reach primary server from node! Check firewall rules."
    exit 1
  fi

  # --- Step 2: Install agent ---
  info "Step 2: Installing starnexus-agent on $NODE_SSH..."
  ssh "$NODE_SSH" "curl -sSL http://$PRIMARY_IP:8900/install.sh | bash -s -- \
    --server http://$PRIMARY_IP:8900 \
    --token $API_TOKEN \
    --node-id \"$NODE_ID\" \
    --node-name \"$NODE_NAME\" \
    --provider \"$PROVIDER\""
  ok "Agent installed"

  # --- Step 3: Enable connection tracking + probe to primary ---
  info "Step 3: Enabling connection tracking and probe..."
  ssh "$NODE_SSH" "
    # Only append if not already configured
    if ! grep -q 'geoip_db_path' /root/starnexus/config.yaml 2>/dev/null; then
      cat >> /root/starnexus/config.yaml << 'ENDCFG'
geoip_db_path: \"./GeoLite2-City.mmdb\"
connection_report_interval_seconds: 5
ENDCFG
    fi
    # Add probe_targets if not present
    if ! grep -q 'probe_targets' /root/starnexus/config.yaml 2>/dev/null; then
      cat >> /root/starnexus/config.yaml << 'ENDCFG'
probe_targets:
ENDCFG
    fi
    # Add this specific probe target
    if ! grep -q '$PRIMARY_NODE_ID' /root/starnexus/config.yaml 2>/dev/null; then
      cat >> /root/starnexus/config.yaml << 'ENDCFG'
  - node_id: \"$PRIMARY_NODE_ID\"
    host: \"$PRIMARY_IP\"
    port: $PRIMARY_SSH_PORT
ENDCFG
    fi
    systemctl restart starnexus-agent
  "
  ok "Agent configured with probe + connection tracking"

  # --- Step 4: Add probe target on primary ---
  info "Step 4: Adding probe target on primary server..."
  local primary_cfg
  primary_cfg=$(remote_agent_config "$PRIMARY_SSH")

  ssh "$PRIMARY_SSH" "
    # Add probe_targets key if not present
    if ! grep -q 'probe_targets' $primary_cfg 2>/dev/null; then
      echo 'probe_targets:' >> $primary_cfg
    fi
    # Add this specific probe target
    if ! grep -q '$NODE_ID' $primary_cfg 2>/dev/null; then
      cat >> $primary_cfg << 'ENDCFG'
  - node_id: \"$NODE_ID\"
    host: \"$NODE_IP\"
    port: $NODE_SSH_PORT
ENDCFG
    fi
    systemctl restart starnexus-agent
  "
  ok "Primary agent updated"

  # --- Step 5: Fix panel binding if 3x-ui/x-ui/s-ui detected ---
  info "Step 5: Checking for management panels..."
  PANEL_EXPOSED=$(ssh "$NODE_SSH" "ss -tlnp | grep -E 'x-ui|s-ui' | grep -v '127.0.0.1' | grep -v '\\[::1\\]'" 2>/dev/null || true)
  if [[ -n "$PANEL_EXPOSED" ]]; then
    warn "Management panel exposed on public interface!"
    echo "$PANEL_EXPOSED" | sed 's/^/    /'
    read -rp "  Fix panel to bind 127.0.0.1 only? (Y/n): " FIX_PANEL
    if [[ ! "$FIX_PANEL" =~ ^[nN] ]]; then
      fix_panel_binding "$NODE_SSH" "$NODE_ID"
    fi
  else
    ok "No exposed panels found"
  fi

  # --- Done ---
  echo ""
  echo "============================================================"
  echo "  Node $NODE_ID added successfully!"
  echo "============================================================"

  sleep 5
  echo ""
  info "Verifying..."
  ssh "$PRIMARY_SSH" "curl -s http://localhost:8900/api/status"
  echo ""
}

# ============================================================
# FIX PANEL BINDING (shared helper)
# ============================================================

fix_panel_binding() {
  local host="$1" label="$2"

  # Find panel DB
  local panel_db=""
  if ssh "$host" "test -f /etc/x-ui/x-ui.db" 2>/dev/null; then
    panel_db="/etc/x-ui/x-ui.db"
  elif ssh "$host" "test -f /usr/local/s-ui/db/s-ui.db" 2>/dev/null; then
    panel_db="/usr/local/s-ui/db/s-ui.db"
  fi

  if [[ -z "$panel_db" ]]; then
    warn "Could not find panel database. Fix manually."
    return
  fi

  local tmp_db="/tmp/panel-fix-$label.db"
  scp "$host:$panel_db" "$tmp_db"

  sqlite3 "$tmp_db" "
    UPDATE settings SET value='127.0.0.1' WHERE key='webListen';
    INSERT OR IGNORE INTO settings (key, value) VALUES ('webListen', '127.0.0.1');
    UPDATE settings SET value='127.0.0.1' WHERE key='subListen';
    INSERT OR IGNORE INTO settings (key, value) VALUES ('subListen', '127.0.0.1');
  "

  local panel_svc
  panel_svc=$(ssh "$host" "systemctl list-units --type=service --state=running --no-legend | grep -oE '(x-ui|s-ui)' | head -1" 2>/dev/null || echo "x-ui")

  ssh "$host" "systemctl stop $panel_svc"
  scp "$tmp_db" "$host:$panel_db"
  ssh "$host" "systemctl start $panel_svc"
  rm -f "$tmp_db"
  ok "Panel now binds to 127.0.0.1 only (access via SSH tunnel)"
}

# ============================================================
# REMOVE NODE
# ============================================================

cmd_remove() {
  echo ""
  echo "============================================================"
  echo "  Remove a monitored node"
  echo "============================================================"
  echo ""

  load_or_detect_primary

  # Show current nodes
  echo ""
  info "Current nodes:"
  ssh "$PRIMARY_SSH" "curl -s http://localhost:8900/api/nodes" | python3 -c "
import sys, json
data = json.load(sys.stdin)
nodes = data.get('nodes', data) if isinstance(data, dict) else data
for n in nodes:
    marker = ' (primary)' if n['id'] == '$PRIMARY_NODE_ID' else ''
    print(f'    {n[\"id\"]:<20s} {n[\"name\"]:<20s} {n.get(\"ip_address\",\"?\"):<18s} {n[\"status\"]}{marker}')
" 2>/dev/null
  echo ""

  ask_required "Node ID to remove" NODE_ID

  # Don't allow removing primary
  if [[ "$NODE_ID" == "$PRIMARY_NODE_ID" ]]; then
    err "Cannot remove the primary server node!"
    echo "    Use deploy-server.sh to migrate to a new primary first."
    exit 1
  fi

  # Get node IP from API
  NODE_IP=$(ssh "$PRIMARY_SSH" "curl -s http://localhost:8900/api/nodes" | python3 -c "
import sys, json
data = json.load(sys.stdin)
nodes = data.get('nodes', data) if isinstance(data, dict) else data
for n in nodes:
    if n['id'] == '$NODE_ID':
        print(n.get('ip_address', ''))
        break
" 2>/dev/null)

  if [[ -z "$NODE_IP" ]]; then
    err "Node '$NODE_ID' not found in server database"
    exit 1
  fi

  ask "SSH alias for this node (Enter to skip — won't uninstall agent)" NODE_SSH ""

  echo ""
  echo "  Will remove:"
  echo "    Node:  $NODE_ID ($NODE_IP)"
  echo "    - Delete from server DB (node + metrics + links + history)"
  echo "    - Remove firewall rules on primary"
  echo "    - Remove probe target from primary agent config"
  if [[ -n "$NODE_SSH" ]]; then
    echo "    - Stop and uninstall agent on $NODE_SSH"
  fi
  echo ""
  read -rp "  Proceed? (y/N): " CONFIRM
  [[ ! "$CONFIRM" =~ ^[yY] ]] && { echo "Aborted."; exit 0; }

  # --- Step 1: Stop agent on the node ---
  if [[ -n "$NODE_SSH" ]]; then
    info "Step 1: Stopping agent on $NODE_SSH..."
    ssh "$NODE_SSH" "
      systemctl disable --now starnexus-agent 2>/dev/null || true
      rm -f /etc/systemd/system/starnexus-agent.service
      systemctl daemon-reload 2>/dev/null || true
    " 2>/dev/null
    ok "Agent stopped and disabled"
  else
    info "Step 1: Skipping agent uninstall (no SSH alias)"
  fi

  # --- Step 2: Delete node from server (cascades to links + metrics) ---
  info "Step 2: Deleting node from database..."
  HTTP_CODE=$(ssh "$PRIMARY_SSH" "curl -s -o /dev/null -w '%{http_code}' -X DELETE http://localhost:8900/api/nodes/$NODE_ID -H 'Authorization: Bearer $API_TOKEN'" 2>/dev/null)
  if [[ "$HTTP_CODE" == "200" || "$HTTP_CODE" == "204" ]]; then
    ok "Node deleted (including links, metrics, history)"
  else
    warn "DELETE returned HTTP $HTTP_CODE (may already be removed)"
  fi

  # --- Step 3: Remove firewall rules ---
  info "Step 3: Removing firewall rules for $NODE_IP..."
  if ssh "$PRIMARY_SSH" "command -v ufw &>/dev/null && ufw status | grep -q 'Status: active'" 2>/dev/null; then
    ssh "$PRIMARY_SSH" "
      ufw status numbered | grep '$NODE_IP' | grep -oP '^\[ *\K[0-9]+' | sort -rn | while read num; do
        echo y | ufw delete \$num
      done
    " 2>/dev/null
    ok "UFW rules removed"
  else
    ssh "$PRIMARY_SSH" "
      while iptables -D INPUT -p tcp -s $NODE_IP --dport 8900 -j ACCEPT 2>/dev/null; do :; done
      while iptables -D INPUT -p icmp -s $NODE_IP -j ACCEPT 2>/dev/null; do :; done
      if command -v netfilter-persistent &>/dev/null; then netfilter-persistent save 2>/dev/null; fi
    " 2>/dev/null
    ok "iptables rules removed and saved"
  fi

  # --- Step 4: Remove probe target from primary agent config ---
  info "Step 4: Removing probe target from primary agent config..."
  local primary_cfg
  primary_cfg=$(remote_agent_config "$PRIMARY_SSH")

  ssh "$PRIMARY_SSH" "
    if grep -q '$NODE_ID' $primary_cfg 2>/dev/null; then
      sed -i '/$NODE_ID/,+2d' $primary_cfg
      systemctl restart starnexus-agent
      echo 'done'
    else
      echo 'not found (already clean)'
    fi
  " 2>/dev/null
  ok "Probe target removed, agent restarted"

  # --- Done ---
  echo ""
  echo "============================================================"
  echo "  Node $NODE_ID removed!"
  echo "============================================================"
  echo ""

  info "Current status:"
  ssh "$PRIMARY_SSH" "curl -s http://localhost:8900/api/status"
  echo ""

  if [[ -n "$NODE_SSH" ]]; then
    echo "  Agent files still at ~/starnexus/ on the node."
    echo "  To fully clean up: ssh $NODE_SSH 'rm -rf ~/starnexus'"
  fi
}

# ============================================================
# UPDATE IP (node changed IP, e.g. VPS migration)
# ============================================================

cmd_update_ip() {
  echo ""
  echo "============================================================"
  echo "  Update a node's IP address"
  echo "============================================================"
  echo ""

  load_or_detect_primary

  # Show current nodes
  echo ""
  info "Current nodes:"
  ssh "$PRIMARY_SSH" "curl -s http://localhost:8900/api/nodes" | python3 -c "
import sys, json
data = json.load(sys.stdin)
nodes = data.get('nodes', data) if isinstance(data, dict) else data
for n in nodes:
    print(f'    {n[\"id\"]:<20s} {n.get(\"ip_address\",\"?\"):<18s} {n[\"status\"]}')
" 2>/dev/null
  echo ""

  ask_required "Node ID to update" NODE_ID
  ask_required "New IP address" NEW_IP

  # Get old IP
  OLD_IP=$(ssh "$PRIMARY_SSH" "curl -s http://localhost:8900/api/nodes" | python3 -c "
import sys, json
data = json.load(sys.stdin)
nodes = data.get('nodes', data) if isinstance(data, dict) else data
for n in nodes:
    if n['id'] == '$NODE_ID':
        print(n.get('ip_address', ''))
        break
" 2>/dev/null)

  if [[ -z "$OLD_IP" ]]; then
    err "Node '$NODE_ID' not found"
    exit 1
  fi

  if [[ "$OLD_IP" == "$NEW_IP" ]]; then
    ok "IP unchanged ($OLD_IP), nothing to do"
    exit 0
  fi

  ask "New SSH port (Enter to keep current)" NEW_SSH_PORT ""

  echo ""
  echo "  Will update:"
  echo "    Node:    $NODE_ID"
  echo "    Old IP:  $OLD_IP"
  echo "    New IP:  $NEW_IP"
  echo "    - Swap firewall rules on primary"
  echo "    - Update probe target IP in primary agent config"
  echo ""
  read -rp "  Proceed? (Y/n): " CONFIRM
  [[ "$CONFIRM" =~ ^[nN] ]] && { echo "Aborted."; exit 0; }

  # --- Step 1: Update firewall ---
  info "Step 1: Updating firewall rules..."
  if ssh "$PRIMARY_SSH" "command -v ufw &>/dev/null && ufw status | grep -q 'Status: active'" 2>/dev/null; then
    ssh "$PRIMARY_SSH" "
      # Remove old
      ufw status numbered | grep '$OLD_IP' | grep -oP '^\[ *\K[0-9]+' | sort -rn | while read num; do
        echo y | ufw delete \$num
      done
      # Add new
      ufw allow from $NEW_IP to any port 8900 comment 'starnexus-$NODE_ID'
      ufw allow from $NEW_IP proto icmp comment 'starnexus-$NODE_ID-icmp'
    " 2>/dev/null
    ok "UFW rules swapped"
  else
    ssh "$PRIMARY_SSH" "
      while iptables -D INPUT -p tcp -s $OLD_IP --dport 8900 -j ACCEPT 2>/dev/null; do :; done
      while iptables -D INPUT -p icmp -s $OLD_IP -j ACCEPT 2>/dev/null; do :; done
      iptables -I INPUT -p tcp -s $NEW_IP --dport 8900 -j ACCEPT
      iptables -I INPUT -p icmp -s $NEW_IP -j ACCEPT
      if command -v netfilter-persistent &>/dev/null; then netfilter-persistent save 2>/dev/null; fi
    " 2>/dev/null
    ok "iptables rules swapped and saved"
  fi

  # --- Step 2: Update probe target in primary agent config ---
  info "Step 2: Updating probe target IP..."
  local primary_cfg
  primary_cfg=$(remote_agent_config "$PRIMARY_SSH")

  ssh "$PRIMARY_SSH" "
    sed -i 's|host: \"$OLD_IP\"|host: \"$NEW_IP\"|g' $primary_cfg
    $([ -n "${NEW_SSH_PORT:-}" ] && echo "sed -i '/$NODE_ID/{n;n;s|port: [0-9]*|port: $NEW_SSH_PORT|}' $primary_cfg")
    systemctl restart starnexus-agent
  " 2>/dev/null
  ok "Primary agent config updated"

  # --- Step 3: Update ~/.starnexus.env if this is the primary ---
  if [[ "$NODE_ID" == "$PRIMARY_NODE_ID" ]]; then
    PRIMARY_IP="$NEW_IP"
    [[ -n "${NEW_SSH_PORT:-}" ]] && PRIMARY_SSH_PORT="$NEW_SSH_PORT"
    save_env
    ok "Updated ~/.starnexus.env with new primary IP"
  fi

  echo ""
  echo "============================================================"
  echo "  IP updated: $OLD_IP -> $NEW_IP"
  echo "============================================================"
  echo ""
  echo "  Note: If the agent is already running on the new IP,"
  echo "  it will reconnect automatically. If the VPS changed,"
  echo "  re-run the install script on the new machine."
}

# ============================================================
# RECONFIGURE (change primary server)
# ============================================================

cmd_reconfig() {
  info "Clearing saved primary server config..."
  rm -f "$ENV_FILE"
  ok "Removed $ENV_FILE"
  echo ""
  info "Run any command to set up the new primary:"
  echo "    $0 list"
}

# ============================================================
# Main
# ============================================================

case "${1:-}" in
  add)       cmd_add ;;
  remove)    cmd_remove ;;
  update-ip) cmd_update_ip ;;
  list)      cmd_list ;;
  reconfig)  cmd_reconfig ;;
  *)
    echo "StarNexus Node Manager"
    echo ""
    echo "Usage: $0 <command>"
    echo ""
    echo "Commands:"
    echo "  add         Add a new monitored node"
    echo "  remove      Remove a node from monitoring"
    echo "  update-ip   Change a node's IP (firewall + probe update)"
    echo "  list        Show all nodes and links"
    echo "  reconfig    Change the primary server"
    echo ""
    echo "Config: $ENV_FILE (auto-created on first run)"
    echo ""
    echo "Examples:"
    echo "  $0 add          # Install agent, configure firewall & probes"
    echo "  $0 remove       # Uninstall agent, clean up everything"
    echo "  $0 update-ip    # VPS changed IP? Update firewall + probe"
    echo "  $0 reconfig     # Switch to a different primary server"
    ;;
esac
