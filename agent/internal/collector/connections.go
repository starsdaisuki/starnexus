package collector

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
)

// ConnInfo represents an aggregated connection per source IP.
type ConnInfo struct {
	SrcIP      string  `json:"src_ip"`
	SrcLat     float64 `json:"src_lat"`
	SrcLng     float64 `json:"src_lng"`
	SrcCountry string  `json:"src_country"`
	SrcCity    string  `json:"src_city"`
	LocalPort  int     `json:"local_port"`
	Protocol   string  `json:"protocol"`
	Rate       float64 `json:"rate"`        // bytes/sec current speed
	TotalBytes uint64  `json:"total_bytes"` // cumulative since first seen
}

// ConnCollector collects active proxy connections with per-IP rate tracking.
type ConnCollector struct {
	geoReader      *geoip2.Reader
	portLabels     map[int]string
	proxyProcesses []string
	interval       time.Duration

	mu          sync.RWMutex
	listenPorts []int

	// Per-TCP-connection: last known bytes. key = "srcIP:srcPort-localPort"
	prevConnBytes map[string]uint64
	// Per-IP monotonic total. key = "srcIP-localPort"
	ipTotal  map[string]uint64
	ipRate   map[string]float64
	prevTime time.Time
}

var bytesRe = regexp.MustCompile(`bytes_sent:(\d+)`)
var bytesRecvRe = regexp.MustCompile(`bytes_received:(\d+)`)

func NewConnCollector(geoDBPath string, portLabels map[int]string, proxyProcesses []string, intervalSec int) *ConnCollector {
	cc := &ConnCollector{
		portLabels:     portLabels,
		proxyProcesses: proxyProcesses,
		interval:       time.Duration(intervalSec) * time.Second,
		prevConnBytes:  make(map[string]uint64),
		ipTotal:        make(map[string]uint64),
		ipRate:         make(map[string]float64),
	}

	if geoDBPath != "" {
		reader, err := geoip2.Open(geoDBPath)
		if err != nil {
			log.Printf("GeoIP DB open failed (%s): %v — connections will have no geo data", geoDBPath, err)
		} else {
			cc.geoReader = reader
			log.Printf("GeoIP DB loaded: %s", geoDBPath)
		}
	}

	cc.scanPorts()
	return cc
}

func (cc *ConnCollector) Close() {
	if cc.geoReader != nil {
		cc.geoReader.Close()
	}
}

func (cc *ConnCollector) StartPortScanner(stop <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cc.scanPorts()
		case <-stop:
			return
		}
	}
}

// Collect returns per-IP aggregated connections with rate and cumulative bytes.
func (cc *ConnCollector) Collect() []ConnInfo {
	cc.mu.RLock()
	ports := cc.listenPorts
	cc.mu.RUnlock()

	if len(ports) == 0 {
		return nil
	}

	now := time.Now()
	elapsed := now.Sub(cc.prevTime).Seconds()
	if elapsed <= 0 {
		elapsed = float64(cc.interval.Seconds())
	}

	// Step 1: Read all active connections
	curConnBytes := make(map[string]uint64)
	type ipInfo struct {
		srcIP string
		port  int
	}
	ipInfos := make(map[string]*ipInfo) // ipKey → info
	ipDeltas := make(map[string]uint64) // ipKey → sum of deltas this cycle

	for _, port := range ports {
		raw := cc.getConnectionsForPort(port)
		for _, r := range raw {
			connKey := fmt.Sprintf("%s:%d-%d", r.srcIP, r.srcPort, port)
			ipKey := fmt.Sprintf("%s-%d", r.srcIP, port)
			curBytes := r.bytesSent + r.bytesRecv

			curConnBytes[connKey] = curBytes

			if _, ok := ipInfos[ipKey]; !ok {
				ipInfos[ipKey] = &ipInfo{srcIP: r.srcIP, port: port}
			}

			// Delta = new bytes since last sample for THIS connection
			prevBytes, seen := cc.prevConnBytes[connKey]
			if seen && curBytes >= prevBytes {
				ipDeltas[ipKey] += curBytes - prevBytes
			} else if !seen {
				// New connection: count all its current bytes as new
				ipDeltas[ipKey] += curBytes
			}
			// If curBytes < prevBytes (counter reset), skip — shouldn't happen with TCP
		}
	}

	// Step 2: Add deltas to monotonic IP totals, compute rate
	for ipKey, delta := range ipDeltas {
		prev := cc.ipTotal[ipKey]
		cc.ipTotal[ipKey] = prev + delta
		if elapsed > 0 {
			cc.ipRate[ipKey] = float64(delta) / elapsed
		}
	}

	// Zero out rate for IPs that had no delta this cycle but still have active connections
	for ipKey := range ipInfos {
		if _, hasDelta := ipDeltas[ipKey]; !hasDelta {
			cc.ipRate[ipKey] = 0
		}
	}

	// Step 3: Build result
	var conns []ConnInfo
	for ipKey, info := range ipInfos {
		total := cc.ipTotal[ipKey]
		rate := cc.ipRate[ipKey]

		lat, lng, country, city := cc.lookupGeo(info.srcIP)
		protocol := fmt.Sprintf("port-%d", info.port)
		if label, ok := cc.portLabels[info.port]; ok {
			protocol = label
		}

		conns = append(conns, ConnInfo{
			SrcIP: info.srcIP, SrcLat: lat, SrcLng: lng,
			SrcCountry: country, SrcCity: city,
			LocalPort: info.port, Protocol: protocol,
			Rate: rate, TotalBytes: total,
		})
	}

	cc.prevConnBytes = curConnBytes
	cc.prevTime = now
	return conns
}

