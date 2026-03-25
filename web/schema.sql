-- Node info
CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,           -- e.g. "tokyo-1"
    name TEXT NOT NULL,            -- display name, e.g. "Tokyo DMIT"
    provider TEXT,                 -- e.g. "DMIT", "LisaHost"
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    status TEXT DEFAULT 'unknown', -- online / offline / degraded / unknown
    last_seen INTEGER,             -- last report UNIX timestamp
    created_at INTEGER DEFAULT (unixepoch())
);

-- Node metrics snapshot (latest, for map display)
CREATE TABLE IF NOT EXISTS node_metrics (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id),
    cpu_percent REAL,
    memory_percent REAL,
    disk_percent REAL,
    bandwidth_up REAL,             -- upload KB/s
    bandwidth_down REAL,           -- download KB/s
    load_avg REAL,
    connections INTEGER,
    uptime_seconds INTEGER,
    updated_at INTEGER DEFAULT (unixepoch())
);

-- Inter-node link info
CREATE TABLE IF NOT EXISTS links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_node_id TEXT NOT NULL REFERENCES nodes(id),
    target_node_id TEXT NOT NULL REFERENCES nodes(id),
    latency_ms REAL,
    packet_loss REAL,              -- 0-100
    status TEXT DEFAULT 'unknown', -- good / degraded / bad / unknown
    updated_at INTEGER DEFAULT (unixepoch()),
    UNIQUE(source_node_id, target_node_id)
);

-- Status change history
CREATE TABLE IF NOT EXISTS status_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    old_status TEXT,
    new_status TEXT NOT NULL,
    reason TEXT,
    created_at INTEGER DEFAULT (unixepoch())
);
