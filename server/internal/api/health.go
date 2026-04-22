package api

import (
	"net/http"
	"os"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/buildinfo"
	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type componentHealth struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Path    string `json:"path,omitempty"`
	Checked bool   `json:"checked"`
}

type healthResponse struct {
	Status       string            `json:"status"`
	GeneratedAt  int64             `json:"generated_at"`
	UptimeSec    int64             `json:"uptime_seconds"`
	Version      buildinfo.Info    `json:"version"`
	Database     db.Health         `json:"database"`
	NodeStatus   *db.StatusCounts  `json:"node_status,omitempty"`
	Components   []componentHealth `json:"components"`
	ActiveIssues []componentHealth `json:"active_issues,omitempty"`
}

func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildinfo.Current("starnexus-server"))
}

func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	s.RefreshMetrics()
	s.metrics.Handler().ServeHTTP(w, r)
}

func (s *Server) handleGetHealth(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	dbHealth, err := s.db.HealthCheck()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":       "unhealthy",
			"generated_at": now,
			"version":      buildinfo.Current("starnexus-server"),
			"error":        err.Error(),
		})
		return
	}

	nodeStatus, err := s.db.GetStatusCounts()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":       "unhealthy",
			"generated_at": now,
			"version":      buildinfo.Current("starnexus-server"),
			"database":     dbHealth,
			"error":        err.Error(),
		})
		return
	}

	components := []componentHealth{
		pathHealth("web_dir", s.webDir, true),
		pathHealth("agent_binary", s.agentBinaryPath, true),
		pathHealth("geoip_db", s.geoipDBPath, false),
		pathHealth("experiment_labels", s.experimentLabelsPath, false),
	}
	activeIssues := activeHealthIssues(dbHealth, nodeStatus, components)
	status := "ok"
	if !dbHealth.OK {
		status = "unhealthy"
	} else if len(activeIssues) > 0 {
		status = "degraded"
	}

	code := http.StatusOK
	if status == "unhealthy" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, healthResponse{
		Status:       status,
		GeneratedAt:  now,
		UptimeSec:    now - s.startedAt,
		Version:      buildinfo.Current("starnexus-server"),
		Database:     dbHealth,
		NodeStatus:   nodeStatus,
		Components:   components,
		ActiveIssues: activeIssues,
	})
}

func pathHealth(name, path string, required bool) componentHealth {
	item := componentHealth{Name: name, Path: path, Checked: path != ""}
	if path == "" {
		item.OK = !required
		item.Status = "not_configured"
		return item
	}
	info, err := os.Stat(path)
	if err != nil {
		item.OK = !required
		item.Status = "missing"
		item.Detail = err.Error()
		return item
	}
	item.OK = true
	if info.IsDir() {
		item.Status = "directory"
	} else {
		item.Status = "file"
	}
	return item
}

func activeHealthIssues(dbHealth db.Health, status *db.StatusCounts, components []componentHealth) []componentHealth {
	issues := []componentHealth{}
	if !dbHealth.OK {
		issues = append(issues, componentHealth{Name: "database", OK: false, Status: "unhealthy", Detail: dbHealth.QuickCheck})
	}
	if status != nil {
		if status.Offline > 0 {
			issues = append(issues, componentHealth{Name: "nodes_offline", OK: false, Status: "degraded", Detail: "one or more nodes are offline"})
		}
		if status.Degraded > 0 {
			issues = append(issues, componentHealth{Name: "nodes_degraded", OK: false, Status: "degraded", Detail: "one or more nodes are degraded"})
		}
	}
	for _, component := range components {
		if !component.OK && component.Status != "not_configured" {
			issues = append(issues, component)
		}
	}
	return issues
}
