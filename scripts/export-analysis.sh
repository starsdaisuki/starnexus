#!/usr/bin/env bash
set -euo pipefail

HOST="dmit"
REMOTE_DB="/root/starnexus/starnexus.db"
REMOTE_EXPERIMENTS="/root/starnexus/analysis-output/experiments.jsonl"
BACKUP_DIR="backups"
OUT_ROOT="analysis-output"
HOURS="168"
KEEP_BACKUPS="5"
KEEP_RUNS="0"
FROM_BACKUP=""

usage() {
  cat <<'EOF'
Usage:
  scripts/export-analysis.sh [options]

Options:
  --host <ssh-alias>             SSH host for the primary StarNexus server. Default: dmit
  --remote-db <path>             Remote SQLite database path. Default: /root/starnexus/starnexus.db
  --remote-experiments <path>    Remote experiment labels path. Default: /root/starnexus/analysis-output/experiments.jsonl
  --from-backup <path.sqlite.gz> Use an existing local backup instead of creating a new one
  --backup-dir <path>            Local backup directory. Default: backups
  --out-root <path>              Analysis output root. Default: analysis-output
  --hours <count>                Lookback window in hours. Default: 168
  --keep-backups <count>         Keep newest local backups for host. Default: 5
  --keep-runs <count>            Keep newest timestamped analysis runs. 0 keeps all. Default: 0
  -h, --help                     Show this help.

Creates a timestamped analysis run under <out-root>/runs/<timestamp>/ and
updates <out-root>/latest to point at it. The run contains CSV exports,
analytics.json, report.md, copied experiment labels, and manifest.json.
EOF
}

err() { echo "error: $*" >&2; }
info() { echo "==> $*"; }
ok() { echo "  ok: $*"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) HOST="$2"; shift 2 ;;
    --remote-db) REMOTE_DB="$2"; shift 2 ;;
    --remote-experiments) REMOTE_EXPERIMENTS="$2"; shift 2 ;;
    --from-backup) FROM_BACKUP="$2"; shift 2 ;;
    --backup-dir) BACKUP_DIR="$2"; shift 2 ;;
    --out-root) OUT_ROOT="$2"; shift 2 ;;
    --hours) HOURS="$2"; shift 2 ;;
    --keep-backups) KEEP_BACKUPS="$2"; shift 2 ;;
    --keep-runs) KEEP_RUNS="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    --*) err "unknown option: $1"; usage; exit 1 ;;
    *) err "unexpected argument: $1"; usage; exit 1 ;;
  esac
done

for pair in "hours:$HOURS:1:87600" "keep-backups:$KEEP_BACKUPS:0:10000" "keep-runs:$KEEP_RUNS:0:10000"; do
  IFS=: read -r name value min max <<< "$pair"
  if ! [[ "$value" =~ ^[0-9]+$ ]] || (( value < min || value > max )); then
    err "--$name must be an integer between $min and $max"
    exit 1
  fi
done

if [[ -n "$FROM_BACKUP" && ! -f "$FROM_BACKUP" ]]; then
  err "backup not found: $FROM_BACKUP"
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  err "go is required to run the local analysis CLI"
  exit 1
fi
if ! command -v sqlite3 >/dev/null 2>&1; then
  err "sqlite3 is required to verify the backup"
  exit 1
fi

repo_root="$(pwd)"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
runs_dir="$OUT_ROOT/runs"
run_dir="$runs_dir/$timestamp"
mkdir -p "$run_dir" "$BACKUP_DIR"
run_dir_abs="$(cd "$run_dir" && pwd)"

backup_path="$FROM_BACKUP"
if [[ -z "$backup_path" ]]; then
  info "Creating production database backup"
  backup_log="$(scripts/backup-db.sh --host "$HOST" --remote-db "$REMOTE_DB" --out-dir "$BACKUP_DIR" --keep "$KEEP_BACKUPS")"
  printf '%s\n' "$backup_log"
  backup_path="$(printf '%s\n' "$backup_log" | sed -n 's/^  ok: backup written: //p' | tail -n 1)"
  if [[ -z "$backup_path" || ! -f "$backup_path" ]]; then
    err "could not determine backup path from backup-db.sh output"
    exit 1
  fi
else
  info "Using existing backup: $backup_path"
fi

experiments_path="$run_dir/experiments.jsonl"
info "Fetching experiment labels from $HOST"
if ssh "$HOST" "test -f '$REMOTE_EXPERIMENTS' && cat '$REMOTE_EXPERIMENTS' || true" > "$experiments_path"; then
  if [[ -s "$experiments_path" ]]; then
    ok "experiment labels written: $experiments_path"
  else
    rm -f "$experiments_path"
    ok "no experiment labels found"
  fi
else
  rm -f "$experiments_path"
  err "failed to fetch experiment labels"
  exit 1
fi

tmpdb="$(mktemp /tmp/starnexus-analysis.XXXXXX.sqlite)"
cleanup() { rm -f "$tmpdb"; }
trap cleanup EXIT

info "Decompressing and verifying backup"
case "$backup_path" in
  *.gz) gzip -dc "$backup_path" > "$tmpdb" ;;
  *) cp "$backup_path" "$tmpdb" ;;
esac

integrity="$(sqlite3 "$tmpdb" 'PRAGMA integrity_check;')"
if [[ "$integrity" != "ok" ]]; then
  err "backup integrity check failed: $integrity"
  exit 1
fi
ok "backup integrity_check=ok"

info "Running analysis export"
analyze_args=(-db "$tmpdb" -schema "$repo_root/server/schema.sql" -out "$run_dir_abs" -hours "$HOURS")
if [[ -s "$experiments_path" ]]; then
  experiments_abs="$(cd "$(dirname "$experiments_path")" && pwd)/$(basename "$experiments_path")"
  analyze_args+=(-experiments "$experiments_abs")
fi
(
  cd server
  go run ./cmd/starnexus-analyze "${analyze_args[@]}"
)

cat > "$run_dir/manifest.json" <<EOF
{
  "generated_at": "$timestamp",
  "host": "$HOST",
  "hours": $HOURS,
  "backup_path": "$backup_path",
  "remote_db": "$REMOTE_DB",
  "remote_experiments": "$REMOTE_EXPERIMENTS",
  "experiments_included": $(if [[ -s "$experiments_path" ]]; then echo true; else echo false; fi)
}
EOF

(
  cd "$OUT_ROOT"
  ln -sfn "runs/$timestamp" latest
)
ok "analysis run written: $run_dir"
ok "latest updated: $OUT_ROOT/latest"

if [[ "$KEEP_RUNS" -gt 0 ]]; then
  removed=0
  while IFS= read -r old_run; do
    [[ -z "$old_run" ]] && continue
    rm -rf "$old_run"
    removed=$((removed + 1))
  done < <(ls -1dt "$runs_dir"/* 2>/dev/null | tail -n +"$((KEEP_RUNS + 1))")
  if [[ "$removed" -gt 0 ]]; then
    ok "run retention removed $removed old run(s); kept newest $KEEP_RUNS"
  fi
fi

printf '\nReport: %s\n' "$run_dir/report.md"
printf 'JSON:   %s\n' "$run_dir/analytics.json"
printf 'CSV:    %s\n' "$run_dir/event_classifications.csv"
