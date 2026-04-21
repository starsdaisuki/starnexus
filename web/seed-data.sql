-- Nodes
INSERT INTO nodes (id, name, provider, latitude, longitude, location_source, status, last_seen) VALUES
('tokyo-1', 'Tokyo Node', 'Provider A', 35.6762, 139.6503, 'manual', 'online', unixepoch()),
('osaka-1', 'Osaka Node', 'Provider A', 34.6937, 135.5023, 'manual', 'online', unixepoch()),
('hk-1', 'Hong Kong Node', 'Provider B', 22.3193, 114.1694, 'manual', 'online', unixepoch()),
('la-1', 'Los Angeles Node', 'Provider A', 34.0522, -118.2437, 'manual', 'online', unixepoch()),
('sj-1', 'San Jose Node', 'Provider C', 37.3382, -121.8863, 'geoip', 'degraded', unixepoch()),
('sg-1', 'Singapore Node', 'Provider D', 1.3521, 103.8198, 'manual_override', 'offline', unixepoch());

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

-- Scores
INSERT INTO node_scores (node_id, availability, latency_score, stability, composite_score, updated_at) VALUES
('tokyo-1', 99.2, 92.0, 95.0, 95.2, unixepoch()),
('osaka-1', 99.6, 96.0, 96.0, 97.2, unixepoch()),
('hk-1', 98.4, 82.0, 88.0, 90.3, unixepoch()),
('la-1', 97.8, 62.0, 90.0, 84.7, unixepoch()),
('sj-1', 95.2, 58.0, 52.0, 69.3, unixepoch()),
('sg-1', 80.0, 10.0, 30.0, 42.0, unixepoch());

-- Events
INSERT INTO events (node_id, type, severity, title, body, created_at) VALUES
('sg-1', 'status_change', 'critical', 'Node offline', 'No report received within offline threshold.', unixepoch() - 3600),
('sj-1', 'status_change', 'warning', 'Node degraded', 'CPU and memory crossed the resource threshold.', unixepoch() - 900),
('tokyo-1', 'anomaly', 'warning', 'Metric anomaly detected', 'Bandwidth spike observed outside the recent baseline.', unixepoch() - 1800);

-- Metric samples for detail charts
INSERT INTO metrics_raw (node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, created_at) VALUES
('tokyo-1', 11.4, 43.1, 33.0, 118.0, 1720.0, 0.31, 121, unixepoch() - 18000),
('tokyo-1', 12.2, 44.0, 33.0, 126.0, 1940.0, 0.32, 124, unixepoch() - 14400),
('tokyo-1', 14.8, 47.2, 33.0, 141.0, 2220.0, 0.39, 136, unixepoch() - 10800),
('tokyo-1', 13.1, 45.3, 33.0, 132.0, 2105.0, 0.36, 131, unixepoch() - 7200),
('tokyo-1', 15.5, 46.8, 33.0, 148.0, 2408.0, 0.41, 140, unixepoch() - 3600),
('tokyo-1', 12.5, 45.2, 33.0, 150.3, 2048.7, 0.35, 128, unixepoch() - 300),
('sj-1', 66.0, 74.2, 72.1, 42.0, 460.0, 1.92, 440, unixepoch() - 18000),
('sj-1', 69.8, 76.5, 72.1, 46.0, 480.0, 2.05, 458, unixepoch() - 14400),
('sj-1', 75.5, 80.1, 72.1, 49.0, 510.0, 2.14, 489, unixepoch() - 10800),
('sj-1', 80.4, 84.5, 72.1, 53.0, 530.0, 2.44, 521, unixepoch() - 7200),
('sj-1', 78.7, 85.1, 72.1, 51.0, 505.0, 2.39, 514, unixepoch() - 3600),
('sj-1', 78.3, 85.6, 72.1, 50.2, 500.1, 2.35, 512, unixepoch() - 300);

-- Sampled ingress sources
INSERT INTO connection_samples (node_id, source_key, source_ip, source_country, source_city, protocol, local_port, is_cloudflare, rate_bps, total_bytes, sample_at) VALUES
('tokyo-1', '198.41.214.42|VLESS+WS|443', '198.41.214.42', 'Japan', 'Tokyo', 'VLESS+WS', 443, 1, 192304.0, 18203402, unixepoch() - 120),
('tokyo-1', '198.41.214.42|VLESS+WS|443', '198.41.214.42', 'Japan', 'Tokyo', 'VLESS+WS', 443, 1, 214201.0, 18588482, unixepoch() - 30),
('sj-1', '89.187.181.12|Reality|8443', '89.187.181.12', 'United States', 'Los Angeles', 'Reality', 8443, 0, 86312.0, 6244912, unixepoch() - 90),
('hk-1', '45.83.64.7|Trojan|37982', '45.83.64.7', 'Hong Kong', 'Hong Kong', 'Trojan', 37982, 0, 45212.0, 3188411, unixepoch() - 240);
