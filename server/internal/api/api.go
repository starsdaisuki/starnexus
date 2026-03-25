package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type Server struct {
	db       *db.DB
	token    string
	webDir   string
	mux      *http.ServeMux
}

func New(database *db.DB, token, webDir string) *Server {
	s := &Server{
		db:     database,
		token:  token,
		webDir: webDir,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Public API
	s.mux.HandleFunc("GET /api/nodes", s.handleGetNodes)
	s.mux.HandleFunc("GET /api/nodes/{id}", s.handleGetNode)
	s.mux.HandleFunc("GET /api/links", s.handleGetLinks)
	s.mux.HandleFunc("GET /api/status", s.handleGetStatus)
	s.mux.HandleFunc("GET /api/history/{id}", s.handleGetHistory)

	// Agent API (auth required)
	s.mux.HandleFunc("POST /api/report", s.requireAuth(s.handleReport))
	s.mux.HandleFunc("POST /api/nodes", s.requireAuth(s.handleCreateNode))

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

	oldStatus, err := s.db.UpsertReport(&req)
	if err != nil {
		log.Printf("UpsertReport error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Record status change if node came back online
	if oldStatus != "" && oldStatus != "online" {
		_ = s.db.RecordStatusChange(req.NodeID, oldStatus, "online", "Node reported in")
	}

	// Threshold alerts
	if req.Metrics.CPUPercent > 80 || req.Metrics.MemoryPercent > 90 {
		newStatus := "degraded"
		if oldStatus == "online" || oldStatus == "" {
			_ = s.db.RecordStatusChange(req.NodeID, "online", newStatus, "High resource usage")
		}
		// Mark as degraded
		s.db.SetNodeDegraded(req.NodeID)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		Provider  string  `json:"provider"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.ID == "" || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and name are required"})
		return
	}

	if err := s.db.CreateNode(req.ID, req.Name, req.Provider, req.Latitude, req.Longitude); err != nil {
		log.Printf("CreateNode error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
