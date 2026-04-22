package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
	"github.com/starsdaisuki/starnexus/server/internal/locations"
)

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
	startedAt            int64
}

func New(database *db.DB, token, webDir, agentBinaryPath, geoipDBPath, experimentLabelsPath string, nodeLocations *locations.Store) *Server {
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
		startedAt:            time.Now().Unix(),
	}
	s.routes()
	return s
}

// SetReportGenerator sets the report generator (called after analytics scheduler is created).
func (s *Server) SetReportGenerator(rg ReportGenerator) {
	s.reportGen = rg
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
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

	// Downloads (no auth)
	s.mux.HandleFunc("GET /download/agent", s.handleDownloadAgent)
	s.mux.HandleFunc("GET /download/geoip", s.handleDownloadGeoIP)
	s.mux.HandleFunc("GET /download/install.sh", s.handleInstallScript)
	s.mux.HandleFunc("GET /install.sh", s.handleInstallScript)

	// Static files
	if s.webDir != "" {
		fs := http.FileServer(http.Dir(s.webDir))
		s.mux.Handle("GET /", fs)
	}
}

// --- Middleware ---

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || auth[7:] != s.token {
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

	oldStatus, err := s.db.UpsertReport(&req)
	if err != nil {
		log.Printf("UpsertReport error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if isHistoricalReplay(req.CollectedAt) {
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

const installScript = `#!/usr/bin/env bash
set -euo pipefail

# StarNexus Agent Install Script
# Usage: curl -sSL http://<server>:8900/install.sh | bash -s -- \
#   --server http://<server>:8900 --token <token> --node-id <id> --node-name "<name>"

SERVER_URL=""
API_TOKEN=""
NODE_ID=""
NODE_NAME=""
PROVIDER=""
INSTALL_DIR="$HOME/starnexus"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server)   SERVER_URL="$2"; shift 2 ;;
    --token)    API_TOKEN="$2"; shift 2 ;;
    --node-id)  NODE_ID="$2"; shift 2 ;;
    --node-name) NODE_NAME="$2"; shift 2 ;;
    --provider) PROVIDER="$2"; shift 2 ;;
    --dir)      INSTALL_DIR="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

if [[ -z "$SERVER_URL" || -z "$API_TOKEN" || -z "$NODE_ID" ]]; then
  echo "Error: --server, --token, and --node-id are required"
  echo ""
  echo "Usage: curl -sSL http://<server>:8900/install.sh | bash -s -- \\"
  echo "  --server http://<server>:8900 \\"
  echo "  --token <api-token> \\"
  echo "  --node-id <node-id> \\"
  echo "  --node-name \"<display name>\" \\"
  echo "  --provider \"<provider name>\""
  exit 1
fi

[[ -z "$NODE_NAME" ]] && NODE_NAME="$NODE_ID"
[[ -z "$PROVIDER" ]] && PROVIDER="Unknown"

echo "==> Installing StarNexus Agent"
echo "    Server:  $SERVER_URL"
echo "    Node ID: $NODE_ID"
echo "    Name:    $NODE_NAME"
echo "    Dir:     $INSTALL_DIR"
echo ""

# Create directory
mkdir -p "$INSTALL_DIR"

# Download agent binary
echo "==> Downloading agent binary..."
curl -sSL "$SERVER_URL/download/agent" -o "$INSTALL_DIR/starnexus-agent"
chmod +x "$INSTALL_DIR/starnexus-agent"
echo "    Downloaded: $INSTALL_DIR/starnexus-agent"

# Write config (lat/lng = 0 triggers auto-detect on first run)
cat > "$INSTALL_DIR/config.yaml" << YAML
server_url: "$SERVER_URL"
api_token: "$API_TOKEN"
node_id: "$NODE_ID"
node_name: "$NODE_NAME"
provider: "$PROVIDER"
latitude: 0
longitude: 0
report_interval_seconds: 30
queue_path: "./agent-queue.jsonl"
queue_max_reports: 2880
queue_flush_batch_size: 120
YAML
echo "    Config written: $INSTALL_DIR/config.yaml"

# Download GeoIP database (for connection visualization)
echo "==> Downloading GeoIP database..."
if curl -sSL --fail "$SERVER_URL/download/geoip" -o "$INSTALL_DIR/GeoLite2-City.mmdb" 2>/dev/null; then
  echo "    Downloaded: $INSTALL_DIR/GeoLite2-City.mmdb"
else
  echo "    GeoIP DB not available on server (connection tracking will be disabled)"
fi

# Create systemd service
cat > /etc/systemd/system/starnexus-agent.service << UNIT
[Unit]
Description=StarNexus Agent
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/starnexus-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT
echo "    Systemd service created"

# Enable and start
systemctl daemon-reload
systemctl enable --now starnexus-agent
sleep 3

# Status check
if systemctl is-active --quiet starnexus-agent; then
  echo ""
  echo "==> StarNexus Agent installed and running!"
  echo "    Status: $(systemctl is-active starnexus-agent)"
  echo "    Logs:   journalctl -u starnexus-agent -f"
else
  echo ""
  echo "==> WARNING: Agent may not have started correctly."
  echo "    Check: journalctl -u starnexus-agent --no-pager -n 20"
fi
`

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
