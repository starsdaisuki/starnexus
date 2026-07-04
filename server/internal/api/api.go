package api

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
	"github.com/starsdaisuki/starnexus/server/internal/locations"
	"github.com/starsdaisuki/starnexus/server/internal/metrics"
	"github.com/starsdaisuki/starnexus/server/internal/webassets"
)

// installScript is served at /install.sh. Kept in its own file so it
// stays shell-checkable; this embedded copy is the single source of
// truth for the one-liner agent install.
//
//go:embed install-agent.sh
var installScript string

// ReportGenerator generates on-demand daily reports.
type ReportGenerator interface {
	GenerateReport() string
}

type Server struct {
	db                   *db.DB
	token                string
	webDir               string
	agentBinaryPath      string
	geoipDBPath          string
	experimentLabelsPath string
	reportGen            ReportGenerator
	connStore            *ConnStore
	nodeLocations        *locations.Store
	mux                  *http.ServeMux
	metrics              *metrics.Registry
	instrumented         http.Handler
	startedAt            int64
	metricsMu            sync.Mutex
	metricsRefreshedAt   time.Time
}

// metricsRefreshTTL bounds how often RefreshMetrics hits the DB. A
// Prometheus scraper polling every 15 s would otherwise trigger full
// DB queries on every scrape; 20 s gives the scraper fresh-enough
// numbers without redundant work.
const metricsRefreshTTL = 20 * time.Second

func New(database *db.DB, token, webDir, agentBinaryPath, geoipDBPath, experimentLabelsPath string, nodeLocations *locations.Store) *Server {
	registry := metrics.New()
	s := &Server{
		db:                   database,
		token:                token,
		webDir:               webDir,
		agentBinaryPath:      agentBinaryPath,
		geoipDBPath:          geoipDBPath,
		experimentLabelsPath: experimentLabelsPath,
		connStore:            NewConnStore(),
		nodeLocations:        nodeLocations,
		mux:                  http.NewServeMux(),
		metrics:              registry,
		startedAt:            time.Now().Unix(),
	}
	s.registerMetrics()
	s.routes()
	s.instrumented = registry.HTTPMiddleware(s.mux)
	return s
}

// Metrics returns the registry so external callers (analytics scheduler,
// anomaly detector) can record their own metrics. Unexported types are
// kept within the metrics package; this keeps the API surface narrow.
func (s *Server) Metrics() *metrics.Registry {
	return s.metrics
}

// SetReportGenerator sets the report generator (called after analytics scheduler is created).
func (s *Server) SetReportGenerator(rg ReportGenerator) {
	s.reportGen = rg
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.instrumented.ServeHTTP(w, r)
}

func (s *Server) registerMetrics() {
	s.metrics.RegisterGauge("starnexus_uptime_seconds", "Seconds since the server process started.")
	s.metrics.RegisterGauge("starnexus_nodes_total", "Total registered nodes, labelled by status.")
	s.metrics.RegisterGauge("starnexus_active_incidents", "Active incidents grouped by lifecycle state.")
	s.metrics.RegisterGauge("starnexus_database_rows", "Row counts for key tables.")
	s.metrics.RegisterCounter("starnexus_anomaly_detection_runs_total", "Anomaly detection scheduler passes.")
	s.metrics.RegisterSummary("starnexus_anomaly_detection_seconds", "Anomaly detection runtime in seconds.")
}

// RefreshMetrics updates gauges that derive from current DB state. The
// analytics scheduler calls this periodically; handlers also call it
// opportunistically (/metrics scrape), but a TTL guards against
// hitting the DB on every scrape at sub-second intervals.
func (s *Server) RefreshMetrics() {
	s.metricsMu.Lock()
	if time.Since(s.metricsRefreshedAt) < metricsRefreshTTL {
		// Still serve an always-fresh uptime even when the rest of the
		// snapshot is cached.
		s.metrics.SetGauge("starnexus_uptime_seconds", nil, float64(time.Now().Unix()-s.startedAt))
		s.metricsMu.Unlock()
		return
	}
	s.metricsRefreshedAt = time.Now()
	s.metricsMu.Unlock()

	s.metrics.SetGauge("starnexus_uptime_seconds", nil, float64(time.Now().Unix()-s.startedAt))
	if counts, err := s.db.GetStatusCounts(); err == nil && counts != nil {
		s.metrics.SetGauge("starnexus_nodes_total", map[string]string{"status": "online"}, float64(counts.Online))
		s.metrics.SetGauge("starnexus_nodes_total", map[string]string{"status": "degraded"}, float64(counts.Degraded))
		s.metrics.SetGauge("starnexus_nodes_total", map[string]string{"status": "offline"}, float64(counts.Offline))
		s.metrics.SetGauge("starnexus_nodes_total", map[string]string{"status": "unknown"}, float64(counts.Unknown))
	}
	if active, err := s.db.GetActiveIncidents(500); err == nil {
		byState := map[string]int{"open": 0, "acknowledged": 0, "suppressed": 0}
		for _, incident := range active {
			byState[incident.Status]++
		}
		for state, count := range byState {
			s.metrics.SetGauge("starnexus_active_incidents", map[string]string{"state": state}, float64(count))
		}
	}
	if nodes, err := s.db.GetAllNodes(); err == nil {
		s.metrics.SetGauge("starnexus_database_rows", map[string]string{"table": "nodes"}, float64(len(nodes)))
	}
}

