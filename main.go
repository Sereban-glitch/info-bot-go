package main

import (
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"info-bot-go/internal/bot"
	"info-bot-go/internal/config"
	"info-bot-go/internal/directory"
	"info-bot-go/internal/imap"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
	"info-bot-go/internal/stats"
	"info-bot-go/internal/web"
)

func main() {
	// Load .env file from multiple possible locations
	paths := []string{
		".env",
		filepath.Join("..", ".env"),
		filepath.Join(os.Getenv("HOME"), "info-bot-go", ".env"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			_ = godotenv.Load(p)
			break
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config load: %v", err)
	}

	// Initialize directory
	dir := directory.All()

	// Initialize session storage
	sessDir := cfg.SessionDir
	if sessDir == "" {
		sessDir = ".sessions_go"
	}
	dir.LoadLearned(sessDir)
	sessStore, err := session.NewFileStore(sessDir)
	if err != nil {
		log.Fatalf("session store init: %v", err)
	}
	defer sessStore.Close()

	// Initialize sent log
	sentLog, err := sentlog.New(sessDir)
	if err != nil {
		log.Fatalf("sent log init: %v", err)
	}
	defer sentLog.Close()

	// Initialize global stats
	globalStats, err := stats.New(sessDir)
	if err != nil {
		log.Fatalf("stats init: %v", err)
	}

	// Initialize IMAP watcher
	var watcher *imap.Watcher
	if cfg.GmailUser != "" && cfg.GmailAppPassword != "" {
		watcher = imap.NewWatcher(cfg.IMAPHost, cfg.IMAPPort, cfg.GmailUser, cfg.GmailAppPassword, cfg.IMAPPollMinutes)
		watcher.SetSentLog(sentLog)
		watcher.SetStats(globalStats)
	}

	// Initialize and start the bot
	b, err := bot.New(cfg, sessStore, sentLog, globalStats, watcher)
	if err != nil {
		log.Fatalf("bot init: %v", err)
	}

	// Start HTTP server for Mini App and API
	webServer := web.NewServer(cfg, sessStore, sentLog, b.Rotator(), dir)
	go webServer.Start(":8081")

	// Start IMAP watcher if configured
	if watcher != nil {
		go func() {
			time.Sleep(10 * time.Second)
			watcher.Start(b.Telebot())
		}()
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("Shutting down...")
		if watcher != nil {
			watcher.Stop()
		}
		b.Stop()
		os.Exit(0)
	}()

	log.Println("Info-Bot-Go starting...")
	b.Start()
}
