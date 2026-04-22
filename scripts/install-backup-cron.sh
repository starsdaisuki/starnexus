#!/usr/bin/env bash
set -euo pipefail

HOST="dmit"
REMOTE_DB="/root/starnexus/starnexus.db"
REMOTE_OUT_DIR="/root/starnexus/backups"
MINUTE="20"
HOUR="3"
KEEP="14"
SKIP_VERIFY=0

usage() {
  cat <<'EOF'
Usage:
  scripts/install-backup-cron.sh [options]

Options:
  --host <ssh-alias>       SSH host for the primary StarNexus server. Default: dmit
  --remote-db <path>       Remote SQLite database path. Default: /root/starnexus/starnexus.db
  --remote-out-dir <path>  Backup directory on the primary server. Default: /root/starnexus/backups
  --minute <0-59>          Cron minute. Default: 20
  --hour <0-23>            Cron hour in server local time. Default: 3
  --keep <count>           Keep newest count remote backups. Default: 14
  --skip-verify            Install script and cron without running an immediate backup
  -h, --help               Show this help.

Installs /root/starnexus/backup-db-local.sh and a cron entry tagged
starnexus-backup. The backup uses sqlite3 ".backup", gzip compression, and
retention on the primary VPS.
EOF
}

err() { echo "error: $*" >&2; }
info() { echo "==> $*"; }
ok() { echo "  ok: $*"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) HOST="$2"; shift 2 ;;
    --remote-db) REMOTE_DB="$2"; shift 2 ;;
    --remote-out-dir) REMOTE_OUT_DIR="$2"; shift 2 ;;
    --minute) MINUTE="$2"; shift 2 ;;
    --hour) HOUR="$2"; shift 2 ;;
    --keep) KEEP="$2"; shift 2 ;;
    --skip-verify) SKIP_VERIFY=1; shift ;;
    -h|--help) usage; exit 0 ;;
    --*) err "unknown option: $1"; usage; exit 1 ;;
    *) err "unexpected argument: $1"; usage; exit 1 ;;
  esac
done

for pair in "minute:$MINUTE:0:59" "hour:$HOUR:0:23" "keep:$KEEP:1:3650"; do
  IFS=: read -r name value min max <<< "$pair"
  if ! [[ "$value" =~ ^[0-9]+$ ]] || (( value < min || value > max )); then
    err "--$name must be an integer between $min and $max"
    exit 1
  fi
done

info "Installing remote backup script on $HOST"
ssh "$HOST" \
  "REMOTE_DB='$REMOTE_DB' REMOTE_OUT_DIR='$REMOTE_OUT_DIR' KEEP='$KEEP' bash -s" <<'REMOTE'
set -euo pipefail

mkdir -p /root/starnexus "$REMOTE_OUT_DIR"
cat > /root/starnexus/backup-db-local.sh <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

REMOTE_DB="${REMOTE_DB:-/root/starnexus/starnexus.db}"
REMOTE_OUT_DIR="${REMOTE_OUT_DIR:-/root/starnexus/backups}"
KEEP="${KEEP:-14}"

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "sqlite3 is required for StarNexus backups" >&2
  exit 1
fi
if [[ ! -f "$REMOTE_DB" ]]; then
  echo "database not found: $REMOTE_DB" >&2
  exit 1
fi

mkdir -p "$REMOTE_OUT_DIR"
tmp="$(mktemp /tmp/starnexus-db-backup.XXXXXX.sqlite)"
cleanup() { rm -f "$tmp"; }
trap cleanup EXIT

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
host="$(hostname | tr -c 'A-Za-z0-9_.-' '_')"
out="$REMOTE_OUT_DIR/starnexus-db-$host-$timestamp.sqlite.gz"

low_priority() {
  if command -v ionice >/dev/null 2>&1; then
    ionice -c2 -n7 nice -n 10 "$@"
  else
    nice -n 10 "$@"
  fi
}

low_priority sqlite3 "$REMOTE_DB" ".backup '$tmp'"
low_priority gzip -1 -c "$tmp" > "$out"

removed=0
while IFS= read -r old_backup; do
  [[ -z "$old_backup" ]] && continue
  rm -f "$old_backup"
  removed=$((removed + 1))
done < <(ls -1t "$REMOTE_OUT_DIR"/starnexus-db-*.sqlite.gz 2>/dev/null | tail -n +"$((KEEP + 1))")

echo "backup=$out removed=$removed keep=$KEEP"
SCRIPT
chmod 700 /root/starnexus/backup-db-local.sh
REMOTE
ok "remote script installed"

info "Installing cron entry"
ssh "$HOST" \
  "MINUTE='$MINUTE' HOUR='$HOUR' REMOTE_DB='$REMOTE_DB' REMOTE_OUT_DIR='$REMOTE_OUT_DIR' KEEP='$KEEP' bash -s" <<'REMOTE'
set -euo pipefail

entry="$MINUTE $HOUR * * * REMOTE_DB='$REMOTE_DB' REMOTE_OUT_DIR='$REMOTE_OUT_DIR' KEEP='$KEEP' /root/starnexus/backup-db-local.sh >> /root/starnexus/backup-db-local.log 2>&1 # starnexus-backup"
(crontab -l 2>/dev/null | grep -v 'starnexus-backup'; echo "$entry") | crontab -
REMOTE
ok "cron installed: $MINUTE $HOUR * * *"

if [[ "$SKIP_VERIFY" -eq 1 ]]; then
  ok "skipped immediate backup verification"
else
  info "Running one backup now for verification"
  ssh "$HOST" "REMOTE_DB='$REMOTE_DB' REMOTE_OUT_DIR='$REMOTE_OUT_DIR' KEEP='$KEEP' /root/starnexus/backup-db-local.sh"
  ok "backup cron verified"
fi
