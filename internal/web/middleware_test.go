package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"info-bot-go/internal/config"
	"info-bot-go/internal/ratelimiter"
)

// ТЗ №4, D1: сверх нормы — 429 с заголовком Retry-After.
func TestRateLimitMiddlewareReturns429(t *testing.T) {
	s := newTestServer()
	lim := ratelimiter.NewKeyLimiter(2, time.Minute)
	mw := s.rateLimitMiddleware(lim, "pub")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := mw(next)

	// первые 2 запроса проходят
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/rating", nil)
		req.RemoteAddr = "203.0.113.10:44444"
		h(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("запрос %d: %d, ожидаем 200", i+1, rec.Code)
		}
	}

	// 3-й — блок
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	req.RemoteAddr = "203.0.113.10:44444"
	h(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3-й запрос: %d, ожидаем 429", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatal("429 должен содержать Retry-After")
	}

	// другой IP — проходит (лимит персональный)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	req.RemoteAddr = "198.51.100.7:55555"
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("запрос с другого IP: %d, ожидаем 200", rec.Code)
	}
}

// ТЗ №4, D1: реальный адрес клиента берётся из X-Forwarded-For (Caddy).
func TestClientIPFromXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.99, 10.0.0.2")
	if got := clientIP(req); got != "203.0.113.99" {
		t.Fatalf("clientIP=%s, ожидаем первый адрес из XFF: 203.0.113.99", got)
	}

	// без XFF — RemoteAddr без порта
	req2 := httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	req2.RemoteAddr = "192.0.2.5:9999"
	if got := clientIP(req2); got != "192.0.2.5" {
		t.Fatalf("clientIP=%s, ожидаем 192.0.2.5", got)
	}
}

// ТЗ №4, D2: CORS allowlist — свой источник проходит, чужой — нет,
// запрос без Origin (нативные клиенты Telegram, curl) проходит всегда.
func TestCORSAllowlist(t *testing.T) {
	s := &Server{cfg: &config.Config{
		BotToken:           "test-token",
		MiniAppURL:         "https://mini.example.com/app",
		CORSAllowedOrigins: "",
	}}

	pass := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := s.corsMiddleware(pass)

	// 1) разрешённый источник — зеркалим Origin
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	req.Header.Set("Origin", "https://mini.example.com")
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("свой Origin: %d, ожидаем 200", rec.Code)
	}
	if ao := rec.Header().Get("Access-Control-Allow-Origin"); ao != "https://mini.example.com" {
		t.Fatalf("Access-Control-Allow-Origin=%q, ожидаем зеркало источника", ao)
	}

	// 2) чужой сайт — отказ
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	req.Header.Set("Origin", "https://evil.example.net")
	h(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("чужой Origin: %d, ожидаем 403", rec.Code)
	}

	// 3) без Origin (нативный клиент / прямой заход) — проходит
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("без Origin: %d, ожидаем 200", rec.Code)
	}

	// 4) web.telegram.org в списке по умолчанию
	for _, origin := range s.cfg.CORSAllowlist() {
		if origin == "https://web.telegram.org" {
			return // найден — ок
		}
	}
	t.Fatal("web.telegram.org должен быть в allowlist по умолчанию")
}

// ТЗ №4, D2: переопределение списка из .env (CORS_ALLOWED_ORIGINS).
func TestCORSAllowlistFromEnv(t *testing.T) {
	s := &Server{cfg: &config.Config{
		BotToken:           "test-token",
		CORSAllowedOrigins: "https://custom.example.org, https://t.me",
	}}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := s.corsMiddleware(next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	req.Header.Set("Origin", "https://custom.example.org")
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Origin из CORS_ALLOWED_ORIGINS: %d, ожидаем 200", rec.Code)
	}

	// дефолтный mini.example.com больше не в списке — список заменён целиком
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/rating", nil)
	req.Header.Set("Origin", "https://mini.example.com")
	h(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("Origin вне кастомного списка: %d, ожидаем 403", rec.Code)
	}
}
