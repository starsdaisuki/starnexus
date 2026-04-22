#!/usr/bin/env bash
set -euo pipefail

SSH_HOST=""
NODE_ID=""
SERVER_SSH="dmit"
DURATION=150
OUT_DIR="analysis-output"
LABELS_PATH=""
SERVER_LABELS_PATH="/root/starnexus/analysis-output/experiments.jsonl"
PUSH_SERVER_LABEL=1

usage() {
  cat <<'USAGE'
Usage:
  scripts/fault-injection.sh --ssh-host lisahost --node-id jp-lisahost [options]

Options:
  --ssh-host <alias>      SSH config alias for the experimental node.
  --node-id <id>          StarNexus node id to poll from the server API.
  --server-ssh <alias>    SSH config alias for the StarNexus server. Default: dmit
  --duration <seconds>    CPU stress duration. Default: 150, max: 600
  --out-dir <path>        Local output directory for CSV logs. Default: analysis-output
  --labels <path>         JSONL experiment labels path. Default: <out-dir>/experiments.jsonl
  --server-labels <path>  Remote JSONL label path on the StarNexus server.
                          Default: /root/starnexus/analysis-output/experiments.jsonl
  --no-server-label       Do not append the experiment label to the server.

This script only performs CPU-only fault injection. It does not touch memory,
network shaping, firewall rules, proxy services, or SSH settings.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ssh-host) SSH_HOST="${2:-}"; shift 2 ;;
    --node-id) NODE_ID="${2:-}"; shift 2 ;;
    --server-ssh) SERVER_SSH="${2:-}"; shift 2 ;;
    --duration) DURATION="${2:-}"; shift 2 ;;
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

mkdir -p "$OUT_DIR"
START_TS="$(date -u +%Y%m%dT%H%M%SZ)"
START_EPOCH="$(date -u +%s)"
END_EPOCH=$(( START_EPOCH + DURATION ))
EXPERIMENT_ID="${NODE_ID}-${START_TS}-cpu"
CSV_PATH="$OUT_DIR/fault-injection-${NODE_ID}-${START_TS}.csv"
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
  a[\"cpu\"][\"current\"],
  a[\"cpu\"][\"robust_z\"],
  a[\"memory\"][\"current\"],
))
'"
}

echo "timestamp,status,risk_level,cpu_percent,cpu_robust_z,memory_percent" > "$CSV_PATH"

echo "Pre-check: $NODE_ID via $SERVER_SSH"
fetch_node | tee -a "$CSV_PATH"

echo "Starting CPU-only injection on $SSH_HOST for ${DURATION}s"
PID="$(ssh "$SSH_HOST" "nohup nice -n 10 timeout ${DURATION}s bash -c 'while :; do :; done' >/tmp/starnexus-cpu-injection.log 2>&1 & echo \$!")"
echo "remote_pid=$PID"

# Build the label via jq so every interpolated value is shell-agnostic
# JSON — a node-id containing quotes or backslashes cannot corrupt the
# jsonl file. jq is a hard dependency on the operator's local machine.
if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required to safely build experiment labels" >&2
  exit 2
fi
LABEL_JSON="$(jq -c -n \
  --arg id "$EXPERIMENT_ID" \
  --arg node "$NODE_ID" \
  --arg host "$SSH_HOST" \
  --argjson start "$START_EPOCH" \
  --argjson end "$END_EPOCH" \
  --argjson duration "$DURATION" \
  '{
    experiment_id: $id,
    node_id: $node,
    injection_type: "cpu_stress",
    expected_metric: "cpu_percent",
    expected_direction: "increase",
    started_at: $start,
    ended_at: $end,
    duration_seconds: $duration,
    ssh_host: $host,
    notes: "CPU-only nice+timeout fault injection"
  }')"
echo "$LABEL_JSON" >> "$LABELS_PATH"
echo "experiment_label=$LABELS_PATH"
if (( PUSH_SERVER_LABEL )); then
  # Pass SERVER_LABELS_PATH and the JSON label as positional args on the
  # remote side so neither value can be expanded as shell syntax — the
  # remote shell sees them only as $1/$2 of the bash -s reader.
  ssh "$SERVER_SSH" \
    'bash -s "$1" "$2"' _ "$SERVER_LABELS_PATH" "$LABEL_JSON" <<'REMOTE_SCRIPT'
set -euo pipefail
path="$1"
label="$2"
mkdir -p "$(dirname "$path")"
printf '%s\n' "$label" >> "$path"
REMOTE_SCRIPT
  echo "server_experiment_label=${SERVER_SSH}:${SERVER_LABELS_PATH}"
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
