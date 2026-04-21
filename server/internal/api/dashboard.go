package api

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/analytics"
	"github.com/starsdaisuki/starnexus/server/internal/db"
)

type dashboardResponse struct {
	GeneratedAt    int64                            `json:"generated_at"`
	Status         *db.StatusCounts                 `json:"status"`
	Nodes          []db.Node                        `json:"nodes"`
	Links          []db.Link                        `json:"links"`
	Scores         []db.NodeScore                   `json:"scores"`
	Events         []db.Event                       `json:"events"`
	HotSources     []db.ConnectionSummary           `json:"hot_sources"`
	FleetAnalytics analytics.FleetAnalytics         `json:"fleet_analytics"`
	Reliability    analytics.ReliabilityAnalytics   `json:"reliability_analytics"`
	GroundTruth    *analytics.GroundTruthEvaluation `json:"ground_truth,omitempty"`
}

type nodeDetailsResponse struct {
	GeneratedAt        int64                     `json:"generated_at"`
	Node               *db.Node                  `json:"node"`
	Score              *db.NodeScore             `json:"score,omitempty"`
	History            []db.StatusHistory        `json:"history"`
	Events             []db.Event                `json:"events"`
	Links              []db.Link                 `json:"links"`
	MetricsWindowHours int                       `json:"metrics_window_hours"`
	Metrics            []db.MetricPoint          `json:"metrics"`
	RecentConnections  []db.ConnectionSummary    `json:"recent_connections"`
	LiveConnections    []ConnInfo                `json:"live_connections"`
	Analytics          analytics.DetailAnalytics `json:"analytics"`
}

