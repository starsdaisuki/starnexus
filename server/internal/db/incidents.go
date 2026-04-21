package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	IncidentStatusOpen         = "open"
	IncidentStatusAcknowledged = "acknowledged"
	IncidentStatusSuppressed   = "suppressed"
	IncidentStatusRecovered    = "recovered"
)

type Incident struct {
	ID             int64   `json:"id"`
	NodeID         *string `json:"node_id,omitempty"`
	NodeName       *string `json:"node_name,omitempty"`
	Type           string  `json:"type"`
	Severity       string  `json:"severity"`
	Status         string  `json:"status"`
	Title          string  `json:"title"`
	Body           *string `json:"body,omitempty"`
	Fingerprint    string  `json:"fingerprint"`
	FirstSeen      int64   `json:"first_seen"`
	LastSeen       int64   `json:"last_seen"`
	RecoveredAt    *int64  `json:"recovered_at,omitempty"`
	AcknowledgedAt *int64  `json:"acknowledged_at,omitempty"`
	AcknowledgedBy *string `json:"acknowledged_by,omitempty"`
	SuppressUntil  *int64  `json:"suppress_until,omitempty"`
	EventCount     int     `json:"event_count"`
	Metadata       *string `json:"metadata,omitempty"`
}

type IncidentChange struct {
	Incident     Incident
	Created      bool
	Suppressed   bool
	ShouldNotify bool
}

func (d *DB) UpsertIncident(nodeID, incidentType, severity, title, body, fingerprint, metadata string) (*IncidentChange, error) {
	now := time.Now().Unix()
	if fingerprint == "" {
		fingerprint = BuildIncidentFingerprint(nodeID, incidentType, title)
	}

	existing, err := d.getActiveIncidentByFingerprint(fingerprint)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		status := existing.Status
		if status == IncidentStatusSuppressed && existing.SuppressUntil != nil && *existing.SuppressUntil <= now {
			status = IncidentStatusOpen
		}
		severity = maxSeverity(existing.Severity, severity)

		var bodyValue any
		if body != "" {
			bodyValue = body
		}
		var metadataValue any
		if metadata != "" {
			metadataValue = metadata
		}

		if _, err := d.conn.Exec(`
			UPDATE incidents
			SET severity = ?, status = ?, title = ?, body = ?, metadata = ?, last_seen = ?, event_count = event_count + 1
			WHERE id = ?
		`, severity, status, title, bodyValue, metadataValue, now, existing.ID); err != nil {
			return nil, err
		}

		incident, err := d.GetIncident(existing.ID)
		if err != nil {
			return nil, err
		}
		return &IncidentChange{
			Incident:     *incident,
			Created:      false,
			Suppressed:   incident.IsSuppressed(now),
			ShouldNotify: false,
		}, nil
	}

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

	result, err := d.conn.Exec(`
		INSERT INTO incidents (node_id, type, severity, status, title, body, fingerprint, first_seen, last_seen, metadata)
		VALUES (?, ?, ?, 'open', ?, ?, ?, ?, ?, ?)
	`, nodeValue, incidentType, normalizeSeverity(severity), title, bodyValue, fingerprint, now, now, metadataValue)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	incident, err := d.GetIncident(id)
	if err != nil {
		return nil, err
	}
	return &IncidentChange{
		Incident:     *incident,
		Created:      true,
		Suppressed:   false,
		ShouldNotify: true,
	}, nil
}

func (d *DB) GetIncident(id int64) (*Incident, error) {
	rows, err := d.conn.Query(baseIncidentSelect()+" WHERE i.id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}
	incident, err := scanIncident(rows)
	if err != nil {
		return nil, err
	}
	return &incident, rows.Err()
}

func (d *DB) GetActiveIncidents(limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 20
	}
	return d.queryIncidents(`
		WHERE i.status IN ('open', 'acknowledged', 'suppressed')
		ORDER BY
			CASE i.severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,
			i.last_seen DESC
		LIMIT ?
	`, limit)
}

func (d *DB) GetRecentIncidents(limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 50
	}
	return d.queryIncidents(" ORDER BY i.last_seen DESC LIMIT ?", limit)
}

func (d *DB) GetNodeIncidents(nodeID string, limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 12
	}
	return d.queryIncidents(`
		WHERE i.node_id = ?
		ORDER BY
			CASE i.status WHEN 'open' THEN 0 WHEN 'acknowledged' THEN 1 WHEN 'suppressed' THEN 2 ELSE 3 END,
			i.last_seen DESC
		LIMIT ?
	`, nodeID, limit)
}

func (d *DB) GetNodeActiveIncidents(nodeID string, limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 50
	}
	return d.queryIncidents(`
		WHERE i.node_id = ? AND i.status IN ('open', 'acknowledged', 'suppressed')
		ORDER BY i.last_seen DESC
		LIMIT ?
	`, nodeID, limit)
}

