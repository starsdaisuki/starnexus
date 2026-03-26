-- Nodes
INSERT INTO nodes (id, name, provider, latitude, longitude, status, last_seen) VALUES
('tokyo-1', 'Tokyo Node', 'Provider A', 35.6762, 139.6503, 'online', unixepoch()),
('osaka-1', 'Osaka Node', 'Provider A', 34.6937, 135.5023, 'online', unixepoch()),
('hk-1', 'Hong Kong Node', 'Provider B', 22.3193, 114.1694, 'online', unixepoch()),
('la-1', 'Los Angeles Node', 'Provider A', 34.0522, -118.2437, 'online', unixepoch()),
('sj-1', 'San Jose Node', 'Provider C', 37.3382, -121.8863, 'degraded', unixepoch()),
('sg-1', 'Singapore Node', 'Provider D', 1.3521, 103.8198, 'offline', unixepoch());

-- Metrics
INSERT INTO node_metrics (node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, uptime_seconds) VALUES
('tokyo-1', 12.5, 45.2, 33.0, 150.3, 2048.7, 0.35, 128, 2592000),
('osaka-1', 8.1, 38.7, 28.5, 80.1, 1200.4, 0.22, 76, 1728000),
('hk-1', 23.4, 62.1, 45.0, 220.5, 3500.2, 0.78, 256, 864000),
('la-1', 15.7, 51.3, 40.2, 180.9, 2800.6, 0.45, 192, 3456000),
('sj-1', 78.3, 85.6, 72.1, 50.2, 500.1, 2.35, 512, 432000),
('sg-1', 0, 0, 0, 0, 0, 0, 0, 0);

-- Links
INSERT INTO links (source_node_id, target_node_id, latency_ms, packet_loss, status) VALUES
('tokyo-1', 'osaka-1', 8.5, 0.0, 'good'),
('tokyo-1', 'hk-1', 45.2, 0.1, 'good'),
('tokyo-1', 'la-1', 120.8, 0.5, 'good'),
('tokyo-1', 'sj-1', 135.4, 2.3, 'degraded'),
('hk-1', 'sg-1', 999.0, 100.0, 'bad'),
('la-1', 'sj-1', 12.3, 0.0, 'good');

-- Status history
INSERT INTO status_history (node_id, old_status, new_status, reason) VALUES
('sg-1', 'online', 'offline', 'Network unreachable'),
('sj-1', 'online', 'degraded', 'High CPU and memory usage');
