package probe

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/starsdaisuki/starnexus/agent/internal/config"
)

// LinkResult holds the result of probing a single target.
type LinkResult struct {
	TargetNodeID string  `json:"target_node_id"`
	LatencyMs    float64 `json:"latency_ms"`
	PacketLoss   float64 `json:"packet_loss"`
}

var (
	lossRe = regexp.MustCompile(`(\d+(?:\.\d+)?)% packet loss`)
	rttRe  = regexp.MustCompile(`rtt min/avg/max/mdev = [\d.]+/([\d.]+)/`)
)

// ProbeAll pings each target and returns link results.
func ProbeAll(targets []config.ProbeTarget) []LinkResult {
	var results []LinkResult
	for _, t := range targets {
		r := probeOne(t)
		results = append(results, r)
	}
	return results
}

func probeOne(target config.ProbeTarget) LinkResult {
	r := LinkResult{
		TargetNodeID: target.NodeID,
		LatencyMs:    -1,
		PacketLoss:   100,
	}

	// ping -c 5 -W 3 <host>
	out, _ := exec.Command("ping", "-c", "5", "-W", "3", target.Host).CombinedOutput()
	output := string(out)

	// Parse packet loss
	if m := lossRe.FindStringSubmatch(output); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			r.PacketLoss = v
		}
	}

	// Parse avg latency from "rtt min/avg/max/mdev = ..."
	if m := rttRe.FindStringSubmatch(output); len(m) > 1 {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			r.LatencyMs = v
		}
	}

	// If no rtt line but we got some output, try alternate format
	if r.LatencyMs < 0 && strings.Contains(output, "time=") {
		// Fallback: won't happen with -c 5, but safe
		r.LatencyMs = 999
	}

	return r
}
