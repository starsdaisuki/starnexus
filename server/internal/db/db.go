package db

import (
	"database/sql"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func Open(dbPath, schemaPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrency
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, err
	}

	// Run schema
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec(string(schema)); err != nil {
		conn.Close()
		return nil, err
	}

	database := &DB{conn: conn}
	if err := database.runMigrations(); err != nil {
		conn.Close()
		return nil, err
	}

	return database, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

// --- Node types ---

type Node struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Provider       *string      `json:"provider"`
	IPAddress      *string      `json:"ip_address"`
	Latitude       float64      `json:"latitude"`
	Longitude      float64      `json:"longitude"`
	LocationSource *string      `json:"location_source,omitempty"`
	Status         string       `json:"status"`
	LastSeen       *int64       `json:"last_seen"`
	Metrics        *NodeMetrics `json:"metrics,omitempty"`
}

type NodeMetrics struct {
	CPUPercent    *float64 `json:"cpu_percent"`
	MemoryPercent *float64 `json:"memory_percent"`
	DiskPercent   *float64 `json:"disk_percent"`
	BandwidthUp   *float64 `json:"bandwidth_up"`
	BandwidthDown *float64 `json:"bandwidth_down"`
	LoadAvg       *float64 `json:"load_avg"`
	Connections   *int     `json:"connections"`
	UptimeSeconds *int64   `json:"uptime_seconds"`
	UpdatedAt     *int64   `json:"updated_at"`
}

type Link struct {
	ID           int     `json:"id"`
	SourceNodeID string  `json:"source_node_id"`
	TargetNodeID string  `json:"target_node_id"`
	LatencyMs    float64 `json:"latency_ms"`
	PacketLoss   float64 `json:"packet_loss"`
	Status       string  `json:"status"`
	UpdatedAt    int64   `json:"updated_at"`
}

type StatusHistory struct {
	ID        int     `json:"id"`
	NodeID    string  `json:"node_id"`
	OldStatus *string `json:"old_status"`
	NewStatus string  `json:"new_status"`
	Reason    *string `json:"reason"`
	CreatedAt int64   `json:"created_at"`
}

// --- Queries ---

