package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"info-bot-go/internal/ai"
	"info-bot-go/internal/config"
	"info-bot-go/internal/ratelimiter"
	"info-bot-go/internal/stars"
)

// newAnalyzeTestServer — сервер с подписью и stars-хранилищем.
func newAnalyzeTestServer(t *testing.T, starsEnabled bool) *Server {
	t.Helper()
	cfg := &config.Config{BotToken: testBotToken, StarsEnabled: starsEnabled, StarsFreeCredits: 3, StarsAnalyzePrice: 25, StarsAnalyzePack: 10}
	s := &Server{cfg: cfg}
	s.stars = stars.NewStore(filepath.Join(t.TempDir(), "stars.json"))
	s.analyzeUsers = ratelimiter.NewKeyLimiter(6, time.Hour)
	return s
}

func authedAnalyzeReq(t *testing.T, s *Server, body interface{}) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/analyze", bytes.NewReader(b))
	req.Header.Set("X-Init-Data", signInitData(t, 100, s.cfg.BotToken))
	return req, httptest.NewRecorder()
}

func TestAnalyzeEndpointCheapPaths(t *testing.T) {
	s := newAnalyzeTestServer(t, false)

	// GET → 405 (проверка метода внутри хендлера, без middleware)
	req := httptest.NewRequest(http.MethodGet, "/api/analyze", nil)
	rec := httptest.NewRecorder()
	s.handleAnalyze(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d, want 405", rec.Code)
	}

	// без подписи → 401
	req = httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(`{"text":"x"}`))
	rec = httptest.NewRecorder()
	s.authMiddleware(s.handleAnalyze)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("без подписи: %d, want 401", rec.Code)
	}

	// с подписью, но без AI → 503
	req, rec = authedAnalyzeReq(t, s, AnalyzeRequest{Text: strings.Repeat("відмова ", 20)})
	s.authMiddleware(s.handleAnalyze)(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("без AI: %d, want 503", rec.Code)
	}
}

func TestAnalyzeStarsGate(t *testing.T) {
	// Монетизация ВКЛЮЧЕНА. Ротатор без ключей: AI «настроен», но каждый
	// вызов мгновенно падает без сети — идеально для проверки гейта.
	s := newAnalyzeTestServer(t, true)
	s.gemini = ai.NewRotator(nil, "test-model", "test-fallback")

	// 1) Есть welcome-кредиты: гейт пропускает, AI падает → 500,
	//    и кредит ВОЗВРАЩАЕТСЯ (баланс не должен таять из-за сбоя).
	req, rec := authedAnalyzeReq(t, s, AnalyzeRequest{Text: strings.Repeat("відмова органу не надав інформацію ", 5)})
	s.authMiddleware(s.handleAnalyze)(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("вызов с кредитами: %d, want 500 (гейт прошёл, AI упал)", rec.Code)
	}
	if s.stars.Balance(100) != 3 {
		t.Fatalf("после сбоя AI баланс = %d, want 3 (кредит должен вернуться)", s.stars.Balance(100))
	}

	// 2) Баланс слит (например, потрачен раньше) → 402 no credits.
	if !s.stars.Spend(100, 3) {
		t.Fatal("не удалось слить тестовый баланс")
	}
	req, rec = authedAnalyzeReq(t, s, AnalyzeRequest{Text: strings.Repeat("відмова органу не надав інформацію ", 5)})
	s.authMiddleware(s.handleAnalyze)(rec, req)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("пустой баланс: %d, want 402", rec.Code)
	}
	var resp APIResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Err != "no credits" {
		t.Fatalf("err = %q, want no credits", resp.Err)
	}
}

func TestAnalyzeHourlyLimitFreeMode(t *testing.T) {
	// Монетизация ВЫКЛЮЧЕНА: часовой лимит 6/час на пользователя.
	s := newAnalyzeTestServer(t, false)
	s.gemini = ai.NewRotator(nil, "test-model", "test-fallback")

	for i := 0; i < 6; i++ {
		req, rec := authedAnalyzeReq(t, s, AnalyzeRequest{Text: strings.Repeat("відмова ", 30)})
		s.authMiddleware(s.handleAnalyze)(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("вызов %d: %d, want 500 (лимит ещё не бьёт, AI падает)", i+1, rec.Code)
		}
	}
	// 7-й — лимит.
	req, rec := authedAnalyzeReq(t, s, AnalyzeRequest{Text: strings.Repeat("відмова ", 30)})
	s.authMiddleware(s.handleAnalyze)(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("7-й вызов: %d, want 429", rec.Code)
	}

	// Другой пользователь — не задет лимитом.
	req2 := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(`{"text":"`+strings.Repeat("a", 50)+`"}`))
	req2.Header.Set("X-Init-Data", signInitData(t, 200, s.cfg.BotToken))
	rec2 := httptest.NewRecorder()
	s.authMiddleware(s.handleAnalyze)(rec2, req2)
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("другой пользователь: %d, want 500", rec2.Code)
	}
}

func TestAnalyzeShortTextRejected(t *testing.T) {
	s := newAnalyzeTestServer(t, false)
	// текст < 40 символов отклоняется ещё до AI
	req, rec := authedAnalyzeReq(t, s, AnalyzeRequest{Text: "коротко"})
	s.authMiddleware(s.handleAnalyze)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("короткий текст: %d, want 400", rec.Code)
	}
}

func TestStarsStatusDisabled(t *testing.T) {
	s := newAnalyzeTestServer(t, false)

	req := httptest.NewRequest(http.MethodGet, "/api/stars/status", nil)
	req.Header.Set("X-Init-Data", signInitData(t, 100, s.cfg.BotToken))
	rec := httptest.NewRecorder()
	s.authMiddleware(s.handleStarsStatus)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp struct {
		OK   bool                `json:"ok"`
		Data StarsStatusResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.Enabled {
		t.Fatal("монетизация выключена, а status говорит enabled=true")
	}
	if resp.Data.Balance != 0 {
		t.Fatalf("баланс при выключенной: %d, want 0", resp.Data.Balance)
	}
}

func TestStarsStatusEnabledShowsWelcome(t *testing.T) {
	s := newAnalyzeTestServer(t, true)

	req := httptest.NewRequest(http.MethodGet, "/api/stars/status", nil)
	req.Header.Set("X-Init-Data", signInitData(t, 100, s.cfg.BotToken))
	rec := httptest.NewRecorder()
	s.authMiddleware(s.handleStarsStatus)(rec, req)

	var resp struct {
		OK   bool                `json:"ok"`
		Data StarsStatusResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Data.Enabled {
		t.Fatal("включенная монетизация не видна в status")
	}
	if resp.Data.Balance != 3 {
		t.Fatalf("welcome-баланс = %d, want 3", resp.Data.Balance)
	}
	if resp.Data.Price != 25 || resp.Data.Pack != 10 {
		t.Fatalf("price/pack = %d/%d, want 25/10", resp.Data.Price, resp.Data.Pack)
	}
}

func TestStarsInvoiceDisabledReturns403(t *testing.T) {
	s := newAnalyzeTestServer(t, false)

	req := httptest.NewRequest(http.MethodPost, "/api/stars/invoice", nil)
	req.Header.Set("X-Init-Data", signInitData(t, 100, s.cfg.BotToken))
	rec := httptest.NewRecorder()
	s.authMiddleware(s.handleStarsInvoice)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("invoice при выключенной: %d, want 403", rec.Code)
	}
}
