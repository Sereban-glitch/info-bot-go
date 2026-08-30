package web

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"info-bot-go/internal/ratelimiter"
)

// ---------------------------------------------------------------------------
// Ограничение частоты (ТЗ №4, D1)
// ---------------------------------------------------------------------------

// apiLimiters держит три нормы: публичные эндпоинты, личные (после
// проверки подписи Telegram) и дорогая генерация шаблона с ИИ.
// Ключ корзины — IP посетителя (см. clientIP).
type apiLimiters struct {
	public   *ratelimiter.KeyRateLimiter
	auth     *ratelimiter.KeyRateLimiter
	generate *ratelimiter.KeyRateLimiter
	analyze  *ratelimiter.KeyRateLimiter // AI-розбір ответа органа
}

func newAPILimiters(public, auth, generate, analyze int) *apiLimiters {
	return &apiLimiters{
		public:   ratelimiter.NewKeyLimiter(public, time.Minute),
		auth:     ratelimiter.NewKeyLimiter(auth, time.Minute),
		generate: ratelimiter.NewKeyLimiter(generate, time.Minute),
		analyze:  ratelimiter.NewKeyLimiter(analyze, time.Minute),
	}
}

// clientIP достаёт адрес клиента. Мини-приложение сидит за обратным
// прокси (Caddy), поэтому реальный адрес приходит в X-Forwarded-For
// (первый элемент — исходный клиент); если заголовка нет — берём
// RemoteAddr как есть.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// первый адрес в списке — исходный отправитель
		if i := strings.IndexByte(xff, ','); i >= 0 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimitMiddleware возвращает функцию-обёртку: пропускает не более
// limit запросов в минуту с одного IP; сверх нормы — 429 с заголовком
// Retry-After (когда клиенту можно пробовать снова).
func (s *Server) rateLimitMiddleware(l *ratelimiter.KeyRateLimiter, bucket string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			key := bucket + "|" + clientIP(r)
			if !l.Allow(key) {
				retry := l.RetryAfter(key)
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retry))
				log.Printf("[WEB] rate limit: %s %s ip=%s retry_after=%ds", r.Method, r.URL.Path, clientIP(r), retry)
				writeJSON(w, http.StatusTooManyRequests, APIResponse{
					OK:  false,
					Err: fmt.Sprintf("занадто багато запитів, спробуйте через %d с", retry),
				})
				return
			}
			next(w, r)
		}
	}
}

// ---------------------------------------------------------------------------
// CORS — разрешённые источники (ТЗ №4, D2)
// ---------------------------------------------------------------------------

// corsMiddleware решает, можно ли отвечать браузеру с источника Origin.
// Нативные клиенты Telegram и обычные запросы без Origin пропускаются
// всегда (Origin шлют только браузеры). Браузерным страницам отвечаем
// Access-Control-Allow-Origin только для источников из allowlist
// (домен мини-приложения, web.telegram.org, t.me). Чужим сайтам заголовок
// не выдаётся — браузер сам заблокирует им чтение ответа.
//
// Попутно (аудит): из Allow-Headers убран мёртвый X-User-ID — идентификатор
// пользователя устанавливает ТОЛЬКО authMiddleware после проверки подписи.
func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if s.originAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Init-Data, Authorization")
				w.Header().Add("Vary", "Origin")
			} else {
				// чужой источник: без CORS-заголовков браузер не даст
				// прочитать ответ; сам запрос также отклоняем
				log.Printf("[WEB] CORS отказ: origin=%s path=%s", origin, r.URL.Path)
				writeJSON(w, http.StatusForbidden, APIResponse{OK: false, Err: "origin not allowed"})
				return
			}
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// originAllowed сверяет источник (без регистра) со списком разрешённых.
func (s *Server) originAllowed(origin string) bool {
	o := strings.ToLower(strings.TrimSpace(origin))
	if o == "" || o == "null" {
		return false
	}
	for _, allowed := range s.cfg.CORSAllowlist() {
		if o == allowed {
			return true
		}
	}
	return false
}
