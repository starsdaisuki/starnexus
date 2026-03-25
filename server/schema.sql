CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    provider TEXT,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    status TEXT DEFAULT 'unknown',
    last_seen INTEGER,
    created_at INTEGER DEFAULT (strftime('%s', 'now'))
);

CREATE TABLE IF NOT EXISTS node_metrics (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id),
    cpu_percent REAL,
    memory_percent REAL,
    disk_percent REAL,
    bandwidth_up REAL,
    bandwidth_down REAL,
    load_avg REAL,
    connections INTEGER,
    uptime_seconds INTEGER,
    updated_at INTEGER DEFAULT (strftime('%s', 'now'))
);

CREATE TABLE IF NOT EXISTS links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_node_id TEXT NOT NULL REFERENCES nodes(id),
    target_node_id TEXT NOT NULL REFERENCES nodes(id),
    latency_ms REAL,
    packet_loss REAL,
    status TEXT DEFAULT 'unknown',
    updated_at INTEGER DEFAULT (strftime('%s', 'now')),
    UNIQUE(source_node_id, target_node_id)
);

CREATE TABLE IF NOT EXISTS status_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    old_status TEXT,
    new_status TEXT NOT NULL,
    reason TEXT,
    created_at INTEGER DEFAULT (strftime('%s', 'now'))
);
