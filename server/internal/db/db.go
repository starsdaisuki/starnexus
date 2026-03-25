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

	return &DB{conn: conn}, nil
}

func (d *DB) Close() error {
	return d.conn.Close()
}

// --- Node types ---

type Node struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Provider  *string      `json:"provider"`
	Latitude  float64      `json:"latitude"`
	Longitude float64      `json:"longitude"`
	Status    string       `json:"status"`
	LastSeen  *int64       `json:"last_seen"`
	Metrics   *NodeMetrics `json:"metrics,omitempty"`
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
			n.id, n.name, n.provider, n.latitude, n.longitude, n.status, n.last_seen,
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
			&n.ID, &n.Name, &n.Provider, &n.Latitude, &n.Longitude, &n.Status, &n.LastSeen,
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
			n.id, n.name, n.provider, n.latitude, n.longitude, n.status, n.last_seen,
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
		&n.ID, &n.Name, &n.Provider, &n.Latitude, &n.Longitude, &n.Status, &n.LastSeen,
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

type ReportRequest struct {
	NodeID    string  `json:"node_id"`
	Name      string  `json:"name"`
	Provider  string  `json:"provider"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Metrics   struct {
		CPUPercent    float64 `json:"cpu_percent"`
		MemoryPercent float64 `json:"memory_percent"`
		DiskPercent   float64 `json:"disk_percent"`
		BandwidthUp   float64 `json:"bandwidth_up"`
		BandwidthDown float64 `json:"bandwidth_down"`
		LoadAvg       float64 `json:"load_avg"`
		Connections   int     `json:"connections"`
		UptimeSeconds int64   `json:"uptime_seconds"`
	} `json:"metrics"`
}

// UpsertReport inserts or updates a node and its metrics. Returns the old status (or "" if new node).
func (d *DB) UpsertReport(r *ReportRequest) (oldStatus string, err error) {
	now := time.Now().Unix()

	// Get old status
	row := d.conn.QueryRow("SELECT status FROM nodes WHERE id = ?", r.NodeID)
	_ = row.Scan(&oldStatus) // ignore ErrNoRows

	// Upsert node
	_, err = d.conn.Exec(`
		INSERT INTO nodes (id, name, provider, latitude, longitude, status, last_seen)
		VALUES (?, ?, ?, ?, ?, 'online', ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			provider = excluded.provider,
			latitude = excluded.latitude,
			longitude = excluded.longitude,
			status = 'online',
			last_seen = excluded.last_seen
	`, r.NodeID, r.Name, r.Provider, r.Latitude, r.Longitude, now)
	if err != nil {
		return
	}

	// Upsert metrics
	_, err = d.conn.Exec(`
		INSERT INTO node_metrics (node_id, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections, uptime_seconds, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			cpu_percent = excluded.cpu_percent,
			memory_percent = excluded.memory_percent,
			disk_percent = excluded.disk_percent,
			bandwidth_up = excluded.bandwidth_up,
			bandwidth_down = excluded.bandwidth_down,
			load_avg = excluded.load_avg,
			connections = excluded.connections,
			uptime_seconds = excluded.uptime_seconds,
			updated_at = excluded.updated_at
	`, r.NodeID, r.Metrics.CPUPercent, r.Metrics.MemoryPercent, r.Metrics.DiskPercent,
		r.Metrics.BandwidthUp, r.Metrics.BandwidthDown, r.Metrics.LoadAvg,
		r.Metrics.Connections, r.Metrics.UptimeSeconds, now)

	return
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

func (d *DB) CreateNode(id, name, provider string, lat, lng float64) error {
	_, err := d.conn.Exec(
		"INSERT INTO nodes (id, name, provider, latitude, longitude) VALUES (?, ?, ?, ?, ?)",
		id, name, provider, lat, lng,
	)
	return err
}
