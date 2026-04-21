CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    provider TEXT,
    ip_address TEXT,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    location_source TEXT DEFAULT 'unknown',
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

-- Raw metrics time series (every 30s report)
CREATE TABLE IF NOT EXISTS metrics_raw (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL,
    cpu_percent REAL,
    memory_percent REAL,
    disk_percent REAL,
    bandwidth_up REAL,
    bandwidth_down REAL,
    load_avg REAL,
    connections INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metrics_raw_node_time ON metrics_raw(node_id, created_at);

-- Hourly aggregated metrics (7-30 days)
CREATE TABLE IF NOT EXISTS metrics_hourly (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL,
    hour INTEGER NOT NULL,
    cpu_avg REAL, cpu_max REAL, cpu_stddev REAL,
    mem_avg REAL, mem_max REAL, mem_stddev REAL,
    bw_up_avg REAL, bw_down_avg REAL,
    load_avg REAL,
    sample_count INTEGER,
    UNIQUE(node_id, hour)
);

-- Daily aggregated metrics (30+ days)
CREATE TABLE IF NOT EXISTS metrics_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL,
    day INTEGER NOT NULL,
    cpu_avg REAL, cpu_max REAL, cpu_stddev REAL,
    mem_avg REAL, mem_max REAL, mem_stddev REAL,
    bw_up_avg REAL, bw_down_avg REAL,
    load_avg REAL,
    online_seconds INTEGER,
    sample_count INTEGER,
    UNIQUE(node_id, day)
);

-- Node scores (recalculated daily)
CREATE TABLE IF NOT EXISTS node_scores (
    node_id TEXT PRIMARY KEY,
    availability REAL,
    latency_score REAL,
    stability REAL,
    composite_score REAL,
    updated_at INTEGER
);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT REFERENCES nodes(id),
    type TEXT NOT NULL,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT,
    metadata TEXT,
    created_at INTEGER DEFAULT (strftime('%s', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_node_time ON events(node_id, created_at DESC);

CREATE TABLE IF NOT EXISTS connection_samples (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    source_key TEXT NOT NULL,
    source_ip TEXT NOT NULL,
    source_country TEXT,
    source_city TEXT,
    protocol TEXT,
    local_port INTEGER,
    is_cloudflare INTEGER DEFAULT 0,
    rate_bps REAL,
    total_bytes INTEGER,
    sample_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_connection_samples_node_time ON connection_samples(node_id, sample_at DESC);
CREATE INDEX IF NOT EXISTS idx_connection_samples_time ON connection_samples(sample_at DESC);