func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.db.GetAllNodes()
	if err != nil {
		log.Printf("GetAllNodes error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	links, err := s.db.GetAllLinks()
	if err != nil {
		log.Printf("GetAllLinks error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	status, err := s.db.GetStatusCounts()
	if err != nil {
		log.Printf("GetStatusCounts error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	scores, err := s.db.GetAllScores()
	if err != nil {
		log.Printf("GetAllScores error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if scores == nil {
		scores = []db.NodeScore{}
	}

	events, err := s.db.GetRecentEvents(limitParam(r, 15))
	if err != nil {
		log.Printf("GetRecentEvents error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if events == nil {
		events = []db.Event{}
	}

	hotSources, err := s.db.GetConnectionHighlights(time.Now().Add(-24*time.Hour).Unix(), 8)
	if err != nil {
		log.Printf("GetConnectionHighlights error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if hotSources == nil {
		hotSources = []db.ConnectionSummary{}
	}

	scoreByNode := make(map[string]*db.NodeScore, len(scores))
	for i := range scores {
		scoreByNode[scores[i].NodeID] = &scores[i]
	}

	now := time.Now()
	from := now.Add(-24 * time.Hour).Unix()
	windowEvents, err := s.db.GetEventsSince(from, 1000)
	if err != nil {
		log.Printf("GetEventsSince for reliability analytics error: %v", err)
		windowEvents = []db.Event{}
	}

	fleetSamples := make([]analytics.FleetNodeSample, 0, len(nodes))
	pointsByNode := make(map[string][]db.MetricPoint, len(nodes))
	for _, node := range nodes {
		points, err := s.db.GetMetricPoints(node.ID, from, now.Unix())
		if err != nil {
			log.Printf("GetMetricPoints for fleet analytics error (%s): %v", node.ID, err)
			continue
		}
		pointsByNode[node.ID] = points
		fleetSamples = append(fleetSamples, analytics.FleetNodeSample{
			Node:      node,
			Score:     scoreByNode[node.ID],
			Analytics: analytics.BuildDetailAnalytics(points, 24),
		})
	}
	groundTruth := s.buildDashboardGroundTruth(from, pointsByNode)
	reliability := analytics.BuildReliabilityAnalytics(24, now.Unix(), fleetSamples, windowEvents)

	writeJSON(w, http.StatusOK, dashboardResponse{
		GeneratedAt:    now.Unix(),
		Status:         status,
		Nodes:          nodes,
		Links:          links,
		Scores:         scores,
		Events:         events,
		HotSources:     hotSources,
		FleetAnalytics: analytics.BuildFleetAnalytics(24, fleetSamples),
		Reliability:    reliability,
		GroundTruth:    groundTruth,
	})
}

func (s *Server) buildDashboardGroundTruth(from int64, pointsByNode map[string][]db.MetricPoint) *analytics.GroundTruthEvaluation {
	if s.experimentLabelsPath == "" {
		return nil
	}
	labels, err := analytics.LoadExperimentLabelsJSONL(s.experimentLabelsPath)
	if err != nil {
		log.Printf("LoadExperimentLabelsJSONL error: %v", err)
		return nil
	}
	labels = filterExperimentLabelsSince(labels, from)
	if len(labels) == 0 {
		return nil
	}
	events, err := s.db.GetEventsSince(from, 5000)
	if err != nil {
		log.Printf("GetEventsSince for ground truth error: %v", err)
		return nil
	}
	groundTruth := analytics.BuildGroundTruthEvaluation(labels, events, pointsByNode)
	return &groundTruth
}

func filterExperimentLabelsSince(labels []analytics.ExperimentLabel, from int64) []analytics.ExperimentLabel {
	filtered := make([]analytics.ExperimentLabel, 0, len(labels))
	for _, label := range labels {
		if label.EndedAt >= from {
			filtered = append(filtered, label)
		}
	}
	return filtered
}

func (s *Server) handleGetNodeDetails(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	node, err := s.db.GetNode(nodeID)
	if err != nil {
		log.Printf("GetNode error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if node == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Node not found"})
		return
	}

	hours := parseHoursParam(r, 24)
	now := time.Now()
	from := now.Add(-time.Duration(hours) * time.Hour).Unix()

	points, err := s.db.GetMetricPoints(nodeID, from, now.Unix())
	if err != nil {
		log.Printf("GetMetricPoints error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	history, err := s.db.GetHistory(nodeID)
	if err != nil {
		log.Printf("GetHistory error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if history == nil {
		history = []db.StatusHistory{}
	}

	events, err := s.db.GetNodeEvents(nodeID, 12)
	if err != nil {
		log.Printf("GetNodeEvents error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if events == nil {
		events = []db.Event{}
	}

	score, err := s.db.GetNodeScore(nodeID)
	if err != nil {
		log.Printf("GetNodeScore error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	allLinks, err := s.db.GetAllLinks()
	if err != nil {
		log.Printf("GetAllLinks error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	links := make([]db.Link, 0, len(allLinks))
	for _, link := range allLinks {
		if link.SourceNodeID == nodeID || link.TargetNodeID == nodeID {
			links = append(links, link)
		}
	}

	recentConnections, err := s.db.GetNodeConnectionSummary(nodeID, now.Add(-24*time.Hour).Unix(), 10)
	if err != nil {
		log.Printf("GetNodeConnectionSummary error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if recentConnections == nil {
		recentConnections = []db.ConnectionSummary{}
	}

	liveConnections := s.connStore.GetNode(nodeID)
	if liveConnections == nil {
		liveConnections = []ConnInfo{}
	}

	writeJSON(w, http.StatusOK, nodeDetailsResponse{
		GeneratedAt:        now.Unix(),
		Node:               node,
		Score:              score,
		History:            history,
		Events:             events,
		Links:              links,
		MetricsWindowHours: hours,
		Metrics:            db.DownsampleMetricPoints(points, 120),
		RecentConnections:  recentConnections,
		LiveConnections:    liveConnections,
		Analytics:          analytics.BuildDetailAnalytics(points, hours),
	})
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.db.GetRecentEvents(limitParam(r, 20))
	if err != nil {
		log.Printf("GetRecentEvents error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if events == nil {
		events = []db.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func limitParam(r *http.Request, fallback int) int {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseHoursParam(r *http.Request, fallback int) int {
	value := r.URL.Query().Get("hours")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > 168 {
		return 168
	}
	return parsed
}
