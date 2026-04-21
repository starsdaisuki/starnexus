package db

type MetricPoint struct {
	Timestamp     int64   `json:"timestamp"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	DiskPercent   float64 `json:"disk_percent"`
	BandwidthUp   float64 `json:"bandwidth_up"`
	BandwidthDown float64 `json:"bandwidth_down"`
	LoadAvg       float64 `json:"load_avg"`
	Connections   int     `json:"connections"`
}

func (d *DB) GetMetricPoints(nodeID string, from, to int64) ([]MetricPoint, error) {
	rows, err := d.conn.Query(`
		SELECT created_at, cpu_percent, memory_percent, disk_percent, bandwidth_up, bandwidth_down, load_avg, connections
		FROM metrics_raw
		WHERE node_id = ? AND created_at >= ? AND created_at < ?
		ORDER BY created_at
	`, nodeID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []MetricPoint
	for rows.Next() {
		var point MetricPoint
		if err := rows.Scan(
			&point.Timestamp,
			&point.CPUPercent,
			&point.MemoryPercent,
			&point.DiskPercent,
			&point.BandwidthUp,
			&point.BandwidthDown,
			&point.LoadAvg,
			&point.Connections,
		); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

func DownsampleMetricPoints(points []MetricPoint, maxPoints int) []MetricPoint {
	if maxPoints <= 0 || len(points) <= maxPoints {
		return points
	}

	step := (len(points) + maxPoints - 1) / maxPoints

	downsampled := make([]MetricPoint, 0, maxPoints)
	for i := 0; i < len(points); i += step {
		downsampled = append(downsampled, points[i])
	}
	last := points[len(points)-1]
	if downsampled[len(downsampled)-1].Timestamp != last.Timestamp {
		downsampled = append(downsampled, last)
	}
	return downsampled
}
