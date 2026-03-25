package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/starsdaisuki/starnexus/server/internal/alert"
	"github.com/starsdaisuki/starnexus/server/internal/api"
	"github.com/starsdaisuki/starnexus/server/internal/config"
	"github.com/starsdaisuki/starnexus/server/internal/db"
)

func main() {
	// Determine config path
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}

	// Load config
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Find schema.sql relative to the binary or working directory
	schemaPath := "schema.sql"
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		// Try next to the binary
		exe, _ := os.Executable()
		schemaPath = filepath.Join(filepath.Dir(exe), "schema.sql")
	}

	// Open database
	database, err := db.Open(cfg.DBPath, schemaPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Start offline monitor
	monitor := alert.NewMonitor(database, cfg.OfflineThresholdSeconds)
	monitor.Start()
	defer monitor.Stop()

	// Start HTTP server
	server := api.New(database, cfg.APIToken, cfg.WebDir)
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("StarNexus server starting on %s", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
