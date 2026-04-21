package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/starsdaisuki/starnexus/bot/internal/config"
	"github.com/starsdaisuki/starnexus/bot/internal/monitor"
	"github.com/starsdaisuki/starnexus/bot/internal/telegram"
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

	bot := telegram.NewBot(cfg.TelegramToken, cfg.ChatIDs)
	mon := monitor.New(cfg.ServerURL, cfg.APIToken, bot)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})

	// Status polling goroutine
	go func() {
		mon.StartPolling(cfg.PollIntervalSeconds, done)
	}()

	// Reverse heartbeat goroutine
	go func() {
		mon.StartHeartbeat(cfg.HeartbeatIntervalSeconds, done)
	}()

	// Daily summary dispatcher. Per-chat delivery can be toggled with /daily on|off.
	go func() {
		mon.StartDailySummary(3600, done)
	}()

	// Command listener goroutine
	go func() {
		bot.PollCommands(mon.HandleCommand, done)
	}()

	log.Printf("StarNexus bot started (poll: %ds, heartbeat: %ds)",
		cfg.PollIntervalSeconds, cfg.HeartbeatIntervalSeconds)

	// Send startup message
	if err := bot.SendMessage("\xf0\x9f\x9f\xa2 StarNexus Bot started."); err != nil {
		log.Printf("Failed to send startup message: %v", err)
	}

	<-stop
	log.Println("Shutting down bot")
	close(done)
}