// --- Port scanning ---

func (cc *ConnCollector) scanPorts() {
	out, err := exec.Command("ss", "-tlnp").CombinedOutput()
	if err != nil {
		log.Printf("ss -tlnp failed: %v", err)
		return
	}

	var ports []int
	seen := make(map[int]bool)

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "LISTEN") {
			continue
		}

		matchesProxy := false
		lower := strings.ToLower(line)
		for _, proc := range cc.proxyProcesses {
			if strings.Contains(lower, strings.ToLower(proc)) {
				matchesProxy = true
				break
			}
		}
		if !matchesProxy {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		_, port := parseAddr(fields[3])
		if port == 0 {
			localAddr := fields[3]
			idx := strings.LastIndex(localAddr, ":")
			if idx >= 0 {
				port, _ = strconv.Atoi(localAddr[idx+1:])
			}
		}
		// port already parsed above
		if port > 0 && !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
	}

	cc.mu.Lock()
	cc.listenPorts = ports
	cc.mu.Unlock()

	if len(ports) > 0 {
		log.Printf("Detected proxy ports: %v", ports)
	}
}

// --- Connection details ---

type rawConn struct {
	srcIP     string
	srcPort   int
	bytesSent uint64
	bytesRecv uint64
}

func (cc *ConnCollector) getConnectionsForPort(port int) []rawConn {
	out, err := exec.Command("ss", "-tni", fmt.Sprintf("sport = :%d", port)).CombinedOutput()
	if err != nil {
		return nil
	}

	var conns []rawConn
	lines := strings.Split(string(out), "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "ESTAB") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		srcIP, srcPort := parseAddr(fields[4])
		if srcIP == "" {
			continue
		}

		if isPrivateIP(srcIP) {
			continue
		}

		var bytesSent, bytesRecv uint64
		if i+1 < len(lines) {
			infoLine := lines[i+1]
			if m := bytesRe.FindStringSubmatch(infoLine); len(m) > 1 {
				bytesSent, _ = strconv.ParseUint(m[1], 10, 64)
			}
			if m := bytesRecvRe.FindStringSubmatch(infoLine); len(m) > 1 {
				bytesRecv, _ = strconv.ParseUint(m[1], 10, 64)
			}
		}

		conns = append(conns, rawConn{
			srcIP:     srcIP,
			srcPort:   srcPort,
			bytesSent: bytesSent,
			bytesRecv: bytesRecv,
		})
	}

	return conns
}

// --- GeoIP ---

func (cc *ConnCollector) lookupGeo(ipStr string) (lat, lng float64, country, city string) {
	if cc.geoReader == nil {
		return
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return
	}
	record, err := cc.geoReader.City(ip)
	if err != nil {
		return
	}
	lat = record.Location.Latitude
	lng = record.Location.Longitude
	country = record.Country.Names["en"]
	city = record.City.Names["en"]
	return
}

// --- Helpers ---

func parseAddr(addr string) (string, int) {
	if strings.HasPrefix(addr, "[") {
		closeBracket := strings.Index(addr, "]")
		if closeBracket < 0 {
			return "", 0
		}
		ip := addr[1:closeBracket]
		portStr := ""
		if closeBracket+2 < len(addr) && addr[closeBracket+1] == ':' {
			portStr = addr[closeBracket+2:]
		}
		port, _ := strconv.Atoi(portStr)
		if strings.HasPrefix(ip, "::ffff:") {
			ip = ip[7:]
		}
		return ip, port
	}
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return "", 0
	}
	port, _ := strconv.Atoi(addr[idx+1:])
	return addr[:idx], port
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}
