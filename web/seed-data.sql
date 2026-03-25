-- Nodes
INSERT INTO nodes (id, name, provider, latitude, longitude, status, last_seen) VALUES
('tokyo-dmit', 'Tokyo DMIT', 'DMIT', 35.6762, 139.6503, 'online', unixepoch()),
('osaka-dmit', 'Osaka DMIT', 'DMIT', 34.6937, 135.5023, 'online', unixepoch()),
('hk-dmit', 'Hong Kong DMIT', 'DMIT', 22.3193, 114.1694, 'online', unixepoch()),
('la-dmit', 'Los Angeles DMIT', 'DMIT', 34.0522, -118.2437, 'online', unixepoch()),
('sj-lisahost', 'San Jose LisaHost', 'LisaHost', 37.3382, -121.8863, 'degraded', unixepoch()),
('sg-node', 'Singapore', 'Other', 1.3521, 103.8198, 'offline', unixepoch());

-- Metrics
INSERT INTO node_metrics (node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, uptime_seconds) VALUES
('tokyo-dmit', 12.5, 45.2, 33.0, 150.3, 2048.7, 0.35, 128, 2592000),
('osaka-dmit', 8.1, 38.7, 28.5, 80.1, 1200.4, 0.22, 76, 1728000),
('hk-dmit', 23.4, 62.1, 45.0, 220.5, 3500.2, 0.78, 256, 864000),
('la-dmit', 15.7, 51.3, 40.2, 180.9, 2800.6, 0.45, 192, 3456000),
('sj-lisahost', 78.3, 85.6, 72.1, 50.2, 500.1, 2.35, 512, 432000),
('sg-node', 0, 0, 0, 0, 0, 0, 0, 0);

-- Links
INSERT INTO links (source_node_id, target_node_id, latency_ms, packet_loss, status) VALUES
('tokyo-dmit', 'osaka-dmit', 8.5, 0.0, 'good'),
('tokyo-dmit', 'hk-dmit', 45.2, 0.1, 'good'),
('tokyo-dmit', 'la-dmit', 120.8, 0.5, 'good'),
('tokyo-dmit', 'sj-lisahost', 135.4, 2.3, 'degraded'),
('hk-dmit', 'sg-node', 999.0, 100.0, 'bad'),
('la-dmit', 'sj-lisahost', 12.3, 0.0, 'good');

-- Status history
INSERT INTO status_history (node_id, old_status, new_status, reason) VALUES
('sg-node', 'online', 'offline', 'Suspected GFW block (reachable abroad, unreachable domestically)'),
('sj-lisahost', 'online', 'degraded', 'High CPU and memory usage');
