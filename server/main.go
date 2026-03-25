package main

import (
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
	"github.com/starsdaisuki/starnexus/server/internal/config"
	"github.com/starsdaisuki/starnexus/server/internal/db"
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

	schemaPath := "schema.sql"
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		exe, _ := os.Executable()
		schemaPath = filepath.Join(filepath.Dir(exe), "schema.sql")
	}

	database, err := db.Open(cfg.DBPath, schemaPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

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
	server := api.New(database, cfg.APIToken, cfg.WebDir, cfg.AgentBinaryPath, cfg.GeoIPDBPath)
	server.SetReportGenerator(scheduler)
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("StarNexus server starting on %s", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
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
