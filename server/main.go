package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/starsdaisuki/starnexus/server/internal/alert"
	"github.com/starsdaisuki/starnexus/server/internal/analytics"
	"github.com/starsdaisuki/starnexus/server/internal/api"
	"github.com/starsdaisuki/starnexus/server/internal/buildinfo"
	"github.com/starsdaisuki/starnexus/server/internal/config"
	"github.com/starsdaisuki/starnexus/server/internal/db"
	"github.com/starsdaisuki/starnexus/server/internal/locations"
)

func main() {
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		if os.Args[1] == "--version" || os.Args[1] == "version" {
			fmt.Println(buildinfo.Current("starnexus-server").String())
			return
		}
		if os.Args[1] == "--check-config" || os.Args[1] == "check-config" {
			if len(os.Args) > 2 {
				cfgPath = os.Args[2]
			}
			if _, err := config.Load(cfgPath); err != nil {
				log.Fatalf("Config check failed: %v", err)
			}
			fmt.Printf("Config OK: %s\n", cfgPath)
			return
		}
		cfgPath = os.Args[1]
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("Failed to load config: %v\nCreate one from config.yaml.example, or set STARNEXUS_API_TOKEN (plus optional STARNEXUS_PORT / STARNEXUS_DB_PATH) to run without a config file.", err)
		}
		log.Fatalf("Failed to load config: %v", err)
	}

	database, err := db.OpenWithSchema(cfg.DBPath, resolveSchema())
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	nodeLocations, err := locations.Load(cfg.NodeLocationsPath)
	if err != nil {
		log.Fatalf("Failed to load node location overrides: %v", err)
	}
	if overrides := nodeLocations.DBOverrides(); len(overrides) > 0 {
		if err := database.ApplyLocationOverrides(overrides); err != nil {
			log.Fatalf("Failed to apply node location overrides: %v", err)
		}
		log.Printf("Loaded %d node location override(s)", len(overrides))
	}

	// Offline monitor
	monitor := alert.NewMonitor(database, cfg.OfflineThresholdSeconds)
	monitor.Start()
	defer monitor.Stop()

	// Analytics scheduler — alerts forwarded to Telegram bot if configured
	alertFn := buildAlertFunc(cfg)
	scheduler := analytics.NewScheduler(database, alertFn, cfg.MistralAPIKey)
	scheduler.Start()
	defer scheduler.Stop()

	// HTTP server
	server := api.New(database, cfg.APIToken, resolveWebDir(cfg.WebDir), cfg.AgentBinaryPath, cfg.GeoIPDBPath, cfg.ExperimentLabelsPath, nodeLocations)
	server.SetReportGenerator(scheduler)
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("StarNexus server starting on %s", addr)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

//go:embed schema.sql
var embeddedSchema string

// resolveSchema prefers an on-disk schema.sql (next to the working
// directory or the binary) so local hotfixes still work, and falls
// back to the schema embedded at build time so a deployed binary has
// no file dependency.
func resolveSchema() string {
	for _, candidate := range []string{"schema.sql", filepath.Join(executableDir(), "schema.sql")} {
		if data, err := os.ReadFile(candidate); err == nil {
			return string(data)
		}
	}
	return embeddedSchema
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// resolveWebDir returns a directory to serve the frontend from, or ""
// to serve the copy embedded in the binary. An explicit web_dir that
// does not exist is a config mistake worth surfacing rather than
// silently masking with the embedded copy.
func resolveWebDir(configured string) string {
	if configured != "" {
		if info, err := os.Stat(configured); err == nil && info.IsDir() {
			return configured
		}
		log.Printf("Configured web_dir %q not found — serving the embedded frontend instead", configured)
		return ""
	}
	for _, candidate := range []string{"../web/public", "./web"} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// buildAlertFunc creates an alert function that sends messages to the Telegram bot
// via its sendMessage endpoint, if bot_token and bot_chat_ids are configured.
func buildAlertFunc(cfg *config.Config) analytics.AlertFunc {
	if cfg.BotToken == "" || len(cfg.BotChatIDs) == 0 {
		log.Println("Bot alerting not configured (set bot_token + bot_chat_ids in config)")
		return nil
	}

	apiBase := "https://api.telegram.org/bot" + cfg.BotToken
	client := &http.Client{Timeout: 10 * time.Second}

	return func(message string) {
		for _, chatID := range cfg.BotChatIDs {
			params := url.Values{
				"chat_id":    {strconv.FormatInt(chatID, 10)},
				"text":       {message},
				"parse_mode": {"HTML"},
			}
			resp, err := client.PostForm(apiBase+"/sendMessage", params)
			if err != nil {
				log.Printf("Analytics alert send failed: %v", err)
				continue
			}
			resp.Body.Close()
		}
	}
}