func (s *Server) routes() {
	// Public API
	s.mux.HandleFunc("GET /api/dashboard", s.handleGetDashboard)
	s.mux.HandleFunc("GET /api/health", s.handleGetHealth)
	s.mux.HandleFunc("GET /api/version", s.handleGetVersion)
	s.mux.HandleFunc("GET /api/nodes", s.handleGetNodes)
	s.mux.HandleFunc("GET /api/nodes/{id}", s.handleGetNode)
	s.mux.HandleFunc("GET /api/nodes/{id}/details", s.handleGetNodeDetails)
	s.mux.HandleFunc("GET /api/links", s.handleGetLinks)
	s.mux.HandleFunc("GET /api/status", s.handleGetStatus)
	s.mux.HandleFunc("GET /api/history/{id}", s.handleGetHistory)
	s.mux.HandleFunc("GET /api/scores", s.handleGetScores)
	s.mux.HandleFunc("GET /api/events", s.handleGetEvents)
	s.mux.HandleFunc("GET /api/incidents", s.handleGetIncidents)

	// Agent API (auth required)
	s.mux.HandleFunc("POST /api/report", s.requireAuth(s.handleReport))
	s.mux.HandleFunc("POST /api/nodes", s.requireAuth(s.handleCreateNode))
	s.mux.HandleFunc("DELETE /api/nodes/{id}", s.requireAuth(s.handleDeleteNode))
	s.mux.HandleFunc("GET /api/daily-report", s.requireAuth(s.handleDailyReport))
	s.mux.HandleFunc("POST /api/incidents/{id}/ack", s.requireAuth(s.handleAckIncident))
	s.mux.HandleFunc("POST /api/incidents/{id}/suppress", s.requireAuth(s.handleSuppressIncident))
	s.mux.HandleFunc("POST /api/connections", s.requireAuth(s.handlePostConnections))
	s.mux.HandleFunc("GET /api/connections", s.handleGetConnections)

	// Metrics (Prometheus text format, no auth — only exposed on the
	// private bind address so the SSH tunnel is the only access path).
	s.mux.HandleFunc("GET /metrics", s.handleGetMetrics)

	// Downloads (no auth)
	s.mux.HandleFunc("GET /download/agent", s.handleDownloadAgent)
	s.mux.HandleFunc("GET /download/geoip", s.handleDownloadGeoIP)
	s.mux.HandleFunc("GET /download/install.sh", s.handleInstallScript)
	s.mux.HandleFunc("GET /install.sh", s.handleInstallScript)

	// Static files: an on-disk web_dir (if configured and present)
	// overrides the frontend embedded in the binary, which keeps
	// `web_dir` useful for frontend development without making it a
	// deployment requirement.
	if s.webDir != "" {
		s.mux.Handle("GET /", http.FileServer(http.Dir(s.webDir)))
	} else {
		s.mux.Handle("GET /", http.FileServer(http.FS(webassets.FS())))
	}
}

// --- Middleware ---

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(auth[7:]), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		next(w, r)
	}
}

// --- Public handlers ---

