package web

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"info-bot-go/internal/config"
)

const testBotToken = "123456:TEST-TOKEN-for-auth-middleware"

// signInitData строит валидный initData с HMAC-подписью Telegram WebApp
// (алгоритм — по спецификации; см. computeInitDataHash и контрольный вектор
// TestComputeInitDataHashReferenceVector).
func signInitData(t *testing.T, userID int64, botToken string) string {
	t.Helper()
	userJSON, _ := json.Marshal(map[string]interface{}{"id": userID, "first_name": "Тест"})
	params := url.Values{}
	params.Set("auth_date", strconv.FormatInt(time.Now().Unix(), 10))
	params.Set("query_id", "AAF-test-query")
	params.Set("user", string(userJSON))

	// data-check-string: отсортированные ключи, декодированные значения
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	// url.Values.Sort() даёт лексикографический порядок
	dcs := ""
	for i, k := range sortedKeys(params) {
		if i > 0 {
			dcs += "\n"
		}
		dcs += k + "=" + params.Get(k)
	}

	// Подпись — строго по спецификации Telegram WebApps:
	//   secret_key = HMAC_SHA256(key: "WebAppData", data: <bot_token>)
	//   hash       = HMAC_SHA256(key: secret_key, data: data_check_string)
	// (раньше здесь были переставлены ключ и сообщение — тест проходил,
	// а реальные клиенты Telegram — нет; см. TestComputeInitDataHashReferenceVector)
	params.Set("hash", hex.EncodeToString(computeInitDataHash(dcs, botToken)))
	return params.Encode()
}

func sortedKeys(v url.Values) []string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func newTestServer() *Server {
	return &Server{cfg: &config.Config{BotToken: testBotToken}}
}

// Фикс A1 (аудит №22): спуфнутые идентификаторы без подписи больше
// не проходят аутентификацию — раньше ?user_id= отдавал профиль любого
// пользователя, включая ФИО, e-mail и историю запросов.
func TestAuthRejectsSpoofedIDs(t *testing.T) {
	s := newTestServer()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	})
	mw := s.authMiddleware(next)

	cases := []struct {
		name string
		url  string
		hdr  map[string]string
	}{
		{"spoofed user_id param", "/api/me?user_id=745130167", nil},
		{"spoofed X-User-ID header", "/api/me", map[string]string{"X-User-ID": "745130167"}},
		{"both spoofed", "/api/me?user_id=1", map[string]string{"X-User-ID": "2"}},
		{"no auth at all", "/api/me", nil},
		{"garbage init_data", "/api/me?init_data=garbage", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			for k, v := range tc.hdr {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			mw(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s: получили %d, ожидаем 401 (body: %s)", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// Валидная HMAC-подпись Telegram по-прежнему проходит — мини-апп не ломаем.
func TestAuthPassesValidHMAC(t *testing.T) {
	s := newTestServer()
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if got := getUserID(r); got != 424242 {
			t.Errorf("user id в контексте = %d, ожидаем 424242", got)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	})
	mw := s.authMiddleware(next)

	initData := signInitData(t, 424242, testBotToken)

	// Путь 1: заголовок X-Init-Data
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Init-Data", initData)
	rec := httptest.NewRecorder()
	mw(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Errorf("X-Init-Data: code=%d called=%v — валидная подпись обязана проходить", rec.Code, called)
	}

	// Путь 2: query-параметр init_data
	called = false
	req = httptest.NewRequest(http.MethodGet, "/api/me?init_data="+url.QueryEscape(initData), nil)
	rec = httptest.NewRecorder()
	mw(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Errorf("init_data param: code=%d called=%v — валидная подпись обязана проходить", rec.Code, called)
	}

	// Подпись, сделанная чужим токеном, — отвергается.
	called = false
	forged := signInitData(t, 424242, "999999:WRONG-TOKEN")
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("X-Init-Data", forged)
	rec = httptest.NewRecorder()
	mw(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Errorf("forged: code=%d called=%v — подпись чужим токеном обязана отвергаться", rec.Code, called)
	}
}

// TestComputeInitDataHashReferenceVector — контрольный вектор, независимо
// посчитанный по спецификации Telegram WebApps (Python: hmac.new(
// b"WebAppData", bot_token, sha256)). Если кто-то снова переставит ключ
// и сообщение местами (исторический баг ТЗ №5), этот тест упадёт —
// в отличие от roundtrip-тестов, которые самосогласованы и подмену не ловят.
func TestComputeInitDataHashReferenceVector(t *testing.T) {
	const botToken = "5432109876:AAE-9p0ZtestTESTtestTESTtestTESTtes"
	dcs := "auth_date=1758969246\n" +
		"query_id=AAF17IkBEQAAAAAA-H0Q4AAF-9p0Z\n" +
		`user={"id":745130167,"first_name":"Сергій","last_name":"Гаршин","username":"sereban_tech","language_code":"uk"}`

	got := hex.EncodeToString(computeInitDataHash(dcs, botToken))
	want := "c5e54d340a0a691ae9e3b166172acbc1bc12f05bb670fd274a0373b5ef3e62de"
	if got != want {
		t.Fatalf("подпись не совпадает с контрольным вектором спецификации:\n got  %s\n want %s", got, want)
	}
}
