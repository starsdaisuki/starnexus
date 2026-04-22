// starnexus-loadtest simulates N fake agents hammering /api/report to
// measure server write throughput and API latency at various fleet
// sizes. Used for the scalability section in RESULTS.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type report struct {
	CollectedAt    int64   `json:"collected_at"`
	NodeID         string  `json:"node_id"`
	Name           string  `json:"name"`
	Provider       string  `json:"provider"`
	PublicIP       string  `json:"public_ip"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	LocationSource string  `json:"location_source"`
	Metrics        metrics `json:"metrics"`
}

type metrics struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	DiskPercent   float64 `json:"disk_percent"`
	BandwidthUp   float64 `json:"bandwidth_up"`
	BandwidthDown float64 `json:"bandwidth_down"`
	LoadAvg       float64 `json:"load_avg"`
	Connections   int     `json:"connections"`
	UptimeSeconds int64   `json:"uptime_seconds"`
}

func main() {
	var serverURL string
	var token string
	var agents int
	var duration time.Duration
	var interval time.Duration
	var outPath string

	flag.StringVar(&serverURL, "server", "http://127.0.0.1:8900", "StarNexus server URL")
	flag.StringVar(&token, "token", "", "Bearer token (required)")
	flag.IntVar(&agents, "agents", 100, "Number of virtual agents")
	flag.DurationVar(&duration, "duration", 60*time.Second, "Test duration")
	flag.DurationVar(&interval, "interval", 250*time.Millisecond, "Per-agent report interval")
	flag.StringVar(&outPath, "out", "", "Optional JSON output path for machine-readable results")
	flag.Parse()

	if token == "" {
		log.Fatal("--token is required")
	}
	if agents <= 0 {
		log.Fatal("--agents must be positive")
	}

	log.Printf("Load test: %d agents × %s interval for %s against %s", agents, interval, duration, serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), duration+5*time.Second)
	defer cancel()

	latencies := make(chan time.Duration, agents*64)
	errors := make(chan error, agents*8)
	var totalRequests atomic.Uint64
	var successRequests atomic.Uint64
	var nonOkStatus atomic.Uint64
	var transportFailures atomic.Uint64

	transport := &http.Transport{
		MaxIdleConns:        agents * 2,
		MaxIdleConnsPerHost: agents * 2,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}

	deadline := time.Now().Add(duration)

	var wg sync.WaitGroup
	wg.Add(agents)
	start := time.Now()

	for i := 0; i < agents; i++ {
		go func(idx int) {
			defer wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			rng := rand.New(rand.NewPCG(uint64(idx+1), 7))
			for {
				if time.Now().After(deadline) {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					body, _ := json.Marshal(report{
						CollectedAt:    time.Now().Unix(),
						NodeID:         fmt.Sprintf("loadtest-%04d", idx),
						Name:           fmt.Sprintf("Load Test %d", idx),
						Provider:       "loadtest",
						PublicIP:       fmt.Sprintf("10.0.%d.%d", idx/256, idx%256),
						Latitude:       35.0,
						Longitude:      139.0,
						LocationSource: "manual",
						Metrics: metrics{
							CPUPercent:    rng.Float64() * 100,
							MemoryPercent: rng.Float64() * 100,
							DiskPercent:   rng.Float64() * 100,
							BandwidthUp:   rng.Float64() * 500,
							BandwidthDown: rng.Float64() * 2000,
							LoadAvg:       rng.Float64() * 4,
							Connections:   int(rng.Float64() * 200),
							UptimeSeconds: int64(rng.Float64() * 1_000_000),
						},
					})
					reqStart := time.Now()
					req, _ := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/report", bytes.NewReader(body))
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("Content-Type", "application/json")
					resp, err := client.Do(req)
					latency := time.Since(reqStart)
					totalRequests.Add(1)
					if err != nil {
						transportFailures.Add(1)
						select {
						case errors <- err:
						default:
						}
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						nonOkStatus.Add(1)
						select {
						case errors <- fmt.Errorf("status %d", resp.StatusCode):
						default:
						}
						continue
					}
					successRequests.Add(1)
					select {
					case latencies <- latency:
					default:
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(latencies)
	close(errors)
	elapsed := time.Since(start)

	durations := make([]time.Duration, 0, 4096)
	for latency := range latencies {
		durations = append(durations, latency)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	errorSamples := make([]string, 0, 5)
	for err := range errors {
		if len(errorSamples) < 5 {
			errorSamples = append(errorSamples, err.Error())
		}
	}

	total := totalRequests.Load()
	success := successRequests.Load()
	errorCount := int(total - success)
	rps := float64(total) / elapsed.Seconds()

	p50 := percentile(durations, 0.50)
	p95 := percentile(durations, 0.95)
	p99 := percentile(durations, 0.99)

	summary := map[string]any{
		"agents":           agents,
		"interval_ms":      interval.Milliseconds(),
		"duration_seconds": elapsed.Seconds(),
		"total_requests":   total,
		"success_requests": success,
		"error_count":      errorCount,
		"requests_per_sec": rps,
		"latency_p50_ms":   p50.Milliseconds(),
		"latency_p95_ms":   p95.Milliseconds(),
		"latency_p99_ms":   p99.Milliseconds(),
	}
	fmt.Printf("\nAgents=%d interval=%s elapsed=%s\n", agents, interval, elapsed.Truncate(time.Millisecond))
	fmt.Printf("Requests: total=%d success=%d errors=%d (transport=%d non200=%d) rps=%.1f\n",
		total, success, errorCount, transportFailures.Load(), nonOkStatus.Load(), rps)
	fmt.Printf("Latency (ms): p50=%d p95=%d p99=%d\n", p50.Milliseconds(), p95.Milliseconds(), p99.Milliseconds())
	if len(errorSamples) > 0 {
		fmt.Printf("Error samples:\n")
		for _, sample := range errorSamples {
			fmt.Printf("  - %s\n", sample)
		}
	}

	if outPath != "" {
		file, err := os.Create(outPath)
		if err != nil {
			log.Fatalf("create %s: %v", outPath, err)
		}
		defer file.Close()
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(summary); err != nil {
			log.Fatalf("encode summary: %v", err)
		}
		fmt.Printf("Summary written to %s\n", outPath)
	}

	if success == 0 {
		os.Exit(1)
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}
