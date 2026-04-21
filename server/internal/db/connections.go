package db

import "time"

type ConnectionSampleInput struct {
	SourceKey     string
	SourceIP      string
	SourceCountry string
	SourceCity    string
	Protocol      string
	LocalPort     int
	IsCloudflare  bool
	RateBPS       float64
	TotalBytes    uint64
}

type ConnectionSummary struct {
	SourceKey        string  `json:"source_key"`
	SourceIP         string  `json:"source_ip"`
	SourceCountry    string  `json:"source_country"`
	SourceCity       string  `json:"source_city"`
	Protocol         string  `json:"protocol"`
	LocalPort        int     `json:"local_port"`
	IsCloudflare     bool    `json:"is_cloudflare"`
	SampleCount      int     `json:"sample_count"`
	PeakRateBPS      float64 `json:"peak_rate_bps"`
	AvgRateBPS       float64 `json:"avg_rate_bps"`
	LatestTotalBytes uint64  `json:"latest_total_bytes"`
	LastSeen         int64   `json:"last_seen"`
	NodeID           *string `json:"node_id,omitempty"`
	NodeName         *string `json:"node_name,omitempty"`
}

func (d *DB) SaveConnectionSamples(nodeID string, samples []ConnectionSampleInput) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	stmt, err := tx.Prepare(`
		INSERT INTO connection_samples (
			node_id, source_key, source_ip, source_country, source_city, protocol, local_port,
			is_cloudflare, rate_bps, total_bytes, sample_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sample := range samples {
		if _, err := stmt.Exec(
			nodeID,
			sample.SourceKey,
			sample.SourceIP,
			sample.SourceCountry,
			sample.SourceCity,
			sample.Protocol,
			sample.LocalPort,
			boolToInt(sample.IsCloudflare),
			sample.RateBPS,
			int64(sample.TotalBytes),
			now,
		); err != nil {
			return err
		}
	}

	if _, err := tx.Exec("DELETE FROM connection_samples WHERE sample_at < ?", now-7*86400); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DB) GetNodeConnectionSummary(nodeID string, since int64, limit int) ([]ConnectionSummary, error) {
	rows, err := d.conn.Query(`
		SELECT
			source_key,
			source_ip,
			COALESCE(source_country, ''),
			COALESCE(source_city, ''),
			COALESCE(protocol, ''),
			COALESCE(local_port, 0),
			MAX(is_cloudflare),
			COUNT(*),
			MAX(rate_bps),
			AVG(rate_bps),
			MAX(total_bytes),
			MAX(sample_at)
		FROM connection_samples
		WHERE node_id = ? AND sample_at >= ?
		GROUP BY source_key, source_ip, source_country, source_city, protocol, local_port
		ORDER BY MAX(rate_bps) DESC, MAX(sample_at) DESC
		LIMIT ?
	`, nodeID, since, normalizeLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanConnectionSummaries(rows)
}

func (d *DB) GetConnectionHighlights(since int64, limit int) ([]ConnectionSummary, error) {
	rows, err := d.conn.Query(`
		SELECT
			cs.source_key,
			cs.source_ip,
			COALESCE(cs.source_country, ''),
			COALESCE(cs.source_city, ''),
			COALESCE(cs.protocol, ''),
			COALESCE(cs.local_port, 0),
			MAX(cs.is_cloudflare),
			COUNT(*),
			MAX(cs.rate_bps),
			AVG(cs.rate_bps),
			MAX(cs.total_bytes),
			MAX(cs.sample_at),
			n.id,
			n.name
		FROM connection_samples cs
		LEFT JOIN nodes n ON n.id = cs.node_id
		WHERE cs.sample_at >= ?
		GROUP BY cs.source_key, cs.source_ip, cs.source_country, cs.source_city, cs.protocol, cs.local_port, n.id, n.name
		ORDER BY MAX(cs.rate_bps) DESC, MAX(cs.sample_at) DESC
		LIMIT ?
	`, since, normalizeLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []ConnectionSummary
	for rows.Next() {
		var summary ConnectionSummary
		var isCloudflare int
		if err := rows.Scan(
			&summary.SourceKey,
			&summary.SourceIP,
			&summary.SourceCountry,
			&summary.SourceCity,
			&summary.Protocol,
			&summary.LocalPort,
			&isCloudflare,
			&summary.SampleCount,
			&summary.PeakRateBPS,
			&summary.AvgRateBPS,
			&summary.LatestTotalBytes,
			&summary.LastSeen,
			&summary.NodeID,
			&summary.NodeName,
		); err != nil {
			return nil, err
		}
		summary.IsCloudflare = isCloudflare > 0
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanConnectionSummaries(rows rowScanner) ([]ConnectionSummary, error) {
	var summaries []ConnectionSummary
	for rows.Next() {
		var summary ConnectionSummary
		var isCloudflare int
		if err := rows.Scan(
			&summary.SourceKey,
			&summary.SourceIP,
			&summary.SourceCountry,
			&summary.SourceCity,
			&summary.Protocol,
			&summary.LocalPort,
			&isCloudflare,
			&summary.SampleCount,
			&summary.PeakRateBPS,
			&summary.AvgRateBPS,
			&summary.LatestTotalBytes,
			&summary.LastSeen,
		); err != nil {
			return nil, err
		}
		summary.IsCloudflare = isCloudflare > 0
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	return limit
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
