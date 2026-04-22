package db

type Health struct {
	OK              bool   `json:"ok"`
	QuickCheck      string `json:"quick_check"`
	LatestMigration int    `json:"latest_migration"`
	NodeCount       int    `json:"node_count"`
	MetricCount     int    `json:"metric_count"`
	EventCount      int    `json:"event_count"`
	IncidentCount   int    `json:"incident_count"`
}

func (d *DB) HealthCheck() (Health, error) {
	health := Health{}
	if err := d.conn.QueryRow("PRAGMA quick_check(1)").Scan(&health.QuickCheck); err != nil {
		return health, err
	}
	if err := d.conn.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&health.LatestMigration); err != nil {
		return health, err
	}
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&health.NodeCount); err != nil {
		return health, err
	}
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM metrics_raw").Scan(&health.MetricCount); err != nil {
		return health, err
	}
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM events").Scan(&health.EventCount); err != nil {
		return health, err
	}
	if err := d.conn.QueryRow("SELECT COUNT(*) FROM incidents").Scan(&health.IncidentCount); err != nil {
		return health, err
	}
	health.OK = health.QuickCheck == "ok"
	return health, nil
}
