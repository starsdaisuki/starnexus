# StarNexus Configuration

StarNexus validates YAML configuration at startup. Unknown fields, missing required values, placeholder secrets, invalid URLs, and unsafe interval values fail fast with an actionable error.

Each binary also supports:

```bash
./starnexus-server --check-config ./config.yaml
./starnexus-agent --check-config ./config.yaml
./starnexus-bot --check-config ./bot-config.yaml
```

Use this before restarting services after editing config files.

## Server

Required:

- `api_token`: shared secret for agents and bot.
- `db_path`: SQLite database path.
- `port`: HTTP port, default `8900`.
- `offline_threshold_seconds`: accepted range `30..3600`, default `90`.

Optional:

- `web_dir`: static dashboard directory.
- `node_locations_path`: manual node coordinate overrides.
- `experiment_labels_path`: JSONL labels used by Experiment View.
- `agent_binary_path`: binary served by `/download/agent`.
- `geoip_db_path`: GeoIP DB served by `/download/geoip`.
- `bot_token` and `bot_chat_ids`: server-side analytics alerts. Configure both or neither.
- `mistral_api_key`: optional daily report AI key. Remove the placeholder if unused.

## Agent

Required:

- `server_url`: absolute `http` or `https` URL.
- `api_token`: must match the server token.
- `node_id`: stable database id for the node.

Defaults:

- `node_name`: defaults to `node_id`.
- `provider`: defaults to `Unknown`.
- `report_interval_seconds`: default `30`, accepted range `5..3600`.
- `queue_path`: default `./agent-queue.jsonl`. Set to an empty string to disable disk buffering.
- `queue_max_reports`: default `2880`, about 24 hours at a 30-second report interval.
- `queue_flush_batch_size`: default `120`, accepted range `1..1000`.
- `connection_report_interval_seconds`: default `5`, accepted range `1..300`.
- `geoip_db_path`: default `./GeoLite2-City.mmdb`.

Report queue:

- Failed metric reports are persisted to the agent queue as JSONL.
- When the primary server becomes reachable again, the agent sends the newest live report first and then replays queued reports in FIFO order.
- Replayed reports keep their original `collected_at` timestamp for historical analysis.
- Replayed reports older than the realtime grace window are treated as history and do not create fresh status incidents.
- If the queue exceeds `queue_max_reports`, the oldest reports are dropped first.

Coordinates:

- `latitude` must be `-90..90`.
- `longitude` must be `-180..180`.
- `0,0` asks the agent to auto-detect via GeoIP.

Probe targets:

- `node_id` and `host` are required.
- `port` must be `0..65535`; `0` falls back to SSH port `22`.

## Bot

Required:

- `telegram_token`: Telegram bot token.
- `chat_ids`: allowed Telegram chat IDs.
- `server_url`: absolute `http` or `https` URL.
- `api_token`: must match the server token.

Defaults:

- `poll_interval_seconds`: default `30`, accepted range `5..3600`.
- `heartbeat_interval_seconds`: default `300`, accepted range `30..86400`.

## Common Failure Cases

- `CHANGE_ME`, `BOT_TOKEN_HERE`, and similar placeholders are rejected.
- YAML typos are rejected instead of silently ignored.
- A bot token without chat IDs is rejected.
- A server URL without `http://` or `https://` is rejected.
