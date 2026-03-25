package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/starsdaisuki/starnexus/agent/internal/collector"
	"github.com/starsdaisuki/starnexus/agent/internal/config"
	"github.com/starsdaisuki/starnexus/agent/internal/reporter"
)

func main() {
	// Determine config path
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("StarNexus agent starting: node=%s server=%s interval=%ds",
		cfg.NodeID, cfg.ServerURL, cfg.ReportIntervalSeconds)

	rep := reporter.New(cfg.ServerURL, cfg.APIToken)

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(cfg.ReportIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// Collect and report immediately on start
	collectAndReport(cfg, rep)

	for {
		select {
		case <-ticker.C:
			collectAndReport(cfg, rep)
		case <-stop:
			log.Println("Shutting down agent")
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
		Latitude:  cfg.Latitude,
		Longitude: cfg.Longitude,
		Metrics:   *metrics,
	}

	rep.Send(report)
}
