#!/usr/bin/env bash
# Run a matrix of labelled CPU fault-injection experiments for evaluation.
# Each experiment uses scripts/fault-injection.sh (CPU-only, nice + timeout)
# and appends to analysis-output/experiments.jsonl.
#
# Defaults: 3 reps × [30s, 60s, 150s, 300s] on the given node. ~70 minutes wall time.
set -euo pipefail

SSH_HOST=""
NODE_ID=""
SERVER_SSH="${STARNEXUS_SERVER_SSH:-}"
REPS=3
GAP_SECONDS=120
DURATIONS=(30 60 150 300)
INJECTION_TYPE="cpu"
CAP_FRACTION=40

usage() {
  cat <<'USAGE'
Usage:
  scripts/fault-injection-matrix.sh --ssh-host <node-ssh-host> --node-id <node-id> \
    --server-ssh <server-ssh-alias> [options]

Options:
  --ssh-host <alias>       SSH alias of the experimental node. Required.
  --node-id <id>           StarNexus node id. Required.
  --server-ssh <alias>     Primary server SSH alias. Required unless the
                           STARNEXUS_SERVER_SSH env var is set.
  --type cpu|memory        Injection type (default: cpu).
  --reps <N>               Reps per duration (default: 3).
  --gap <seconds>          Gap between experiments (default: 120).
  --durations "30 60 150"  Space-separated duration list (default: "30 60 150 300").
  --cap-fraction <n>       Memory cap as % of MemAvailable (memory type only, default: 40).

The total wall-time is roughly sum(duration + 90 + gap) across all experiments,
since each inner fault-injection script polls for 90s after the experiment.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ssh-host) SSH_HOST="${2:-}"; shift 2 ;;
    --node-id) NODE_ID="${2:-}"; shift 2 ;;
    --server-ssh) SERVER_SSH="${2:-}"; shift 2 ;;
    --type) INJECTION_TYPE="${2:-}"; shift 2 ;;
    --reps) REPS="${2:-}"; shift 2 ;;
    --gap) GAP_SECONDS="${2:-}"; shift 2 ;;
    --durations) IFS=' ' read -ra DURATIONS <<< "${2:-}"; shift 2 ;;
    --cap-fraction) CAP_FRACTION="${2:-}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "$SSH_HOST" || -z "$NODE_ID" ]]; then
  usage >&2
  exit 2
fi

if [[ -z "$SERVER_SSH" ]]; then
  echo "--server-ssh is required (or set STARNEXUS_SERVER_SSH)" >&2
  usage >&2
  exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
case "$INJECTION_TYPE" in
  cpu)    INNER_SCRIPT="$SCRIPT_DIR/fault-injection.sh" ;;
  memory) INNER_SCRIPT="$SCRIPT_DIR/fault-injection-memory.sh" ;;
  *)      echo "unknown --type: $INJECTION_TYPE (want cpu|memory)" >&2; exit 2 ;;
esac

if [[ ! -x "$INNER_SCRIPT" ]]; then
  echo "Missing or non-executable inner script: $INNER_SCRIPT" >&2
  exit 2
fi

TOTAL=$(( REPS * ${#DURATIONS[@]} ))
index=0
echo "Matrix: ${REPS} reps × durations=(${DURATIONS[*]}) on ${NODE_ID} via ${SSH_HOST}, gap=${GAP_SECONDS}s"

for duration in "${DURATIONS[@]}"; do
  for (( rep=1; rep<=REPS; rep++ )); do
    index=$(( index + 1 ))
    echo
    echo "=== [${index}/${TOTAL}] duration=${duration}s rep=${rep} ==="
    extra_args=()
    if [[ "$INJECTION_TYPE" == "memory" ]]; then
      extra_args+=(--cap-fraction "$CAP_FRACTION")
    fi
    "$INNER_SCRIPT" \
      --ssh-host "$SSH_HOST" \
      --node-id "$NODE_ID" \
      --server-ssh "$SERVER_SSH" \
      --duration "$duration" \
      "${extra_args[@]}"
    if (( index < TOTAL )); then
      echo "Cooling down for ${GAP_SECONDS}s before next experiment..."
      sleep "$GAP_SECONDS"
    fi
  done
done

echo
echo "=== Matrix complete: ${TOTAL} experiments submitted ==="
