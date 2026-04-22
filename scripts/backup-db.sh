#!/usr/bin/env bash
set -euo pipefail

HOST="dmit"
REMOTE_DB="/root/starnexus/starnexus.db"
OUT_DIR="backups"
KEEP=0

usage() {
  cat <<'EOF'
Usage:
  scripts/backup-db.sh [options]

Options:
  --host <ssh-alias>     SSH host for the primary StarNexus server. Default: dmit
  --remote-db <path>     Remote SQLite database path. Default: /root/starnexus/starnexus.db
  --out-dir <path>       Local backup output directory. Default: backups
  --keep <count>         Keep only the newest count backups for this host. Default: keep all
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
    --keep) KEEP="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    --*) err "unknown option: $1"; usage; exit 1 ;;
    *) err "unexpected argument: $1"; usage; exit 1 ;;
  esac
done

if ! [[ "$KEEP" =~ ^[0-9]+$ ]]; then
  err "--keep must be a non-negative integer"
  exit 1
fi

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

low_priority() {
  if command -v ionice >/dev/null 2>&1; then
    ionice -c2 -n7 nice -n 10 "$@"
  else
    nice -n 10 "$@"
  fi
}

low_priority sqlite3 "$REMOTE_DB" ".backup '$tmp'"
low_priority gzip -1 -c "$tmp"
REMOTE

if [[ ! -s "$out_path" ]]; then
  rm -f "$out_path"
  err "backup output is empty"
  exit 1
fi

ok "backup written: $out_path"

if [[ "$KEEP" -gt 0 ]]; then
  removed=0
  while IFS= read -r old_backup; do
    [[ -z "$old_backup" ]] && continue
    rm -f "$old_backup"
    removed=$((removed + 1))
  done < <(ls -1t "$OUT_DIR"/starnexus-db-"$safe_host"-*.sqlite.gz 2>/dev/null | tail -n +"$((KEEP + 1))")
  if [[ "$removed" -gt 0 ]]; then
    ok "retention removed $removed old backup(s); kept newest $KEEP"
  fi
fi
