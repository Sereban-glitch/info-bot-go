package web

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"time"

	"info-bot-go/internal/ai"
	"info-bot-go/internal/config"
	"info-bot-go/internal/directory"
	"info-bot-go/internal/dostup"
	"info-bot-go/internal/ratelimiter"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
	"info-bot-go/internal/stars"
)

//go:embed static/*
var staticFiles embed.FS

// Server is the HTTP server for the Mini App and API.
type Server struct {
	cfg          *config.Config
	sessions     *session.FileStore
	sentLog      *sentlog.SentLog
	gemini       *ai.Rotator
	directory    *directory.Directory
	dostup       *dostup.Client              // канал портала (может быть nil)
	catalog      *dostup.CatalogStore        // каталог органов (может быть nil)
	ratings      *dostup.RatingsStore        // рейтинги органов (может быть nil)
	limits       *apiLimiters                // нормы частоты /api/* (ТЗ №4, D1)
	stars        *stars.Store                // балансы кредитов монетизации (nil = бесплатно)
	starsClient  *stars.Client               // создание инвойс-ссылок Bot API
	analyzeUsers *ratelimiter.KeyRateLimiter // часовой лимит бесплатных розборов (6/час на пользователя)
}

// NewServer creates a new web server.
func NewServer(
	cfg *config.Config,
	sessions *session.FileStore,
	sentLog *sentlog.SentLog,
	gemini *ai.Rotator,
	dir *directory.Directory,
) *Server {
	return &Server{
		cfg:          cfg,
		sessions:     sessions,
		sentLog:      sentLog,
		gemini:       gemini,
		directory:    dir,
		limits:       newAPILimiters(cfg.APIRateLimitPublic, cfg.APIRateLimitAuth, cfg.APIRateLimitGenerate, cfg.APIRateLimitAnalyze),
		analyzeUsers: ratelimiter.NewKeyLimiter(6, time.Hour), // как в боте: 6 бесплатных розборов в час
	}
}

// SetDostup подключает канал портала (рейтинги органов + поиск по запитам).
func (s *Server) SetDostup(client *dostup.Client, catalog *dostup.CatalogStore, ratings *dostup.RatingsStore) {
	s.dostup = client
	s.catalog = catalog
	s.ratings = ratings
}

// SetStars подключает монетизацию: хранилище кредитов и клиент Bot API.
// nil — монетизация выключена (розборы бесплатны, эндпоинт /api/stars/*
// честно сообщает enabled=false/403).
func (s *Server) SetStars(store *stars.Store) {
	s.stars = store
	if store != nil && s.cfg != nil && s.cfg.BotToken != "" {
		s.starsClient = stars.NewClient(s.cfg.BotToken)
	}
}

// Start starts the HTTP server on the given address (e.g. ":8080").
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()

	// Порядок обёрток (снаружи внутрь): CORS → rate limit → подпись → обработчик.
	// Лимит стоит ДО проверки подписи: HMAC-подпись тоже стоит вычислений,
	// и флуд с одного IP отсекается раньше дорогостоящей работы.
	pub := s.rateLimitMiddleware(s.limits.public, "pub")
	aut := s.rateLimitMiddleware(s.limits.auth, "auth")
	gen := s.rateLimitMiddleware(s.limits.generate, "gen")
	anl := s.rateLimitMiddleware(s.limits.analyze, "anl")

	// API routes
	mux.HandleFunc("/api/me", s.corsMiddleware(aut(s.authMiddleware(s.handleMe))))
	mux.HandleFunc("/api/requests", s.corsMiddleware(aut(s.authMiddleware(s.handleRequests))))
	mux.HandleFunc("/api/templates", s.corsMiddleware(aut(s.authMiddleware(s.handleTemplates))))
	mux.HandleFunc("/api/directory", s.corsMiddleware(aut(s.authMiddleware(s.handleDirectory))))
	mux.HandleFunc("/api/stats", s.corsMiddleware(aut(s.authMiddleware(s.handleStats))))
	mux.HandleFunc("/api/generate-template", s.corsMiddleware(gen(s.authMiddleware(s.handleGenerateTemplate))))
	mux.HandleFunc("/api/rating", s.corsMiddleware(pub(s.handleRating))) // публичный: агрегаты без ПД
	mux.HandleFunc("/api/body-stats", s.corsMiddleware(aut(s.authMiddleware(s.handleBodyStats))))
	mux.HandleFunc("/api/search-requests", s.corsMiddleware(aut(s.authMiddleware(s.handleSearchRequests))))
	// ТЗ №5: удаление персональных данных — только POST с подписью Telegram;
	// лимит «дорогих» действий (gen) — 6/мин, чтобы нельзя было долбить
	mux.HandleFunc("/api/delete-my-data", s.corsMiddleware(gen(s.authMiddleware(s.handleDeleteMyData))))

	// Вкладка «Розбір»: AI-анализ ответа органа (дорогая операция — свой
	// лимит anl 6/мин на IP + часовой лимит 6/час на пользователя внутри).
	mux.HandleFunc("/api/analyze", s.corsMiddleware(anl(s.authMiddleware(s.handleAnalyze))))

	// Монетизация Stars (каркас): статус/баланс + создание инвойс-ссылки.
	mux.HandleFunc("/api/stars/status", s.corsMiddleware(aut(s.authMiddleware(s.handleStarsStatus))))
	mux.HandleFunc("/api/stars/invoice", s.corsMiddleware(anl(s.authMiddleware(s.handleStarsInvoice))))

	// Static files (mini-app HTML)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Printf("[WEB] Warning: static files not found: %v", err)
	} else {
		fileServer := http.FileServer(http.FS(staticFS))
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fileServer.ServeHTTP(w, r)
		}))
	}

	log.Printf("[WEB] Starting HTTP server on %s (rate limits: public=%d/min, auth bucket=%d/min, generate=%d/min)",
		addr, s.cfg.APIRateLimitPublic, s.cfg.APIRateLimitAuth, s.cfg.APIRateLimitGenerate)

	handler := loggingMiddleware(mux)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second, // щедро: генерация шаблона с ИИ может занять десятки секунд
		IdleTimeout:  120 * time.Second,
	}
	return srv.ListenAndServe()
}

// loggingMiddleware logs all HTTP requests.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[WEB] %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
