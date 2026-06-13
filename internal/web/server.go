package web

import (
        "embed"
        "io/fs"
        "log"
        "net/http"

        "info-bot-go/internal/ai"
        "info-bot-go/internal/config"
        "info-bot-go/internal/directory"
        "info-bot-go/internal/sentlog"
        "info-bot-go/internal/session"
)

//go:embed static/*
var staticFiles embed.FS

// Server is the HTTP server for the Mini App and API.
type Server struct {
        cfg       *config.Config
        sessions  *session.FileStore
        sentLog   *sentlog.SentLog
        gemini    *ai.Rotator
        directory *directory.Directory
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
                cfg:       cfg,
                sessions:  sessions,
                sentLog:   sentLog,
                gemini:    gemini,
                directory: dir,
        }
}

// Start starts the HTTP server on the given address (e.g. ":8080").
func (s *Server) Start(addr string) error {
        mux := http.NewServeMux()

        // API routes
        mux.HandleFunc("/api/me", corsMiddleware(s.authMiddleware(s.handleMe)))
        mux.HandleFunc("/api/requests", corsMiddleware(s.authMiddleware(s.handleRequests)))
        mux.HandleFunc("/api/templates", corsMiddleware(s.handleTemplates))
        mux.HandleFunc("/api/directory", corsMiddleware(s.handleDirectory))
        mux.HandleFunc("/api/stats", corsMiddleware(s.handleStats))
        mux.HandleFunc("/api/generate-template", corsMiddleware(s.authMiddleware(s.handleGenerateTemplate)))

        // Static files (mini-app HTML)
        staticFS, err := fs.Sub(staticFiles, "static")
        if err != nil {
                log.Printf("[WEB] Warning: static files not found: %v", err)
        } else {
                fileServer := http.FileServer(http.FS(staticFS))
                mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                        w.Header().Set("Access-Control-Allow-Origin", "*")
                        fileServer.ServeHTTP(w, r)
                }))
        }

        log.Printf("[WEB] Starting HTTP server on %s", addr)

        handler := loggingMiddleware(mux)
        return http.ListenAndServe(addr, handler)
}

// corsMiddleware adds CORS headers for development.
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Access-Control-Allow-Origin", "*")
                w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Init-Data, X-User-ID, Authorization")

                if r.Method == "OPTIONS" {
                        w.WriteHeader(http.StatusOK)
                        return
                }

                next(w, r)
        }
}

// loggingMiddleware logs all HTTP requests.
func loggingMiddleware(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                log.Printf("[WEB] %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
                next.ServeHTTP(w, r)
        })
}