func (d *DB) AcknowledgeIncident(id int64, actor string) (*Incident, error) {
	now := time.Now().Unix()
	if actor == "" {
		actor = "operator"
	}
	if _, err := d.conn.Exec(`
		UPDATE incidents
		SET status = 'acknowledged', acknowledged_at = ?, acknowledged_by = ?
		WHERE id = ? AND status IN ('open', 'suppressed')
	`, now, actor, id); err != nil {
		return nil, err
	}
	return d.GetIncident(id)
}

func (d *DB) SuppressIncident(id int64, until int64, actor string) (*Incident, error) {
	if until <= time.Now().Unix() {
		return nil, fmt.Errorf("suppress_until must be in the future")
	}
	now := time.Now().Unix()
	if actor == "" {
		actor = "operator"
	}
	if _, err := d.conn.Exec(`
		UPDATE incidents
		SET status = 'suppressed', suppress_until = ?, acknowledged_at = COALESCE(acknowledged_at, ?), acknowledged_by = COALESCE(acknowledged_by, ?)
		WHERE id = ? AND status IN ('open', 'acknowledged', 'suppressed')
	`, until, now, actor, id); err != nil {
		return nil, err
	}
	return d.GetIncident(id)
}

func (d *DB) RecoverIncident(id int64) (*Incident, error) {
	now := time.Now().Unix()
	if _, err := d.conn.Exec(`
		UPDATE incidents
		SET status = 'recovered', recovered_at = COALESCE(recovered_at, ?), last_seen = ?
		WHERE id = ? AND status IN ('open', 'acknowledged', 'suppressed')
	`, now, now, id); err != nil {
		return nil, err
	}
	return d.GetIncident(id)
}

func (d *DB) RecoverNodeIncidents(nodeID string, incidentTypes ...string) ([]Incident, error) {
	active, err := d.GetNodeActiveIncidents(nodeID, 100)
	if err != nil {
		return nil, err
	}
	typeSet := make(map[string]bool, len(incidentTypes))
	for _, incidentType := range incidentTypes {
		typeSet[incidentType] = true
	}

	recovered := make([]Incident, 0)
	for _, incident := range active {
		if len(typeSet) > 0 && !typeSet[incident.Type] {
			continue
		}
		updated, err := d.RecoverIncident(incident.ID)
		if err != nil {
			return nil, err
		}
		if updated != nil {
			recovered = append(recovered, *updated)
		}
	}
	return recovered, nil
}

func (d *DB) getActiveIncidentByFingerprint(fingerprint string) (*Incident, error) {
	rows, err := d.conn.Query(baseIncidentSelect()+`
		WHERE i.fingerprint = ? AND i.status IN ('open', 'acknowledged', 'suppressed')
		ORDER BY i.last_seen DESC
		LIMIT 1
	`, fingerprint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}
	incident, err := scanIncident(rows)
	if err != nil {
		return nil, err
	}
	return &incident, rows.Err()
}

func (d *DB) queryIncidents(suffix string, args ...any) ([]Incident, error) {
	rows, err := d.conn.Query(baseIncidentSelect()+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		incident, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, incident)
	}
	return incidents, rows.Err()
}

func baseIncidentSelect() string {
	return `
		SELECT
			i.id, i.node_id, n.name, i.type, i.severity, i.status, i.title, i.body, i.fingerprint,
			i.first_seen, i.last_seen, i.recovered_at, i.acknowledged_at, i.acknowledged_by,
			i.suppress_until, i.event_count, i.metadata
		FROM incidents i
		LEFT JOIN nodes n ON n.id = i.node_id
	`
}

type incidentScanner interface {
	Scan(dest ...any) error
}

func scanIncident(scanner incidentScanner) (Incident, error) {
	var incident Incident
	err := scanner.Scan(
		&incident.ID,
		&incident.NodeID,
		&incident.NodeName,
		&incident.Type,
		&incident.Severity,
		&incident.Status,
		&incident.Title,
		&incident.Body,
		&incident.Fingerprint,
		&incident.FirstSeen,
		&incident.LastSeen,
		&incident.RecoveredAt,
		&incident.AcknowledgedAt,
		&incident.AcknowledgedBy,
		&incident.SuppressUntil,
		&incident.EventCount,
		&incident.Metadata,
	)
	return incident, err
}

func (i Incident) IsSuppressed(now int64) bool {
	return i.Status == IncidentStatusSuppressed && i.SuppressUntil != nil && *i.SuppressUntil > now
}

func BuildIncidentFingerprint(nodeID, incidentType, title string) string {
	parts := []string{strings.TrimSpace(nodeID), strings.TrimSpace(incidentType), strings.TrimSpace(strings.ToLower(title))}
	return strings.Join(parts, ":")
}

func maxSeverity(a, b string) string {
	if severityRank(normalizeSeverity(b)) < severityRank(normalizeSeverity(a)) {
		return normalizeSeverity(b)
	}
	return normalizeSeverity(a)
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func normalizeSeverity(severity string) string {
	switch severity {
	case "critical", "warning", "info":
		return severity
	default:
		return "info"
	}
}

var _ incidentScanner = (*sql.Rows)(nil)
