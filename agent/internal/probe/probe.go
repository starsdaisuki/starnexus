package probe

import (
	"fmt"
	"net"
	"time"

	"github.com/starsdaisuki/starnexus/agent/internal/config"
)

// LinkResult holds the result of probing a single target.
type LinkResult struct {
	TargetNodeID string  `json:"target_node_id"`
	LatencyMs    float64 `json:"latency_ms"`
	PacketLoss   float64 `json:"packet_loss"`
}

// ProbeAll probes each target via TCP connect and returns results.
func ProbeAll(targets []config.ProbeTarget) []LinkResult {
	var results []LinkResult
	for _, t := range targets {
		results = append(results, tcpProbe(t))
	}
	return results
}

// tcpProbe measures TCP handshake latency to host:port.
// Sends 5 probes, returns average latency and packet loss.
func tcpProbe(target config.ProbeTarget) LinkResult {
	r := LinkResult{
		TargetNodeID: target.NodeID,
		LatencyMs:    -1,
		PacketLoss:   100,
	}

	port := target.Port
	if port == 0 {
		port = 22 // default SSH
	}

	addr := fmt.Sprintf("%s:%d", target.Host, port)
	const attempts = 5
	var totalMs float64
	var successes int

	for i := 0; i < attempts; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
		elapsed := time.Since(start)

		if err == nil {
			conn.Close()
			totalMs += float64(elapsed.Microseconds()) / 1000.0
			successes++
		}

		// Small delay between probes
		if i < attempts-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	if successes > 0 {
		r.LatencyMs = totalMs / float64(successes)
		r.PacketLoss = float64(attempts-successes) / float64(attempts) * 100
	}

	return r
}
