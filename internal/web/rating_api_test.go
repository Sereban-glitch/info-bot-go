package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"info-bot-go/internal/dostup"
)

// Публичный рейтинг: /api/rating отдаёт агрегаты без авторизации
// (это его смысл — публичная страница), а персональные эндпоинты
// остаются закрытыми (регресс-фикс A1 из аудита №22 не сломан).
func TestRatingAPIPublicAndAuthIntact(t *testing.T) {
	s := newTestServer()
	// Подключаем рейтинги: маленький каталог + собранная статистика
	s.catalog = dostup.NewCatalogStore(filepath.Join(t.TempDir(), "cat.json"))
	s.ratings = dostup.NewRatingsStore(filepath.Join(t.TempDir(), "ratings.json"))
	s.ratings.Set("good", dostup.BodyStats{Requests: 61, Successful: 56, Overdue: 3})
	s.ratings.Set("bad", dostup.BodyStats{Requests: 20, Successful: 1, Overdue: 15})
	s.ratings.Set("tiny", dostup.BodyStats{Requests: 3}) // ниже порога — не в рейтинге

	// 1) Публичный доступ: 200 без какой-либо подписи
	req := httptest.NewRequest(http.MethodGet, "/api/rating?sort=best", nil)
	rec := httptest.NewRecorder()
	s.handleRating(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/rating без подписи: %d, ожидаем 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK   bool           `json:"ok"`
		Data RatingResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON не парсится: %v", err)
	}
	// Пустой каталог (Load не вызывался) — Leaderboard по пустому списку тел.
	// Подкладываем каталог напрямую через Replace.
	cat := &dostup.Catalog{Version: 1, Bodies: []dostup.CatalogBody{
		{Slug: "good", Name: "Мін'юст"},
		{Slug: "bad", Name: "Якась установа"},
		{Slug: "tiny", Name: "Дрібний"},
	}}
	if !s.catalog.Replace(cat) {
		t.Fatal("Replace каталога не сработал")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/rating?sort=best", nil)
	rec = httptest.NewRecorder()
	s.handleRating(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON не парсится: %v", err)
	}
	if !resp.OK {
		t.Fatal("ok=false в ответе рейтинга")
	}
	if resp.Data.Total != 2 {
		t.Fatalf("total=%d, ожидаем 2 (порог ≥5 отрезает tiny)", resp.Data.Total)
	}
	if len(resp.Data.Items) != 2 {
		t.Fatalf("items=%d, ожидаем 2", len(resp.Data.Items))
	}
	first := resp.Data.Items[0]
	if first.Slug != "good" || first.Index != 92 || first.Badge != "🟢" {
		t.Fatalf("первый в топе: %+v — ожидаем good/92/🟢", first)
	}
	if first.PortalURL != dostup.BaseURL+"/body/good" {
		t.Fatalf("portalUrl=%s", first.PortalURL)
	}
	if resp.Data.Catalog != 3 || resp.Data.Covered != 3 {
		t.Fatalf("catalog=%d covered=%d, ожидаем 3/3", resp.Data.Catalog, resp.Data.Covered)
	}

	// Антирейтинг: худший первым
	req = httptest.NewRequest(http.MethodGet, "/api/rating?sort=worst", nil)
	rec = httptest.NewRecorder()
	s.handleRating(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if resp.Data.Items[0].Slug != "bad" || resp.Data.Items[0].Index != 5 {
		t.Fatalf("антирейтинг первым: %+v — ожидаем bad/5", resp.Data.Items[0])
	}

	// 2) Персональные эндпоинты по-прежнему закрыты
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec = httptest.NewRecorder()
	mw := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next не должен вызываться без подписи")
	}))
	mw(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/api/me без подписи: %d, ожидаем 401 — регресс A1!", rec.Code)
	}
}

// POST на рейтинг — 405: эндпоинт строго GET.
func TestRatingAPIMethodNotAllowed(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/rating", nil)
	rec := httptest.NewRecorder()
	s.handleRating(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/rating: %d, ожидаем 405", rec.Code)
	}
}
