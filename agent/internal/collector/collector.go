package collector

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Metrics holds all collected system metrics.
type Metrics struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	DiskPercent   float64 `json:"disk_percent"`
	BandwidthUp   float64 `json:"bandwidth_up"`   // KB/s
	BandwidthDown float64 `json:"bandwidth_down"` // KB/s
	LoadAvg       float64 `json:"load_avg"`
	Connections   int     `json:"connections"`
	UptimeSeconds int64   `json:"uptime_seconds"`
}

// Collect gathers all system metrics. Takes ~2 seconds due to CPU sampling.
func Collect() (*Metrics, error) {
	m := &Metrics{}

	cpu1, err := readCPUSample()
	if err != nil {
		return nil, fmt.Errorf("cpu sample 1: %w", err)
	}
	net1, err := readNetworkBytes()
	if err != nil {
		return nil, fmt.Errorf("net sample 1: %w", err)
	}
	start := time.Now()

	time.Sleep(1 * time.Second)

	cpu2, err := readCPUSample()
	if err != nil {
		return nil, fmt.Errorf("cpu sample 2: %w", err)
	}

	time.Sleep(1 * time.Second)

	cpu3, err := readCPUSample()
	if err != nil {
		return nil, fmt.Errorf("cpu sample 3: %w", err)
	}
	net2, err := readNetworkBytes()
	if err != nil {
		return nil, fmt.Errorf("net sample 2: %w", err)
	}

	// Median over two short intervals and one combined interval suppresses
	// single-tick artifacts on low-load virtualized CPUs.
	m.CPUPercent = median3(
		calculateCPUPercent(cpu1, cpu2),
		calculateCPUPercent(cpu2, cpu3),
		calculateCPUPercent(cpu1, cpu3),
	)

	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	m.BandwidthUp = float64(net2.txBytes-net1.txBytes) / 1024.0 / elapsed
	m.BandwidthDown = float64(net2.rxBytes-net1.rxBytes) / 1024.0 / elapsed

	// Memory
	mem, err := readMemory()
	if err == nil {
		m.MemoryPercent = mem
	}

	// Disk
	disk, err := readDisk("/")
	if err == nil {
		m.DiskPercent = disk
	}

	// Load average
	load, err := readLoadAvg()
	if err == nil {
		m.LoadAvg = load
	}

	// Connections
	conns, err := readConnections()
	if err == nil {
		m.Connections = conns
	}

	// Uptime
	uptime, err := readUptime()
	if err == nil {
		m.UptimeSeconds = uptime
	}

	return m, nil
}

// --- CPU ---

type cpuSample struct {
	idle  uint64
	total uint64
}

func readCPUSample() (*cpuSample, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				return nil, fmt.Errorf("unexpected /proc/stat format")
			}

			values := make([]uint64, 0, len(fields)-1)
			for i := 1; i < len(fields); i++ {
				v, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("unexpected /proc/stat value %q", fields[i])
				}
				values = append(values, v)
			}

			var total uint64
			for i, value := range values {
				// guest and guest_nice are already included in user/nice.
				if i == 8 || i == 9 {
					continue
				}
				total += value
			}
			idle := values[3]
			if len(values) > 4 {
				idle += values[4] // iowait is idle time for utilization purposes.
			}
			return &cpuSample{idle: idle, total: total}, nil
		}
	}
	return nil, fmt.Errorf("/proc/stat: no cpu line found")
}

func calculateCPUPercent(s1, s2 *cpuSample) float64 {
	if s2.total < s1.total || s2.idle < s1.idle {
		return 0
	}
	totalDelta := float64(s2.total - s1.total)
	if totalDelta == 0 {
		return 0
	}
	idleDelta := float64(s2.idle - s1.idle)
	usage := (1.0 - idleDelta/totalDelta) * 100.0
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

func median3(a, b, c float64) float64 {
	if a > b {
		a, b = b, a
	}
	if b > c {
		b, c = c, b
	}
	if a > b {
		a, b = b, a
	}
	return b
}

// --- Memory ---

func readMemory() (float64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, err
	}

	var total, available uint64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			total = parseMemValue(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			available = parseMemValue(line)
		}
	}

	if total == 0 {
		return 0, fmt.Errorf("could not parse MemTotal")
	}
	return float64(total-available) / float64(total) * 100.0, nil
}

func parseMemValue(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseUint(fields[1], 10, 64)
	return v
}

// --- Disk ---

func readDisk(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	if total == 0 {
		return 0, nil
	}
	return float64(total-free) / float64(total) * 100.0, nil
}

// --- Network ---

type netSample struct {
	rxBytes uint64
	txBytes uint64
}

func readNetworkBytes() (*netSample, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, err
	}

	var rx, tx uint64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		// Skip header lines and loopback
		if !strings.Contains(line, ":") || strings.HasPrefix(line, "lo:") {
			continue
		}

		// Split on ":" then parse fields
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}

		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += rxBytes
		tx += txBytes
	}

	return &netSample{rxBytes: rx, txBytes: tx}, nil
}

// --- Load Average ---

func readLoadAvg() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/loadavg format")
	}

	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return load, nil
}

// --- Connections ---

func readConnections() (int, error) {
	data, err := os.ReadFile("/proc/net/sockstat")
	if err != nil {
		return 0, err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "TCP:") {
			fields := strings.Fields(line)
			// Format: "TCP: inuse X orphan Y tw Z ..."
			for i, f := range fields {
				if f == "inuse" && i+1 < len(fields) {
					v, err := strconv.Atoi(fields[i+1])
					if err != nil {
						return 0, err
					}
					return v, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("could not parse TCP inuse from /proc/net/sockstat")
}

// --- Uptime ---

func readUptime() (int64, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/uptime format")
	}

	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return int64(uptime), nil
}
