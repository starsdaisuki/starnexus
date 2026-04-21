#!/usr/bin/env bash
set -euo pipefail

HOST="dmit"
REMOTE_DB="/root/starnexus/starnexus.db"
BACKUP=""
YES=0

usage() {
  cat <<'EOF'
Usage:
  scripts/restore-db.sh --backup <file.sqlite.gz|file.sqlite> [options]

Options:
  --backup <path>        Local backup file to restore. Required.
  --host <ssh-alias>     SSH host for the primary StarNexus server. Default: dmit
  --remote-db <path>     Remote SQLite database path. Default: /root/starnexus/starnexus.db
  --yes                  Do not prompt for confirmation.
  -h, --help             Show this help.

The script stops starnexus-server and starnexus-bot, saves the current DB as
<db>.pre-restore.<timestamp>, restores the backup, removes WAL/SHM sidecars,
starts services, and verifies /api/status.
EOF
}

err() { echo "error: $*" >&2; }
info() { echo "==> $*"; }
ok() { echo "  ok: $*"; }

shell_quote() {
  printf "%q" "$1"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --backup) BACKUP="$2"; shift 2 ;;
    --host) HOST="$2"; shift 2 ;;
    --remote-db) REMOTE_DB="$2"; shift 2 ;;
    --yes) YES=1; shift ;;
    -h|--help) usage; exit 0 ;;
    --*) err "unknown option: $1"; usage; exit 1 ;;
    *) err "unexpected argument: $1"; usage; exit 1 ;;
  esac
done

if [[ -z "$BACKUP" ]]; then
  err "--backup is required"
  usage
  exit 1
fi
if [[ ! -f "$BACKUP" ]]; then
  err "backup file not found: $BACKUP"
  exit 1
fi

echo "StarNexus database restore"
echo "  Host:      $HOST"
echo "  Remote DB: $REMOTE_DB"
echo "  Backup:    $BACKUP"
echo ""

if [[ "$YES" -ne 1 ]]; then
  read -rp "This will replace the remote database. Proceed? (y/N): " confirm
  if [[ ! "$confirm" =~ ^[yY]$ ]]; then
    echo "Aborted."
    exit 0
  fi
fi

remote_tmp="/tmp/starnexus-restore-$(date -u +%Y%m%dT%H%M%SZ).sqlite"
info "Uploading backup to $HOST"
case "$BACKUP" in
  *.gz)
    gzip -dc "$BACKUP" | ssh "$HOST" "cat > $(shell_quote "$remote_tmp")"
    ;;
  *)
    scp -q "$BACKUP" "$HOST:$remote_tmp"
    ;;
esac
ok "backup uploaded"

info "Restoring remote database"
ssh "$HOST" "REMOTE_DB=$(shell_quote "$REMOTE_DB") REMOTE_TMP=$(shell_quote "$remote_tmp") bash -s" <<'REMOTE'
set -euo pipefail

if [[ ! -s "$REMOTE_TMP" ]]; then
  echo "uploaded backup is empty: $REMOTE_TMP" >&2
  exit 1
fi

ts="$(date -u +%Y%m%dT%H%M%SZ)"
db_dir="$(dirname "$REMOTE_DB")"
mkdir -p "$db_dir"

systemctl stop starnexus-bot 2>/dev/null || true
systemctl stop starnexus-server 2>/dev/null || true

if [[ -f "$REMOTE_DB" ]]; then
  cp -a "$REMOTE_DB" "$REMOTE_DB.pre-restore.$ts"
fi
if [[ -f "$REMOTE_DB-wal" ]]; then
  cp -a "$REMOTE_DB-wal" "$REMOTE_DB-wal.pre-restore.$ts"
fi
if [[ -f "$REMOTE_DB-shm" ]]; then
  cp -a "$REMOTE_DB-shm" "$REMOTE_DB-shm.pre-restore.$ts"
fi

install -m 0600 "$REMOTE_TMP" "$REMOTE_DB"
rm -f "$REMOTE_DB-wal" "$REMOTE_DB-shm" "$REMOTE_TMP"

systemctl start starnexus-server
systemctl start starnexus-bot 2>/dev/null || true
sleep 3
systemctl is-active --quiet starnexus-server
curl -fsS http://127.0.0.1:8900/api/status >/dev/null
REMOTE

ok "restore completed and API verified"
