package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProxyChatCollectsParts — диагностика ТЗ №6: AnalyzeRefusal строит
// contents из []interface{} c map[string]string внутри; прокси обязан
// распознавать такую структуру (раньше падал с «пустой запрос»).
func TestProxyChatCollectsParts(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"refusal\",\"isLegal\":\"illegal\",\"nextStep\":\"complaint\"}"}}]}`))
	}))
	defer srv.Close()

	rot := NewRotator([]string{"test-key"}, "m", "mf")
	rot.SetProxy(ProxyConfig{URL: srv.URL, Model: "proxy-model"})

	// Структура 1: как в AnalyzeRefusal (parts = []interface{}).
	parts1 := []interface{}{
		map[string]string{"text": "ТЕКСТ ЗАПИТА"},
	}
	contents1 := []interface{}{
		map[string]interface{}{"role": "user", "parts": parts1},
	}
	_, err := rot.proxyChat("системная инструкция", contents1, false)
	if err != nil {
		t.Fatalf("proxyChat structure1: %v", err)
	}
	if gotBody == nil {
		t.Fatalf("proxy received no body")
	}
	msgs, _ := gotBody["messages"].([]interface{})
	if len(msgs) != 2 { // system + user
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	userMsg, _ := msgs[1].(map[string]interface{})
	userContent, _ := userMsg["content"].([]interface{})
	found := false
	for _, p := range userContent {
		pm, _ := p.(map[string]interface{})
		if txt, _ := pm["text"].(string); txt == "ТЕКСТ ЗАПИТА" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user text part not found in proxy request: %v", gotBody)
	}
}

// TestProxyChatMapStringParts — старая структура (parts = []map[string]string,
// как в GenerateFromDescription/ImproveRequest) тоже должна работать через прокси.
func TestProxyChatMapStringParts(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"subject\":\"s\",\"body\":\"b\"}"}}]}`))
	}))
	defer srv.Close()

	rot := NewRotator([]string{"test-key"}, "m", "mf")
	rot.SetProxy(ProxyConfig{URL: srv.URL, Model: "proxy-model"})

	contents := []interface{}{
		map[string]interface{}{
			"role":  "user",
			"parts": []map[string]string{{"text": "Опис: тест"}},
		},
	}
	_, err := rot.proxyChat("", contents, false)
	if err != nil {
		t.Fatalf("proxyChat map-string parts: %v", err)
	}
	raw, _ := json.Marshal(gotBody)
	if !strings.Contains(string(raw), "Опис: тест") {
		t.Fatalf("text part lost for []map[string]string contents: %s", raw)
	}
}
