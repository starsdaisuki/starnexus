package main

import (
	"log"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/starsdaisuki/starnexus/agent/internal/collector"
	"github.com/starsdaisuki/starnexus/agent/internal/config"
	"github.com/starsdaisuki/starnexus/agent/internal/geoip"
	"github.com/starsdaisuki/starnexus/agent/internal/probe"
	"github.com/starsdaisuki/starnexus/agent/internal/reporter"
)

func main() {
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Auto-detect geolocation if lat/lng not set
	if cfg.Latitude == 0 && cfg.Longitude == 0 {
		log.Println("Latitude/longitude not set, auto-detecting via ip-api.com...")
		geo, err := geoip.Detect()
		if err != nil {
			log.Printf("Geolocation auto-detect failed: %v (set manually in config)", err)
		} else {
			cfg.Latitude = geo.Latitude
			cfg.Longitude = geo.Longitude
			if cfg.PublicIP == "" {
				cfg.PublicIP = geo.PublicIP
			}
			log.Printf("Geolocation: %s, %s (%.4f, %.4f) IP=%s",
				geo.City, geo.Country, geo.Latitude, geo.Longitude, geo.PublicIP)
		}
	}

	log.Printf("StarNexus agent starting: node=%s server=%s interval=%ds probes=%d",
		cfg.NodeID, cfg.ServerURL, cfg.ReportIntervalSeconds, len(cfg.ProbeTargets))

	rep := reporter.New(cfg.ServerURL, cfg.APIToken)

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})

	// Metrics report loop (30s)
	metricsTicker := time.NewTicker(time.Duration(cfg.ReportIntervalSeconds) * time.Second)
	defer metricsTicker.Stop()
	collectAndReport(cfg, rep)

	// Connection collector (5s) — only if GeoIP DB exists
	var connCollector *collector.ConnCollector
	if _, err := os.Stat(cfg.GeoIPDBPath); err == nil {
		connCollector = collector.NewConnCollector(cfg.GeoIPDBPath, cfg.PortLabels, cfg.ProxyProcesses, cfg.ConnIntervalSeconds)
		defer connCollector.Close()
		go connCollector.StartPortScanner(done)

		connTicker := time.NewTicker(time.Duration(cfg.ConnIntervalSeconds) * time.Second)
		defer connTicker.Stop()

		go func() {
			for {
				select {
				case <-connTicker.C:
					conns := connCollector.Collect()
					if len(conns) > 0 {
						// Sort by rate descending, keep top 20
						sort.Slice(conns, func(i, j int) bool {
							return conns[i].RateDown+conns[i].RateUp > conns[j].RateDown+conns[j].RateUp
						})
						if len(conns) > 20 {
							conns = conns[:20]
						}
						rep.SendConnections(reporter.ConnReport{
							NodeID:      cfg.NodeID,
							Connections: conns,
						})
					}
				case <-done:
					return
				}
			}
		}()
		log.Printf("Connection tracking enabled (interval: %ds, geo: %s)", cfg.ConnIntervalSeconds, cfg.GeoIPDBPath)
	} else {
		log.Printf("GeoIP DB not found at %s — connection tracking disabled", cfg.GeoIPDBPath)
	}

	for {
		select {
		case <-metricsTicker.C:
			collectAndReport(cfg, rep)
		case <-stop:
			log.Println("Shutting down agent")
			close(done)
			return
		}
	}
}

func collectAndReport(cfg *config.Config, rep *reporter.Reporter) {
	metrics, err := collector.Collect()
	if err != nil {
		log.Printf("Collection failed: %v", err)
		return
	}

	report := reporter.Report{
		NodeID:    cfg.NodeID,
		Name:      cfg.NodeName,
		Provider:  cfg.Provider,
		PublicIP:  cfg.PublicIP,
		Latitude:  cfg.Latitude,
		Longitude: cfg.Longitude,
		Metrics:   *metrics,
	}

	if len(cfg.ProbeTargets) > 0 {
		report.Links = probe.ProbeAll(cfg.ProbeTargets)
	}

	rep.Send(report)
}
