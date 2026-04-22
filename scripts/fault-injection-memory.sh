#!/usr/bin/env bash
set -euo pipefail

SSH_HOST=""
NODE_ID=""
SERVER_SSH="dmit"
DURATION=150
TARGET_MB=0
CAP_FRACTION=40
OUT_DIR="analysis-output"
LABELS_PATH=""
SERVER_LABELS_PATH="/root/starnexus/analysis-output/experiments.jsonl"
PUSH_SERVER_LABEL=1

usage() {
  cat <<'USAGE'
Usage:
  scripts/fault-injection-memory.sh --ssh-host lisahost --node-id jp-lisahost [options]

Options:
  --ssh-host <alias>      SSH config alias for the experimental node.
  --node-id <id>          StarNexus node id to poll from the server API.
  --server-ssh <alias>    SSH config alias for the StarNexus server. Default: dmit
  --duration <seconds>    Memory hold duration. Default: 150, max: 600
  --target-mb <mb>        Explicit target RAM to allocate, in megabytes.
                          Overrides --cap-fraction. Default: 0 (use fraction).
  --cap-fraction <n>      Cap memory target at N percent of available RAM.
                          Default: 40
  --out-dir <path>        Local output directory for CSV logs. Default: analysis-output
  --labels <path>         JSONL experiment labels path. Default: <out-dir>/experiments.jsonl
  --server-labels <path>  Remote JSONL label path on the StarNexus server.
                          Default: /root/starnexus/analysis-output/experiments.jsonl
  --no-server-label       Do not append the experiment label to the server.

Memory-only fault injection. Spawns a Python allocator that touches pages to
keep them resident and exits on timeout. The target is a fraction of
*available* RAM (not total) so the OOM-killer is unlikely to trigger and the
starnexus agent's working set stays intact.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ssh-host) SSH_HOST="${2:-}"; shift 2 ;;
    --node-id) NODE_ID="${2:-}"; shift 2 ;;
    --server-ssh) SERVER_SSH="${2:-}"; shift 2 ;;
    --duration) DURATION="${2:-}"; shift 2 ;;
    --target-mb) TARGET_MB="${2:-}"; shift 2 ;;
    --cap-fraction) CAP_FRACTION="${2:-}"; shift 2 ;;
    --out-dir) OUT_DIR="${2:-}"; shift 2 ;;
    --labels) LABELS_PATH="${2:-}"; shift 2 ;;
    --server-labels) SERVER_LABELS_PATH="${2:-}"; shift 2 ;;
    --no-server-label) PUSH_SERVER_LABEL=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "$SSH_HOST" || -z "$NODE_ID" ]]; then
  usage >&2
  exit 2
fi

if ! [[ "$DURATION" =~ ^[0-9]+$ ]]; then
  echo "--duration must be an integer number of seconds" >&2
  exit 2
fi
if (( DURATION < 30 || DURATION > 600 )); then
  echo "--duration must be between 30 and 600 seconds" >&2
  exit 2
fi
if ! [[ "$CAP_FRACTION" =~ ^[0-9]+$ ]] || (( CAP_FRACTION < 10 || CAP_FRACTION > 70 )); then
  echo "--cap-fraction must be an integer between 10 and 70" >&2
  exit 2
fi
if ! [[ "$TARGET_MB" =~ ^[0-9]+$ ]]; then
  echo "--target-mb must be a non-negative integer" >&2
  exit 2
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required to safely build experiment labels" >&2
  exit 2
fi

mkdir -p "$OUT_DIR"
START_TS="$(date -u +%Y%m%dT%H%M%SZ)"
START_EPOCH="$(date -u +%s)"
END_EPOCH=$(( START_EPOCH + DURATION ))
EXPERIMENT_ID="${NODE_ID}-${START_TS}-mem"
CSV_PATH="$OUT_DIR/fault-injection-memory-${NODE_ID}-${START_TS}.csv"
if [[ -z "$LABELS_PATH" ]]; then
  LABELS_PATH="$OUT_DIR/experiments.jsonl"
fi

