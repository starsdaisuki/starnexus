package db

import "strings"

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{
		version: 1,
		sql:     "ALTER TABLE nodes ADD COLUMN ip_address TEXT",
	},
	{
		version: 2,
		sql: `
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
`,
	},
	{
		version: 3,
		sql: `
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
`,
	},
	{
		version: 4,
		sql:     "ALTER TABLE nodes ADD COLUMN location_source TEXT DEFAULT 'unknown'",
	},
}

func (d *DB) runMigrations() error {
	if _, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}

	for _, m := range migrations {
		var exists int
		if err := d.conn.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", m.version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err := d.conn.Exec(m.sql); err != nil {
			if (m.version == 1 || m.version == 4) && strings.Contains(err.Error(), "duplicate column name") {
				// Fresh databases already include ip_address in schema.sql.
			} else {
				return err
			}
		}
		if _, err := d.conn.Exec(
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%s', 'now'))",
			m.version,
		); err != nil {
			return err
		}
	}

	return nil
}