func (s *Server) handleGetNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.db.GetAllNodes()
	if err != nil {
		log.Printf("GetAllNodes error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if nodes == nil {
		nodes = []db.Node{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := s.db.GetNode(id)
	if err != nil {
		log.Printf("GetNode error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if node == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Node not found"})
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) handleGetLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.db.GetAllLinks()
	if err != nil {
		log.Printf("GetAllLinks error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if links == nil {
		links = []db.Link{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	counts, err := s.db.GetStatusCounts()
	if err != nil {
		log.Printf("GetStatusCounts error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func (s *Server) handleGetHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	history, err := s.db.GetHistory(id)
	if err != nil {
		log.Printf("GetHistory error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if history == nil {
		history = []db.StatusHistory{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}

func (s *Server) handleGetScores(w http.ResponseWriter, r *http.Request) {
	scores, err := s.db.GetAllScores()
	if err != nil {
		log.Printf("GetAllScores error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if scores == nil {
		scores = []db.NodeScore{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"scores": scores})
}

// --- Agent handlers ---

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	var req db.ReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.NodeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node_id is required"})
		return
	}
	if s.nodeLocations != nil {
		s.nodeLocations.ApplyReport(&req)
	}

	replay := isHistoricalReplay(req.CollectedAt)
	oldStatus, err := s.db.UpsertReport(&req, !replay)
	if err != nil {
		log.Printf("UpsertReport error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if replay {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	targetStatus := "online"
	reason := "Node healthy"
	if req.Metrics.CPUPercent > 80 || req.Metrics.MemoryPercent > 90 {
		targetStatus = "degraded"
		reason = "High resource usage"
	}

	if err := s.db.SetNodeStatus(req.NodeID, targetStatus); err != nil {
		log.Printf("SetNodeStatus error: %v", err)
	}

	if oldStatus != "" && oldStatus != targetStatus {
		_ = s.db.RecordStatusChange(req.NodeID, oldStatus, targetStatus, reason)
		severity := "info"
		title := "Node recovered"
		if targetStatus == "degraded" {
			severity = "warning"
			title = "Node degraded"
		}
		_ = s.db.RecordEvent(req.NodeID, "status_change", severity, title, reason, "")
		switch targetStatus {
		case "online":
			if _, err := s.db.RecoverNodeIncidents(req.NodeID, "node_offline", "node_degraded"); err != nil {
				log.Printf("RecoverNodeIncidents error: %v", err)
			}
		case "degraded":
			if _, err := s.db.RecoverNodeIncidents(req.NodeID, "node_offline"); err != nil {
				log.Printf("RecoverNodeIncidents error: %v", err)
			}
			fingerprint := db.BuildIncidentFingerprint(req.NodeID, "node_degraded", "Node degraded")
			if _, err := s.db.UpsertIncident(req.NodeID, "node_degraded", "warning", "Node degraded", reason, fingerprint, ""); err != nil {
				log.Printf("UpsertIncident error: %v", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func isHistoricalReplay(collectedAt int64) bool {
	if collectedAt <= 0 {
		return false
	}
	const realtimeGraceSeconds = 180
	return time.Now().Unix()-collectedAt > realtimeGraceSeconds
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID             string  `json:"id"`
		Name           string  `json:"name"`
		Provider       string  `json:"provider"`
		Latitude       float64 `json:"latitude"`
		Longitude      float64 `json:"longitude"`
		LocationSource string  `json:"location_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.ID == "" || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and name are required"})
		return
	}

	if s.nodeLocations != nil {
		overrideReq := db.ReportRequest{
			NodeID:         req.ID,
			Latitude:       req.Latitude,
			Longitude:      req.Longitude,
			LocationSource: req.LocationSource,
		}
		if s.nodeLocations.ApplyReport(&overrideReq) {
			req.Latitude = overrideReq.Latitude
			req.Longitude = overrideReq.Longitude
			req.LocationSource = overrideReq.LocationSource
		}
	}
	if req.LocationSource == "" {
		req.LocationSource = "manual"
	}

	if err := s.db.CreateNode(req.ID, req.Name, req.Provider, req.Latitude, req.Longitude, req.LocationSource); err != nil {
		log.Printf("CreateNode error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

func (s *Server) handleDailyReport(w http.ResponseWriter, r *http.Request) {
	if s.reportGen == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "report generator not available"})
		return
	}
	report := s.reportGen.GenerateReport()
	writeJSON(w, http.StatusOK, map[string]string{"report": report})
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.DeleteNode(id); err != nil {
		log.Printf("DeleteNode error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Download handlers ---

func (s *Server) handleDownloadAgent(w http.ResponseWriter, r *http.Request) {
	if s.agentBinaryPath == "" {
		http.Error(w, "Agent binary not configured", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=starnexus-agent")
	http.ServeFile(w, r, s.agentBinaryPath)
}

func (s *Server) handleDownloadGeoIP(w http.ResponseWriter, r *http.Request) {
	if s.geoipDBPath == "" {
		http.Error(w, "GeoIP DB not configured", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=GeoLite2-City.mmdb")
	http.ServeFile(w, r, s.geoipDBPath)
}

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(installScript))
}


// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
