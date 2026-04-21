package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func (s *Server) handleGetIncidents(w http.ResponseWriter, r *http.Request) {
	limit := limitParam(r, 20)
	status := r.URL.Query().Get("status")

	var incidents []db.Incident
	var err error
	if status == "all" || status == "recent" {
		incidents, err = s.db.GetRecentIncidents(limit)
	} else {
		incidents, err = s.db.GetActiveIncidents(limit)
	}
	if err != nil {
		log.Printf("GetIncidents error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if incidents == nil {
		incidents = []db.Incident{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": incidents})
}

func (s *Server) handleAckIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentID(w, r)
	if !ok {
		return
	}

	var req struct {
		Actor string `json:"actor"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	incident, err := s.db.AcknowledgeIncident(id, req.Actor)
	if err != nil {
		log.Printf("AcknowledgeIncident error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if incident == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (s *Server) handleSuppressIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentID(w, r)
	if !ok {
		return
	}

	var req struct {
		Actor           string `json:"actor"`
		DurationSeconds int64  `json:"duration_seconds"`
		SuppressUntil   int64  `json:"suppress_until"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	until := req.SuppressUntil
	if until == 0 && req.DurationSeconds > 0 {
		until = time.Now().Add(time.Duration(req.DurationSeconds) * time.Second).Unix()
	}
	if until <= time.Now().Unix() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "suppress_until or duration_seconds must be in the future"})
		return
	}

	incident, err := s.db.SuppressIncident(id, until, req.Actor)
	if err != nil {
		log.Printf("SuppressIncident error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if incident == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func incidentID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid incident id"})
		return 0, false
	}
	return id, true
}
