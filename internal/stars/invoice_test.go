package stars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPayloadRoundtrip(t *testing.T) {
	p := BuildPayload(42, 10)
	uid, credits, ok := ParsePayload(p)
	if !ok || uid != 42 || credits != 10 {
		t.Fatalf("ParsePayload(%q) = %d,%d,%v — want 42,10,true", p, uid, credits, ok)
	}
}

func TestPayloadRejectsGarbage(t *testing.T) {
	bad := []string{
		"",
		"analyze:42:10",           // без времени
		"analyze:x:10:123",        // нечисловой user
		"analyze:0:10:123",        // нулевой user
		"analyze:42:0:123",        // нулевые кредиты
		"analyze:42:-5:123",       // отрицательные кредиты
		"analyze:42:999999:123",   // завышенные кредиты
		"analyze:42:10:abc",       // нечисловое время
		"bonus:42:10:123",         // чужой префикс
		"analyze:42:10:123:extra", // лишний сегмент
	}
	for _, p := range bad {
		if _, _, ok := ParsePayload(p); ok {
			t.Errorf("ParsePayload(%q) принял мусор", p)
		}
	}
}

func TestCreateInvoiceLink(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/botTOK123/createInvoiceLink") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":"https://t.me/$xABCD"}`))
	}))
	defer srv.Close()

	c := NewClient("TOK123")
	c.SetAPIBase(srv.URL)

	link, err := c.CreateInvoiceLink("Пакет розборів", "10 AI-розборів", BuildPayload(7, 10), 25)
	if err != nil {
		t.Fatalf("CreateInvoiceLink: %v", err)
	}
	if link != "https://t.me/$xABCD" {
		t.Fatalf("link = %q", link)
	}

	// Валюта — строго Stars (XTR), цена — целыми Stars.
	if gotBody["currency"] != "XTR" {
		t.Errorf("currency = %v, want XTR", gotBody["currency"])
	}
	prices := gotBody["prices"].([]interface{})
	p0 := prices[0].(map[string]interface{})
	if p0["amount"].(float64) != 25 {
		t.Errorf("amount = %v, want 25", p0["amount"])
	}
	// provider_token в Stars-платежах не нужен — убедимся, что не отправляем
	if _, exists := gotBody["provider_token"]; exists {
		t.Errorf("provider_token отправлен, хотя для XTR не нужен")
	}
}

func TestCreateInvoiceLinkAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: invoice title is empty"}`))
	}))
	defer srv.Close()

	c := NewClient("TOK")
	c.SetAPIBase(srv.URL)
	if _, err := c.CreateInvoiceLink("", "", "p", 25); err == nil {
		t.Fatal("ошибка Bot API не прокинута")
	}
}

func TestCreateInvoiceLinkPriceValidation(t *testing.T) {
	c := NewClient("TOK")
	if _, err := c.CreateInvoiceLink("t", "d", "p", 0); err == nil {
		t.Fatal("нулевая цена должна отклоняться локально")
	}
	c2 := NewClient("") // без токена
	if _, err := c2.CreateInvoiceLink("t", "d", "p", 5); err == nil {
		t.Fatal("пустой токен должен отклоняться")
	}
}
