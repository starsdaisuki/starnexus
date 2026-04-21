#!/usr/bin/env bash
set -euo pipefail

HOST="dmit"
REMOTE_DB="/root/starnexus/starnexus.db"
OUT_DIR="backups"

usage() {
  cat <<'EOF'
Usage:
  scripts/backup-db.sh [options]

Options:
  --host <ssh-alias>     SSH host for the primary StarNexus server. Default: dmit
  --remote-db <path>     Remote SQLite database path. Default: /root/starnexus/starnexus.db
  --out-dir <path>       Local backup output directory. Default: backups
  -h, --help             Show this help.

Creates a consistent SQLite snapshot using sqlite3 ".backup", compresses it,
and writes a local .sqlite.gz file.
EOF
}

err() { echo "error: $*" >&2; }
info() { echo "==> $*"; }
ok() { echo "  ok: $*"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) HOST="$2"; shift 2 ;;
    --remote-db) REMOTE_DB="$2"; shift 2 ;;
    --out-dir) OUT_DIR="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    --*) err "unknown option: $1"; usage; exit 1 ;;
    *) err "unexpected argument: $1"; usage; exit 1 ;;
  esac
done

mkdir -p "$OUT_DIR"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
safe_host="${HOST//[^A-Za-z0-9_.-]/_}"
out_path="$OUT_DIR/starnexus-db-${safe_host}-${timestamp}.sqlite.gz"

info "Creating SQLite backup on $HOST"
ssh "$HOST" "REMOTE_DB='$REMOTE_DB' bash -s" <<'REMOTE' > "$out_path"
set -euo pipefail

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "sqlite3 is required on the remote host for a consistent backup" >&2
  exit 1
fi
if [[ ! -f "$REMOTE_DB" ]]; then
  echo "database not found: $REMOTE_DB" >&2
  exit 1
fi

tmp="$(mktemp /tmp/starnexus-db-backup.XXXXXX.sqlite)"
cleanup() { rm -f "$tmp"; }
trap cleanup EXIT

sqlite3 "$REMOTE_DB" ".backup '$tmp'"
gzip -c "$tmp"
REMOTE

if [[ ! -s "$out_path" ]]; then
  rm -f "$out_path"
  err "backup output is empty"
  exit 1
fi

ok "backup written: $out_path"