func (d *DB) GetAllNodes() ([]Node, error) {
	rows, err := d.conn.Query(`
		SELECT
			n.id, n.name, n.provider, n.ip_address, n.latitude, n.longitude, n.location_source, n.status, n.last_seen,
			m.cpu_percent, m.memory_percent, m.disk_percent,
			m.bandwidth_up, m.bandwidth_down, m.load_avg, m.connections,
			m.uptime_seconds, m.updated_at
		FROM nodes n
		LEFT JOIN node_metrics m ON n.id = m.node_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var m NodeMetrics
		err := rows.Scan(
			&n.ID, &n.Name, &n.Provider, &n.IPAddress, &n.Latitude, &n.Longitude, &n.LocationSource, &n.Status, &n.LastSeen,
			&m.CPUPercent, &m.MemoryPercent, &m.DiskPercent,
			&m.BandwidthUp, &m.BandwidthDown, &m.LoadAvg, &m.Connections,
			&m.UptimeSeconds, &m.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		n.Metrics = &m
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

func (d *DB) GetNode(id string) (*Node, error) {
	row := d.conn.QueryRow(`
		SELECT
			n.id, n.name, n.provider, n.ip_address, n.latitude, n.longitude, n.location_source, n.status, n.last_seen,
			m.cpu_percent, m.memory_percent, m.disk_percent,
			m.bandwidth_up, m.bandwidth_down, m.load_avg, m.connections,
			m.uptime_seconds, m.updated_at
		FROM nodes n
		LEFT JOIN node_metrics m ON n.id = m.node_id
		WHERE n.id = ?
	`, id)

	var n Node
	var m NodeMetrics
	err := row.Scan(
		&n.ID, &n.Name, &n.Provider, &n.IPAddress, &n.Latitude, &n.Longitude, &n.LocationSource, &n.Status, &n.LastSeen,
		&m.CPUPercent, &m.MemoryPercent, &m.DiskPercent,
		&m.BandwidthUp, &m.BandwidthDown, &m.LoadAvg, &m.Connections,
		&m.UptimeSeconds, &m.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.Metrics = &m
	return &n, nil
}

func (d *DB) GetAllLinks() ([]Link, error) {
	rows, err := d.conn.Query(`
		SELECT id, source_node_id, target_node_id, latency_ms, packet_loss, status, updated_at
		FROM links
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.SourceNodeID, &l.TargetNodeID, &l.LatencyMs, &l.PacketLoss, &l.Status, &l.UpdatedAt); err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

type StatusCounts struct {
	Total    int `json:"total"`
	Online   int `json:"online"`
	Degraded int `json:"degraded"`
	Offline  int `json:"offline"`
	Unknown  int `json:"unknown"`
}

func (d *DB) GetStatusCounts() (*StatusCounts, error) {
	rows, err := d.conn.Query("SELECT status, COUNT(*) FROM nodes GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	s := &StatusCounts{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		s.Total += count
		switch status {
		case "online":
			s.Online = count
		case "degraded":
			s.Degraded = count
		case "offline":
			s.Offline = count
		default:
			s.Unknown += count
		}
	}
	return s, rows.Err()
}

func (d *DB) GetHistory(nodeID string) ([]StatusHistory, error) {
	rows, err := d.conn.Query(`
		SELECT id, node_id, old_status, new_status, reason, created_at
		FROM status_history
		WHERE node_id = ?
		ORDER BY created_at DESC
	`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []StatusHistory
	for rows.Next() {
		var h StatusHistory
		if err := rows.Scan(&h.ID, &h.NodeID, &h.OldStatus, &h.NewStatus, &h.Reason, &h.CreatedAt); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}

// --- Report (upsert node + metrics) ---

type ReportLink struct {
	TargetNodeID string  `json:"target_node_id"`
	LatencyMs    float64 `json:"latency_ms"`
	PacketLoss   float64 `json:"packet_loss"`
}

type ReportRequest struct {
	CollectedAt    int64   `json:"collected_at"`
	NodeID         string  `json:"node_id"`
	Name           string  `json:"name"`
	Provider       string  `json:"provider"`
	PublicIP       string  `json:"public_ip"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	LocationSource string  `json:"location_source"`
	Metrics        struct {
		CPUPercent    float64 `json:"cpu_percent"`
		MemoryPercent float64 `json:"memory_percent"`
		DiskPercent   float64 `json:"disk_percent"`
		BandwidthUp   float64 `json:"bandwidth_up"`
		BandwidthDown float64 `json:"bandwidth_down"`
		LoadAvg       float64 `json:"load_avg"`
		Connections   int     `json:"connections"`
		UptimeSeconds int64   `json:"uptime_seconds"`
	} `json:"metrics"`
	Links []ReportLink `json:"links"`
}

// UpsertReport inserts or updates a node and its metrics. Returns the old status (or "" if new node).
func (d *DB) UpsertReport(r *ReportRequest) (oldStatus string, err error) {
	now := time.Now().Unix()
	metricTime := normalizeCollectedAt(r.CollectedAt, now)

	// Get old status
	row := d.conn.QueryRow("SELECT status FROM nodes WHERE id = ?", r.NodeID)
	_ = row.Scan(&oldStatus) // ignore ErrNoRows

	// Upsert node (with ip_address)
	_, err = d.conn.Exec(`
		INSERT INTO nodes (id, name, provider, ip_address, latitude, longitude, location_source, status, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'online', ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			provider = excluded.provider,
			ip_address = excluded.ip_address,
			latitude = excluded.latitude,
			longitude = excluded.longitude,
			location_source = excluded.location_source,
			status = 'online',
			last_seen = excluded.last_seen
	`, r.NodeID, r.Name, r.Provider, r.PublicIP, r.Latitude, r.Longitude, normalizeLocationSource(r.LocationSource), now)
	if err != nil {
		return
	}

	// Upsert metrics
	_, err = d.conn.Exec(`
		INSERT INTO node_metrics (node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, uptime_seconds, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			cpu_percent = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.cpu_percent ELSE node_metrics.cpu_percent END,
			memory_percent = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.memory_percent ELSE node_metrics.memory_percent END,
			disk_percent = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.disk_percent ELSE node_metrics.disk_percent END,
			bandwidth_up = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.bandwidth_up ELSE node_metrics.bandwidth_up END,
			bandwidth_down = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.bandwidth_down ELSE node_metrics.bandwidth_down END,
			load_avg = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.load_avg ELSE node_metrics.load_avg END,
			connections = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.connections ELSE node_metrics.connections END,
			uptime_seconds = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.uptime_seconds ELSE node_metrics.uptime_seconds END,
			updated_at = CASE WHEN excluded.updated_at >= node_metrics.updated_at THEN excluded.updated_at ELSE node_metrics.updated_at END
	`, r.NodeID, r.Metrics.CPUPercent, r.Metrics.MemoryPercent, r.Metrics.DiskPercent,
		r.Metrics.BandwidthUp, r.Metrics.BandwidthDown, r.Metrics.LoadAvg,
		r.Metrics.Connections, r.Metrics.UptimeSeconds, metricTime)
	if err != nil {
		return
	}

	// Insert raw metrics for time series
	_, _ = d.conn.Exec(`
		INSERT INTO metrics_raw (node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.NodeID, r.Metrics.CPUPercent, r.Metrics.MemoryPercent, r.Metrics.DiskPercent,
		r.Metrics.BandwidthUp, r.Metrics.BandwidthDown, r.Metrics.LoadAvg,
		r.Metrics.Connections, metricTime)

	// Upsert links
	for _, link := range r.Links {
		status := "good"
		if link.PacketLoss >= 100 || link.LatencyMs < 0 {
			status = "bad"
		} else if link.LatencyMs > 150 || link.PacketLoss > 2 {
			status = "degraded"
		}
		_, err = d.conn.Exec(`
			INSERT INTO links (source_node_id, target_node_id, latency_ms, packet_loss, status, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(source_node_id, target_node_id) DO UPDATE SET
				latency_ms = CASE WHEN excluded.updated_at >= links.updated_at THEN excluded.latency_ms ELSE links.latency_ms END,
				packet_loss = CASE WHEN excluded.updated_at >= links.updated_at THEN excluded.packet_loss ELSE links.packet_loss END,
				status = CASE WHEN excluded.updated_at >= links.updated_at THEN excluded.status ELSE links.status END,
				updated_at = CASE WHEN excluded.updated_at >= links.updated_at THEN excluded.updated_at ELSE links.updated_at END
		`, r.NodeID, link.TargetNodeID, link.LatencyMs, link.PacketLoss, status, metricTime)
		if err != nil {
			return
		}
	}

	return
}

func normalizeCollectedAt(collectedAt, now int64) int64 {
	if collectedAt <= 0 {
		return now
	}
	if collectedAt > now+60 {
		return now
	}
	const maxBackfillAge = 90 * 24 * 60 * 60
	if collectedAt < now-maxBackfillAge {
		return now
	}
	return collectedAt
}

func (d *DB) RecordStatusChange(nodeID, oldStatus, newStatus, reason string) error {
	_, err := d.conn.Exec(
		"INSERT INTO status_history (node_id, old_status, new_status, reason, created_at) VALUES (?, ?, ?, ?, ?)",
		nodeID, oldStatus, newStatus, reason, time.Now().Unix(),
	)
	return err
}

// --- Offline detection ---

type StaleNode struct {
	ID        string
	OldStatus string
}

// GetStaleOnlineNodes returns nodes that are "online" or "degraded" but haven't reported within threshold seconds.
func (d *DB) GetStaleOnlineNodes(thresholdSeconds int) ([]StaleNode, error) {
	cutoff := time.Now().Unix() - int64(thresholdSeconds)
	rows, err := d.conn.Query(
		"SELECT id, status FROM nodes WHERE status IN ('online', 'degraded') AND last_seen < ?",
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stale []StaleNode
	for rows.Next() {
		var s StaleNode
		if err := rows.Scan(&s.ID, &s.OldStatus); err != nil {
			return nil, err
		}
		stale = append(stale, s)
	}
	return stale, rows.Err()
}

func (d *DB) SetNodeOffline(nodeID string) error {
	_, err := d.conn.Exec("UPDATE nodes SET status = 'offline' WHERE id = ?", nodeID)
	return err
}

func (d *DB) SetNodeDegraded(nodeID string) error {
	_, err := d.conn.Exec("UPDATE nodes SET status = 'degraded' WHERE id = ?", nodeID)
	return err
}

func (d *DB) SetNodeStatus(nodeID, status string) error {
	_, err := d.conn.Exec("UPDATE nodes SET status = ? WHERE id = ?", status, nodeID)
	return err
}

func (d *DB) DeleteNode(id string) error {
	_, _ = d.conn.Exec("DELETE FROM node_metrics WHERE node_id = ?", id)
	_, _ = d.conn.Exec("DELETE FROM links WHERE source_node_id = ? OR target_node_id = ?", id, id)
	_, _ = d.conn.Exec("DELETE FROM status_history WHERE node_id = ?", id)
	_, _ = d.conn.Exec("DELETE FROM incidents WHERE node_id = ?", id)
	_, err := d.conn.Exec("DELETE FROM nodes WHERE id = ?", id)
	return err
}

type NodeLocationOverride struct {
	NodeID         string
	Latitude       float64
	Longitude      float64
	LocationSource string
}

func (d *DB) CreateNode(id, name, provider string, lat, lng float64, locationSource string) error {
	_, err := d.conn.Exec(
		"INSERT INTO nodes (id, name, provider, latitude, longitude, location_source) VALUES (?, ?, ?, ?, ?, ?)",
		id, name, provider, lat, lng, normalizeLocationSource(locationSource),
	)
	return err
}

func (d *DB) ApplyLocationOverrides(overrides []NodeLocationOverride) error {
	if len(overrides) == 0 {
		return nil
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, item := range overrides {
		if item.NodeID == "" {
			continue
		}
		if _, err := tx.Exec(
			"UPDATE nodes SET latitude = ?, longitude = ?, location_source = ? WHERE id = ?",
			item.Latitude,
			item.Longitude,
			normalizeLocationSource(item.LocationSource),
			item.NodeID,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func normalizeLocationSource(source string) string {
	switch source {
	case "manual", "geoip", "manual_override":
		return source
	default:
		return "unknown"
	}
}

// --- Analytics queries ---

type RawMetric struct {
	CPUPercent    float64
	MemoryPercent float64
	BandwidthDown float64
	CreatedAt     int64
}

// GetRawMetrics returns raw metrics for a node in a time range.
func (d *DB) GetRawMetrics(nodeID string, from, to int64) ([]RawMetric, error) {
	rows, err := d.conn.Query(
		"SELECT cpu_percent, memory_percent, bandwidth_down, created_at FROM metrics_raw WHERE node_id = ? AND created_at >= ? AND created_at < ? ORDER BY created_at",
		nodeID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []RawMetric
	for rows.Next() {
		var m RawMetric
		if err := rows.Scan(&m.CPUPercent, &m.MemoryPercent, &m.BandwidthDown, &m.CreatedAt); err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

// GetRawMetricCount returns number of raw metrics for a node since a timestamp.
func (d *DB) GetRawMetricCount(nodeID string, since int64) (int, error) {
	var count int
	err := d.conn.QueryRow(
		"SELECT COUNT(*) FROM metrics_raw WHERE node_id = ? AND created_at >= ?",
		nodeID, since,
	).Scan(&count)
	return count, err
}

// AggregateHourly aggregates raw metrics into hourly buckets for a time range.
func (d *DB) AggregateHourly(from, to int64) error {
	_, err := d.conn.Exec(`
		INSERT OR REPLACE INTO metrics_hourly (node_id, hour, cpu_avg, cpu_max, cpu_stddev, mem_avg, mem_max, mem_stddev, bw_up_avg, bw_down_avg, load_avg, sample_count)
		SELECT
			node_id,
			(created_at / 3600) * 3600 AS hour,
			AVG(cpu_percent), MAX(cpu_percent),
			CASE WHEN COUNT(*) > 1 THEN SQRT(AVG(cpu_percent * cpu_percent) - AVG(cpu_percent) * AVG(cpu_percent)) ELSE 0 END,
			AVG(memory_percent), MAX(memory_percent),
			CASE WHEN COUNT(*) > 1 THEN SQRT(AVG(memory_percent * memory_percent) - AVG(memory_percent) * AVG(memory_percent)) ELSE 0 END,
			AVG(bandwidth_up), AVG(bandwidth_down),
			AVG(load_avg),
			COUNT(*)
		FROM metrics_raw
		WHERE created_at >= ? AND created_at < ?
		GROUP BY node_id, hour
	`, from, to)
	return err
}

// AggregateDaily aggregates hourly metrics into daily buckets for a time range.
func (d *DB) AggregateDaily(from, to int64) error {
	_, err := d.conn.Exec(`
		INSERT OR REPLACE INTO metrics_daily (node_id, day, cpu_avg, cpu_max, cpu_stddev, mem_avg, mem_max, mem_stddev, bw_up_avg, bw_down_avg, load_avg, online_seconds, sample_count)
		SELECT
			node_id,
			(hour / 86400) * 86400 AS day,
			AVG(cpu_avg), MAX(cpu_max),
			CASE WHEN COUNT(*) > 1 THEN SQRT(AVG(cpu_avg * cpu_avg) - AVG(cpu_avg) * AVG(cpu_avg)) ELSE 0 END,
			AVG(mem_avg), MAX(mem_max),
			CASE WHEN COUNT(*) > 1 THEN SQRT(AVG(mem_avg * mem_avg) - AVG(mem_avg) * AVG(mem_avg)) ELSE 0 END,
			AVG(bw_up_avg), AVG(bw_down_avg),
			AVG(load_avg),
			SUM(sample_count) * 30,
			SUM(sample_count)
		FROM metrics_hourly
		WHERE hour >= ? AND hour < ?
		GROUP BY node_id, day
	`, from, to)
	return err
}

// PurgeRawMetrics deletes raw metrics older than a timestamp.
func (d *DB) PurgeRawMetrics(before int64) (int64, error) {
	result, err := d.conn.Exec("DELETE FROM metrics_raw WHERE created_at < ?", before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// PurgeHourlyMetrics deletes hourly metrics older than a timestamp.
func (d *DB) PurgeHourlyMetrics(before int64) (int64, error) {
	result, err := d.conn.Exec("DELETE FROM metrics_hourly WHERE hour < ?", before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// GetNodeIDs returns all node IDs.
func (d *DB) GetNodeIDs() ([]string, error) {
	rows, err := d.conn.Query("SELECT id FROM nodes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetNodeName returns the display name for a node.
func (d *DB) GetNodeName(nodeID string) string {
	var name string
	d.conn.QueryRow("SELECT name FROM nodes WHERE id = ?", nodeID).Scan(&name)
	return name
}

// GetOnlineSeconds returns seconds a node was online in a time range (based on raw metric count * 30s).
func (d *DB) GetOnlineSeconds(nodeID string, from, to int64) (int64, error) {
	var count int64
	err := d.conn.QueryRow(
		"SELECT COUNT(*) FROM metrics_raw WHERE node_id = ? AND created_at >= ? AND created_at < ?",
		nodeID, from, to,
	).Scan(&count)
	return count * 30, err
}

// GetAvgLinkLatency returns average latency for links involving a node.
func (d *DB) GetAvgLinkLatency(nodeID string) (float64, error) {
	var avg float64
	err := d.conn.QueryRow(
		"SELECT COALESCE(AVG(latency_ms), -1) FROM links WHERE (source_node_id = ? OR target_node_id = ?) AND latency_ms >= 0",
		nodeID, nodeID,
	).Scan(&avg)
	return avg, err
}

// UpsertNodeScore saves or updates a node's composite score.
func (d *DB) UpsertNodeScore(nodeID string, availability, latencyScore, stability, composite float64) error {
	_, err := d.conn.Exec(`
		INSERT INTO node_scores (node_id, availability, latency_score, stability, composite_score, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			availability = excluded.availability,
			latency_score = excluded.latency_score,
			stability = excluded.stability,
			composite_score = excluded.composite_score,
			updated_at = excluded.updated_at
	`, nodeID, availability, latencyScore, stability, composite, time.Now().Unix())
	return err
}

type NodeScore struct {
	NodeID         string  `json:"node_id"`
	Availability   float64 `json:"availability"`
	LatencyScore   float64 `json:"latency_score"`
	Stability      float64 `json:"stability"`
	CompositeScore float64 `json:"composite_score"`
	UpdatedAt      int64   `json:"updated_at"`
}

// GetAllScores returns all node scores.
func (d *DB) GetAllScores() ([]NodeScore, error) {
	rows, err := d.conn.Query("SELECT node_id, availability, latency_score, stability, composite_score, updated_at FROM node_scores ORDER BY composite_score DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scores []NodeScore
	for rows.Next() {
		var s NodeScore
		if err := rows.Scan(&s.NodeID, &s.Availability, &s.LatencyScore, &s.Stability, &s.CompositeScore, &s.UpdatedAt); err != nil {
			return nil, err
		}
		scores = append(scores, s)
	}
	return scores, rows.Err()
}

func (d *DB) GetNodeScore(nodeID string) (*NodeScore, error) {
	row := d.conn.QueryRow(`
		SELECT node_id, availability, latency_score, stability, composite_score, updated_at
		FROM node_scores
		WHERE node_id = ?
	`, nodeID)

	var score NodeScore
	if err := row.Scan(&score.NodeID, &score.Availability, &score.LatencyScore, &score.Stability, &score.CompositeScore, &score.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &score, nil
}

// Conn exposes the underlying connection for analytics queries.
func (d *DB) Conn() *sql.DB {
	return d.conn
}
