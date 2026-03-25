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

// ConnInfo represents a single active connection with geo + rate data.
type ConnInfo struct {
	SrcIP      string  `json:"src_ip"`
	SrcLat     float64 `json:"src_lat"`
	SrcLng     float64 `json:"src_lng"`
	SrcCountry string  `json:"src_country"`
	SrcCity    string  `json:"src_city"`
	LocalPort  int     `json:"local_port"`
	Protocol   string  `json:"protocol"`
	BytesUp    uint64  `json:"bytes_up"`
	BytesDown  uint64  `json:"bytes_down"`
	RateUp     float64 `json:"rate_up"`
	RateDown   float64 `json:"rate_down"`
}

// ConnCollector collects active proxy connections with geo and rate data.
type ConnCollector struct {
	geoReader      *geoip2.Reader
	portLabels     map[int]string
	proxyProcesses []string
	interval       time.Duration

	mu           sync.RWMutex
	listenPorts  []int
	prevBytes    map[string]prevConn // key: "srcIP:srcPort-localPort"
	prevTime     time.Time
}

type prevConn struct {
	bytesUp   uint64
	bytesDown uint64
}

var bytesRe = regexp.MustCompile(`bytes_sent:(\d+)`)
var bytesRecvRe = regexp.MustCompile(`bytes_received:(\d+)`)

// NewConnCollector creates a connection collector with GeoIP and port labels.
func NewConnCollector(geoDBPath string, portLabels map[int]string, proxyProcesses []string, intervalSec int) *ConnCollector {
	cc := &ConnCollector{
		portLabels:     portLabels,
		proxyProcesses: proxyProcesses,
		interval:       time.Duration(intervalSec) * time.Second,
		prevBytes:      make(map[string]prevConn),
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

	// Initial port scan
	cc.scanPorts()

	return cc
}

func (cc *ConnCollector) Close() {
	if cc.geoReader != nil {
		cc.geoReader.Close()
	}
}

// StartPortScanner re-scans listening ports every 5 minutes.
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

// Collect returns current active connections with geo and rate data.
// Multiple TCP connections from the same source IP are aggregated into one entry.
// Rate semantics from the USER's perspective:
//   RateUp   = user uploading   = server bytes_received
//   RateDown = user downloading = server bytes_sent
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

	// Phase 1: collect all raw connections, compute per-connection rates
	newPrev := make(map[string]prevConn)
	type perIPData struct {
		srcIP              string
		port               int
		totalBytesSent     uint64 // server sent = user download
		totalBytesRecv     uint64 // server recv = user upload
		totalRateSent      float64
		totalRateRecv      float64
	}

	// Aggregate by "srcIP:port"
	aggMap := make(map[string]*perIPData)

	for _, port := range ports {
		raw := cc.getConnectionsForPort(port)
		for _, r := range raw {
			connKey := fmt.Sprintf("%s:%d-%d", r.srcIP, r.srcPort, port)

			// Per-connection rate from delta
			var rateSent, rateRecv float64
			if prev, ok := cc.prevBytes[connKey]; ok {
				if r.bytesSent >= prev.bytesUp {
					rateSent = float64(r.bytesSent-prev.bytesUp) / elapsed
				}
				if r.bytesRecv >= prev.bytesDown {
					rateRecv = float64(r.bytesRecv-prev.bytesDown) / elapsed
				}
			}

			newPrev[connKey] = prevConn{bytesUp: r.bytesSent, bytesDown: r.bytesRecv}

			// Aggregate by srcIP + port
			aggKey := fmt.Sprintf("%s-%d", r.srcIP, port)
			agg, ok := aggMap[aggKey]
			if !ok {
				agg = &perIPData{srcIP: r.srcIP, port: port}
				aggMap[aggKey] = agg
			}
			agg.totalBytesSent += r.bytesSent
			agg.totalBytesRecv += r.bytesRecv
			agg.totalRateSent += rateSent
			agg.totalRateRecv += rateRecv
		}
	}

	cc.prevBytes = newPrev
	cc.prevTime = now

	// Phase 2: build ConnInfo list from aggregated data
	var conns []ConnInfo
	for _, agg := range aggMap {
		lat, lng, country, city := cc.lookupGeo(agg.srcIP)

		protocol := fmt.Sprintf("port-%d", agg.port)
		if label, ok := cc.portLabels[agg.port]; ok {
			protocol = label
		}

		conns = append(conns, ConnInfo{
			SrcIP:      agg.srcIP,
			SrcLat:     lat,
			SrcLng:     lng,
			SrcCountry: country,
			SrcCity:    city,
			LocalPort:  agg.port,
			Protocol:   protocol,
			BytesUp:    agg.totalBytesRecv,  // user upload = server recv
			BytesDown:  agg.totalBytesSent,  // user download = server sent
			RateUp:     agg.totalRateRecv,   // user upload rate
			RateDown:   agg.totalRateSent,   // user download rate
		})
	}

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

		// Check if process matches any proxy process name
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

		// Extract port from local address field
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		_, port := parseAddr(fields[3])
		// parseAddr may fail on "0.0.0.0:443" — try LastIndex fallback
		if port == 0 {
			localAddr := fields[3]
			idx := strings.LastIndex(localAddr, ":")
			if idx >= 0 {
				port, _ = strconv.Atoi(localAddr[idx+1:])
			}
		}
		// port already parsed above
		if err != nil || port == 0 {
			continue
		}
		if !seen[port] {
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
	// Use ss -tni to get connections with TCP info (byte counters)
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

		// Parse peer address (field 4)
		// Handles: "1.2.3.4:56789" and "[::ffff:1.2.3.4]:56789"
		srcIP, srcPort := parseAddr(fields[4])
		if srcIP == "" {
			continue
		}

		// Skip private/loopback IPs
		if isPrivateIP(srcIP) {
			continue
		}

		// Next line should be TCP info
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

// parseAddr parses "1.2.3.4:56789" or "[::ffff:1.2.3.4]:56789" into IP and port.
func parseAddr(addr string) (string, int) {
	// Bracketed form: [::ffff:1.2.3.4]:port or [::1]:port
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

		// Extract IPv4 from ::ffff:x.x.x.x mapped address
		if strings.HasPrefix(ip, "::ffff:") {
			ip = ip[7:]
		}
		return ip, port
	}

	// Plain form: 1.2.3.4:56789
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
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}