fetch_node() {
  ssh "$SERVER_SSH" "curl -fsS 'http://127.0.0.1:8900/api/nodes/${NODE_ID}/details?hours=1' | python3 -c '
import json,sys,time
d=json.load(sys.stdin)
a=d[\"analytics\"]
n=d[\"node\"]
print(\"{},{},{},{:.2f},{:.2f},{:.2f}\".format(
  int(time.time()),
  n[\"status\"],
  a[\"risk_level\"],
  a[\"memory\"][\"current\"],
  a[\"memory\"][\"robust_z\"],
  a[\"cpu\"][\"current\"],
))
'"
}

echo "Pre-check: $NODE_ID via $SERVER_SSH"
echo "timestamp,status,risk_level,memory_percent,memory_robust_z,cpu_percent" > "$CSV_PATH"
fetch_node | tee -a "$CSV_PATH"

# Ask the target host for available RAM; compute target.
# /proc/meminfo is present on every systemd Linux we run.
AVAIL_KB="$(ssh "$SSH_HOST" "awk '/^MemAvailable:/ {print \$2}' /proc/meminfo")"
AVAIL_MB=$(( AVAIL_KB / 1024 ))
if (( AVAIL_MB < 256 )); then
  echo "target host has only ${AVAIL_MB} MB MemAvailable — aborting" >&2
  exit 3
fi

if (( TARGET_MB == 0 )); then
  TARGET_MB=$(( AVAIL_MB * CAP_FRACTION / 100 ))
fi
# Hard-cap so we never request more than 4 GB regardless of --target-mb.
if (( TARGET_MB > 4096 )); then
  TARGET_MB=4096
fi
if (( TARGET_MB > AVAIL_MB * 70 / 100 )); then
  echo "target ${TARGET_MB} MB exceeds 70% of available (${AVAIL_MB} MB) — refusing" >&2
  exit 3
fi
echo "remote MemAvailable=${AVAIL_MB}MB; target=${TARGET_MB}MB for ${DURATION}s"

echo "Starting memory injection on $SSH_HOST"
# Allocate a single bytearray, touch one byte per page (4 KiB) so the kernel
# actually commits the pages instead of leaving them in the zero-page
# optimisation. Exit via timeout so a network glitch on the controller can't
# leave the remote holding memory forever.
REMOTE_SCRIPT=$(cat <<'PY'
import os
import sys
import time

target_mb = int(sys.argv[1])
duration = int(sys.argv[2])
size = target_mb * 1024 * 1024
buf = bytearray(size)
# Touch every 4 KiB page so the kernel faults in RSS, not just VSZ.
for i in range(0, size, 4096):
    buf[i] = 1
sys.stdout.write(f"allocated={target_mb}MB pid={os.getpid()}\n")
sys.stdout.flush()
time.sleep(duration)
PY
)
REMOTE_CMD="nohup timeout $((DURATION + 5))s python3 -c \"\$SCRIPT\" $TARGET_MB $DURATION \
  >/tmp/starnexus-mem-injection.log 2>&1 & echo \$!"
PID="$(ssh "$SSH_HOST" "SCRIPT=\$(cat <<'PY'
$REMOTE_SCRIPT
PY
); $REMOTE_CMD")"
echo "remote_pid=$PID"

LABEL_JSON="$(jq -c -n \
  --arg id "$EXPERIMENT_ID" \
  --arg node "$NODE_ID" \
  --arg host "$SSH_HOST" \
  --argjson start "$START_EPOCH" \
  --argjson end "$END_EPOCH" \
  --argjson duration "$DURATION" \
  --argjson target "$TARGET_MB" \
  '{
    experiment_id: $id,
    node_id: $node,
    injection_type: "memory_stress",
    expected_metric: "memory_percent",
    expected_direction: "increase",
    started_at: $start,
    ended_at: $end,
    duration_seconds: $duration,
    target_mb: $target,
    ssh_host: $host,
    notes: "Python bytearray page-touch memory injection capped at a fraction of MemAvailable"
  }')"
echo "$LABEL_JSON" >> "$LABELS_PATH"
echo "experiment_label=$LABELS_PATH"
if (( PUSH_SERVER_LABEL )); then
  # Pipe the JSON via stdin so nothing in its contents can be reparsed by
  # the remote shell — previous argv-based variants silently failed
  # because OpenSSH concatenates remote-command args with plain spaces
  # without per-arg escaping.
  remote_path_q="$(printf '%q' "$SERVER_LABELS_PATH")"
  if ! printf '%s\n' "$LABEL_JSON" \
    | ssh "$SERVER_SSH" "mkdir -p $(printf '%q' "$(dirname "$SERVER_LABELS_PATH")") && cat >> $remote_path_q"; then
    echo "warn: failed to push server label to ${SERVER_SSH}:${SERVER_LABELS_PATH}" >&2
  else
    echo "server_experiment_label=${SERVER_SSH}:${SERVER_LABELS_PATH}"
  fi
fi

END_AT=$(( END_EPOCH + 90 ))
while (( $(date +%s) <= END_AT )); do
  sleep 30
  fetch_node | tee -a "$CSV_PATH"
done

echo "Remote process state:"
ssh "$SSH_HOST" "ps -p ${PID} -o pid,comm,etime,%cpu,%mem || true"
echo "CSV log: $CSV_PATH"
echo "Experiment labels: $LABELS_PATH"
if (( PUSH_SERVER_LABEL )); then
  echo "Server experiment labels: ${SERVER_SSH}:${SERVER_LABELS_PATH}"
fi
