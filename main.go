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
        "info-bot-go/internal/safego"
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

        // Start HTTP server for Mini App and API.
        // ТЗ №4, D4: ошибка веб-сервера больше не глотается — падение видно
        // в журнале строкой [FATAL] web, процесс завершает работу с кодом 1,
        // и systemd (Restart=always) поднимает его заново.
        webServer := web.NewServer(cfg, sessStore, sentLog, b.Rotator(), dir)
        // Канал портала: рейтинги органов + поиск по публичным запросам
        webServer.SetDostup(b.Dostup(), b.DostupCatalog(), b.DostupRatings())
        webErr := make(chan error, 1)
        go func() {
                if err := webServer.Start(":8081"); err != nil {
                        log.Printf("[FATAL] web: %v", err)
                        select {
                        case webErr <- err:
                        default:
                        }
                }
        }()

        // Start IMAP watcher if configured
        if watcher != nil {
                safego.Go("imap-watcher", func() {
                        time.Sleep(10 * time.Second)
                        watcher.Start(b.Telebot())
                })
        }

        // Graceful shutdown.
        // ВАЖНО: os.Exit из сигнальной горутины больше не вызывается —
        // он обрывал deferred-вызовы (sentLog.Close → flush) в произвольный
        // момент, из-за чего журнал отправок мог остаться усечённым.
        quit := make(chan os.Signal, 1)
        signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

        done := make(chan struct{})
        exitCode := make(chan int, 1) // 1 = упал веб-сервер (systemd перезапустит)
        go func() {
                select {
                case <-quit:
                        log.Println("Shutting down...")
                case err := <-webErr:
                        // Веб-сервер упал: штатное завершение со сохранением журналов,
                        // затем код 1 — systemd посчитает это сбоем и перезапустит сервис.
                        log.Printf("[FATAL] веб-сервер зупинився: %v — ініціюю перезапуск", err)
                        exitCode <- 1
                }
                if watcher != nil {
                        watcher.Stop()
                }
                b.Stop() // разблокирует Start() в главном потоке
                select {
                case <-done:
                case <-time.After(5 * time.Second):
                        log.Println("shutdown: старт не завершился за 5с — принудительный выход")
                        os.Exit(1)
                }
        }()

        log.Println("Info-Bot-Go starting...")
        b.Start()
        close(done)
        // Начинаем возвращение из main: отработают defer sessStore.Close()
        // и defer sentLog.Close() (атомарный flush), затем процесс завершится
        // с кодом 0 (штатно) или 1 (упал веб-сервер — systemd перезапустит).
        select {
        case code := <-exitCode:
                os.Exit(code)
        default:
        }
}
