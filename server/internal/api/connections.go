package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"sync"

	"github.com/starsdaisuki/starnexus/server/internal/db"
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

func (cs *ConnStore) GetNode(nodeID string) []ConnInfo {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	conns := make([]ConnInfo, len(cs.data[nodeID]))
	copy(conns, cs.data[nodeID])
	sort.Slice(conns, func(i, j int) bool {
		return conns[i].Rate > conns[j].Rate
	})
	if len(conns) > 12 {
		conns = conns[:12]
	}
	return conns
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
	s.persistConnectionSamples(req.NodeID, filtered)
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

func (s *Server) persistConnectionSamples(nodeID string, conns []ConnInfo) {
	if len(conns) == 0 {
		return
	}

	sorted := make([]ConnInfo, len(conns))
	copy(sorted, conns)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Rate > sorted[j].Rate
	})
	if len(sorted) > 20 {
		sorted = sorted[:20]
	}

	samples := make([]db.ConnectionSampleInput, 0, len(sorted))
	for _, conn := range sorted {
		samples = append(samples, db.ConnectionSampleInput{
			SourceKey:     connectionSourceKey(conn),
			SourceIP:      conn.SrcIP,
			SourceCountry: conn.SrcCountry,
			SourceCity:    conn.SrcCity,
			Protocol:      conn.Protocol,
			LocalPort:     conn.LocalPort,
			IsCloudflare:  isCloudflareIP(conn.SrcIP),
			RateBPS:       conn.Rate,
			TotalBytes:    conn.TotalBytes,
		})
	}

	_ = s.db.SaveConnectionSamples(nodeID, samples)
}

func connectionSourceKey(conn ConnInfo) string {
	return conn.SrcIP + "|" + conn.Protocol + "|" + strconv.Itoa(conn.LocalPort)
}

var cloudflarePrefixes = []string{
	"173.245.", "103.21.", "103.22.", "103.31.", "141.101.", "108.162.",
	"190.93.", "188.114.", "197.234.", "198.41.", "162.158.", "104.16.",
	"104.17.", "104.18.", "104.19.", "104.20.", "104.21.", "104.22.",
	"104.23.", "104.24.", "104.25.", "104.26.", "104.27.", "104.28.",
	"104.29.", "104.30.", "104.31.", "172.64.", "131.0.",
}

func isCloudflareIP(ip string) bool {
	for _, prefix := range cloudflarePrefixes {
		if len(ip) >= len(prefix) && ip[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
