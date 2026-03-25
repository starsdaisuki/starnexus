# StarNexus - Distributed Node Monitoring System

A distributed VPS node health monitoring system with real-time world map visualization, link topology, and automated alerting.

## Live Demo

**https://starnexus-web.pages.dev**

## Architecture

```
┌─────────────────────────────────────────────────┐
│                    Web Frontend                  │
│  World map visualization / Node details / Charts │
│  Tech: HTML + JS + Leaflet + Chart.js            │
└──────────────────────┬──────────────────────────┘
                       │ HTTP API / WebSocket
┌──────────────────────┴──────────────────────────┐
│               Central Server (Backend)           │
│  Data ingestion / Storage / Alerting / API       │
│  Tech: Go (net/http) + SQLite                    │
└──────────────────────┬──────────────────────────┘
            ┌──────────┼──────────┐
            │          │          │
      ┌─────┴───┐ ┌───┴─────┐ ┌─┴───────┐
      │ Agent A │ │ Agent B │ │ Agent C │  ...
      │ Tokyo   │ │ LA      │ │ HK      │
      └─────────┘ └─────────┘ └─────────┘
            │
      ┌─────┴───────────────────────┐
      │  Telegram Bot                │
      │  Alerts + Commands + Watchdog│
      └─────────────────────────────┘
```

## Modules

| Module | Directory | Language | Status |
|--------|-----------|----------|--------|
| `web` | [`web/`](web/) | HTML/JS + Cloudflare Pages | ✅ Live |
| `agent` | [`agent/`](agent/) | Go | 🚧 Coming soon |
| `server` | [`server/`](server/) | Go | 🚧 Coming soon |
| `bot` | [`bot/`](bot/) | Go | 🚧 Coming soon |

## Tech Stack

| Component | Technology | Notes |
|-----------|-----------|-------|
| Web Frontend | Leaflet + Cloudflare Pages | Dark map, animated node markers, day/night terminator |
| Web API | Hono + Cloudflare Pages Functions + D1 | Serverless, zero-CORS same-origin |
| Agent | Go | Single binary, zero dependencies, passive metrics collection |
| Server | Go (net/http) + SQLite | Data aggregation, anomaly detection, alerting |
| Bot | Go + Telegram Bot API | Alert delivery, interactive commands, reverse heartbeat |

## License

MIT
