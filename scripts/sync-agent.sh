#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="$ROOT_DIR/.dist/starnexus-agent"
INSTALL_DIR="/root/starnexus"
SERVICE="starnexus-agent"
BUILD=1
HOSTS=()

usage() {
  cat <<'EOF'
Usage:
  scripts/sync-agent.sh [options] <ssh-host> [<ssh-host>...]

Options:
  --binary <path>       Use an existing Linux agent binary.
  --install-dir <path>  Remote install directory. Default: /root/starnexus
  --service <name>      Remote systemd service name. Default: starnexus-agent
  --no-build            Do not build locally before uploading.
  -h, --help            Show this help.

This updates an existing StarNexus agent safely:
  - keeps remote config files unchanged
  - uploads starnexus-agent.new first
  - backs up the previous binary as starnexus-agent.prev.<timestamp>
  - restarts only the agent systemd service
EOF
}

info() { echo "==> $*"; }
ok() { echo "  ok: $*"; }
err() { echo "  error: $*" >&2; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      BINARY="$2"
      BUILD=0
      shift 2
      ;;
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --service)
      SERVICE="$2"
      shift 2
      ;;
    --no-build)
      BUILD=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --*)
      err "unknown option: $1"
      usage
      exit 1
      ;;
    *)
      HOSTS+=("$1")
      shift
      ;;
  esac
done

if [[ ${#HOSTS[@]} -eq 0 ]]; then
  err "at least one SSH host is required"
  usage
  exit 1
fi

if [[ "$BUILD" -eq 1 ]]; then
  info "Building Linux amd64 agent"
  mkdir -p "$(dirname "$BINARY")"
  (cd "$ROOT_DIR/agent" && GOOS=linux GOARCH=amd64 go build -o "$BINARY" .)
fi

if [[ ! -x "$BINARY" ]]; then
  err "binary not found or not executable: $BINARY"
  exit 1
fi

for host in "${HOSTS[@]}"; do
  info "Syncing agent to $host"

  ssh "$host" "test -d '$INSTALL_DIR'" || {
    err "$host: install dir does not exist: $INSTALL_DIR"
    exit 1
  }

  ssh "$host" "test -f '$INSTALL_DIR/starnexus-agent'" || {
    err "$host: existing agent binary not found at $INSTALL_DIR/starnexus-agent"
    exit 1
  }

  if ! ssh "$host" "systemctl cat '$SERVICE' >/dev/null 2>&1"; then
    err "$host: systemd service not found: $SERVICE"
    exit 1
  fi

  ssh "$host" "cat > '$INSTALL_DIR/starnexus-agent.new' && chmod +x '$INSTALL_DIR/starnexus-agent.new'" < "$BINARY"
  ssh "$host" "
    set -e
    cd '$INSTALL_DIR'
    cp -p starnexus-agent starnexus-agent.prev.\$(date +%s)
    mv starnexus-agent.new starnexus-agent
    systemctl restart '$SERVICE'
    sleep 2
    systemctl is-active --quiet '$SERVICE'
  "

  ok "$host: $SERVICE is active"
done
