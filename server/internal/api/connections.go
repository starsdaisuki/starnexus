package api

import (
	"encoding/json"
	"fmt"
	"net"
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

// Cloudflare's published IPv4 ranges (https://www.cloudflare.com/ips-v4).
// Proper CIDR matching matters here: the old string-prefix list both
// over-matched (Cloudflare owns 103.21.244.0/22, not all of 103.21.x)
// and under-matched (172.64.0.0/13 spans 172.64–71, not just 172.64.x).
var cloudflareCIDRs = mustParseCIDRs(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("invalid built-in CIDR %q: %v", cidr, err))
		}
		nets = append(nets, ipNet)
	}
	return nets
}

func isCloudflareIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, ipNet := range cloudflareCIDRs {
		if ipNet.Contains(parsed) {
			return true
		}
	}
	return false
}
