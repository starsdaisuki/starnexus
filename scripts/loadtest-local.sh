#!/usr/bin/env bash
# Local scalability benchmark. Spins up an isolated StarNexus server on a
# temp DB, runs starnexus-loadtest at several fleet sizes, and writes
# per-size JSON summaries into analysis-output/loadtest/.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

SIZES=(10 50 100 250 500)
INTERVAL="${INTERVAL:-250ms}"
DURATION="${DURATION:-45s}"
PORT="${PORT:-18900}"
TOKEN="loadtest-local-token"
OUT_DIR="$REPO_ROOT/analysis-output/loadtest"
mkdir -p "$OUT_DIR"

WORK_DIR="$(mktemp -d -t sn-loadtest-XXXX)"
trap 'rm -rf "$WORK_DIR"' EXIT

cp "$REPO_ROOT/server/schema.sql" "$WORK_DIR/schema.sql"
mkdir -p "$WORK_DIR/web"
cat > "$WORK_DIR/config.yaml" <<YAML
port: $PORT
db_path: "$WORK_DIR/starnexus.db"
api_token: "$TOKEN"
web_dir: "$WORK_DIR/web"
node_locations_path: ""
experiment_labels_path: ""
agent_binary_path: ""
geoip_db_path: ""
offline_threshold_seconds: 90
bot_token: ""
bot_chat_ids: []
mistral_api_key: ""
YAML

pushd "$REPO_ROOT/server" > /dev/null
SERVER_LOG="$WORK_DIR/server.log"
(go run . "$WORK_DIR/config.yaml" >"$SERVER_LOG" 2>&1) &
SERVER_PID=$!
popd > /dev/null

cleanup() {
  kill "$SERVER_PID" 2>/dev/null || true
  wait "$SERVER_PID" 2>/dev/null || true
}
trap 'cleanup; rm -rf "$WORK_DIR"' EXIT

# Wait for server to come up.
for attempt in $(seq 1 30); do
  if curl -sf "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl -sf "http://127.0.0.1:$PORT/api/health" >/dev/null 2>&1; then
  echo "Server failed to come up. Log:" >&2
  cat "$SERVER_LOG" >&2
  exit 1
fi

echo "Server up on :$PORT (pid=$SERVER_PID)"

for size in "${SIZES[@]}"; do
  OUT_PATH="$OUT_DIR/loadtest-${size}-agents.json"
  echo
  echo "=== Load test: $size agents ==="
  pushd "$REPO_ROOT/server" > /dev/null
  go run ./cmd/starnexus-loadtest \
    -server "http://127.0.0.1:$PORT" \
    -token "$TOKEN" \
    -agents "$size" \
    -duration "$DURATION" \
    -interval "$INTERVAL" \
    -out "$OUT_PATH"
  popd > /dev/null
done

echo
echo "Summaries written to $OUT_DIR"
