package db

type Event struct {
	ID        int64   `json:"id"`
	NodeID    *string `json:"node_id,omitempty"`
	NodeName  *string `json:"node_name,omitempty"`
	Type      string  `json:"type"`
	Severity  string  `json:"severity"`
	Title     string  `json:"title"`
	Body      *string `json:"body,omitempty"`
	Metadata  *string `json:"metadata,omitempty"`
	CreatedAt int64   `json:"created_at"`
}

func (d *DB) RecordEvent(nodeID, eventType, severity, title, body, metadata string) error {
	var nodeValue any
	if nodeID != "" {
		nodeValue = nodeID
	}

	var bodyValue any
	if body != "" {
		bodyValue = body
	}

	var metadataValue any
	if metadata != "" {
		metadataValue = metadata
	}

	_, err := d.conn.Exec(`
		INSERT INTO events (node_id, type, severity, title, body, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, strftime('%s', 'now'))
	`, nodeValue, eventType, severity, title, bodyValue, metadataValue)
	return err
}

func (d *DB) GetRecentEvents(limit int) ([]Event, error) {
	return d.getEvents("", limit)
}

func (d *DB) GetEventsSince(since int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 1000
	}

	rows, err := d.conn.Query(`
		SELECT e.id, e.node_id, n.name, e.type, e.severity, e.title, e.body, e.metadata, e.created_at
		FROM events e
		LEFT JOIN nodes n ON n.id = e.node_id
		WHERE e.created_at >= ?
		ORDER BY e.created_at DESC
		LIMIT ?
	`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.NodeID, &e.NodeName, &e.Type, &e.Severity, &e.Title, &e.Body, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (d *DB) GetNodeEvents(nodeID string, limit int) ([]Event, error) {
	return d.getEvents(nodeID, limit)
}

func (d *DB) HasRecentEvent(nodeID, eventType, title string, withinSeconds int64) (bool, error) {
	if withinSeconds <= 0 {
		withinSeconds = 3600
	}

	row := d.conn.QueryRow(`
		SELECT COUNT(*)
		FROM events
		WHERE node_id = ? AND type = ? AND title = ? AND created_at >= strftime('%s', 'now') - ?
	`, nodeID, eventType, title, withinSeconds)

	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *DB) getEvents(nodeID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
		SELECT e.id, e.node_id, n.name, e.type, e.severity, e.title, e.body, e.metadata, e.created_at
		FROM events e
		LEFT JOIN nodes n ON n.id = e.node_id
	`
	args := []any{}
	if nodeID != "" {
		query += " WHERE e.node_id = ?"
		args = append(args, nodeID)
	}
	query += " ORDER BY e.created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.NodeID, &e.NodeName, &e.Type, &e.Severity, &e.Title, &e.Body, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
