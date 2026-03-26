package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
)

// ConnInfo mirrors the agent's connection data.
type ConnInfo struct {
	SrcIP      string  `json:"src_ip"`
	SrcLat     float64 `json:"src_lat"`
	SrcLng     float64 `json:"src_lng"`
	SrcCountry string  `json:"src_country"`
	SrcCity    string  `json:"src_city"`
	LocalPort  int     `json:"local_port"`
	Protocol   string  `json:"protocol"`
	Rate       float64 `json:"rate"`
	TotalBytes uint64  `json:"total_bytes"`
}

type connReport struct {
	NodeID      string     `json:"node_id"`
	Connections []ConnInfo `json:"connections"`
}

// ConnStore keeps the latest connection snapshot per node (in-memory only).
type ConnStore struct {
	mu   sync.RWMutex
	data map[string][]ConnInfo // node_id -> connections
}

func NewConnStore() *ConnStore {
	return &ConnStore{data: make(map[string][]ConnInfo)}
}

func (cs *ConnStore) Update(nodeID string, conns []ConnInfo) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.data[nodeID] = conns
}

func (cs *ConnStore) GetAll() map[string][]ConnInfo {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	result := make(map[string][]ConnInfo, len(cs.data))
	for k, v := range cs.data {
		// Return top 20 by rate per node
		conns := make([]ConnInfo, len(v))
		copy(conns, v)
		sort.Slice(conns, func(i, j int) bool {
			return conns[i].Rate > conns[j].Rate
		})
		if len(conns) > 20 {
			conns = conns[:20]
		}
		result[k] = conns
	}
	return result
}

// --- Handlers ---

func (s *Server) handlePostConnections(w http.ResponseWriter, r *http.Request) {
	var req connReport
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.NodeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node_id is required"})
		return
	}

	// Filter out connections from known node IPs (proxy chain traffic)
	nodeIPs := s.getNodeIPs()
	var filtered []ConnInfo
	for _, c := range req.Connections {
		if !nodeIPs[c.SrcIP] {
			filtered = append(filtered, c)
		}
	}

	s.connStore.Update(req.NodeID, filtered)
	w.WriteHeader(http.StatusOK)
}

// getNodeIPs returns a set of all known node IP addresses.
func (s *Server) getNodeIPs() map[string]bool {
	nodes, err := s.db.GetAllNodes()
	if err != nil {
		return nil
	}
	ips := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		if n.IPAddress != nil && *n.IPAddress != "" {
			ips[*n.IPAddress] = true
		}
	}
	return ips
}

func (s *Server) handleGetConnections(w http.ResponseWriter, r *http.Request) {
	data := s.connStore.GetAll()
	writeJSON(w, http.StatusOK, data)
}
