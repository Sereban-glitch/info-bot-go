package ai

import (
        "encoding/json"
        "io"
        "net/http"
        "net/http/httptest"
        "strings"
        "sync"
        "testing"
)

// anthropicMux — мок-сервер консоли Antigravity: /v1/messages отвечает
// форматом Anthropic, /v1/chat/completions — OpenAI-форматом, всё
// остальное — 404. Пишет счетчики обращений к эндпоинтам.
func anthropicMux(t *testing.T, messagesBody string, hits *map[string]int) *httptest.Server {
        t.Helper()
        var mu sync.Mutex
        mux := http.NewServeMux()
        mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
                if hits != nil {
                        mu.Lock()
                        (*hits)["/v1/messages"]++
                        mu.Unlock()
                }
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(messagesBody))
        })
        mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
                if hits != nil {
                        mu.Lock()
                        (*hits)["/v1/chat/completions"]++
                        mu.Unlock()
                }
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"choices":[{"message":{"content":"openai-fallback-text"}}]}`))
        })
        return httptest.NewServer(mux)
}

// TestProxyChatMessagesFormat — запрос к /v1/messages собирается в формате
// Anthropic (system отдельно, пользовательский текст в content-блоках),
// ответ собирается из text-блоков, блоки thinking игнорируются.
func TestProxyChatMessagesFormat(t *testing.T) {
        var gotBody map[string]interface{}
        var gotPath string
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                gotPath = r.URL.Path
                raw, _ := io.ReadAll(r.Body)
                _ = json.Unmarshal(raw, &gotBody)
                // thinking-блок + два text-блока: должен собраться конкатенацией
                _, _ = w.Write([]byte(`{"content":[{"type":"thinking","thinking":"роздуми"},{"type":"text","text":"{\"type\":\"refusal\","},{"type":"text","text":"\"isLegal\":\"illegal\"}"}]}`))
        }))
        defer srv.Close()

        rot := NewRotator([]string{"test-key"}, "m", "mf")
        rot.SetProxy(ProxyConfig{URL: srv.URL, Model: "proxy-model"})

        parts1 := []interface{}{
                map[string]string{"text": "ТЕКСТ ЗАПИТА"},
        }
        contents := []interface{}{
                map[string]interface{}{"role": "user", "parts": parts1},
        }
        text, err := rot.proxyChat("системная инструкция", contents, false)
        if err != nil {
                t.Fatalf("proxyChat: %v", err)
        }
        if gotPath != "/v1/messages" {
                t.Fatalf("запрос должен идти на /v1/messages, пошёл на %s", gotPath)
        }
        if !strings.Contains(text, "refusal") || !strings.Contains(text, "illegal") {
                t.Fatalf("текст из нескольких блоков не собран: %q", text)
        }
        if strings.Contains(text, "роздуми") {
                t.Fatalf("thinking-блок не должен попадать в ответ: %q", text)
        }

        // Формат запроса: system отдельно, content-блоки в пользовательском сообщении.
        if sys, _ := gotBody["system"].(string); sys != "системная инструкция" {
                t.Fatalf("system prompt должен передаваться полем system, получено: %v", gotBody["system"])
        }
        if mt, _ := gotBody["max_tokens"].(float64); mt < 1024 {
                t.Fatalf("max_tokens должен быть щедрым для длинных документов, получено %v", mt)
        }
        msgs, _ := gotBody["messages"].([]interface{})
        if len(msgs) != 1 {
                t.Fatalf("ожидалось 1 пользовательское сообщение (system идёт отдельным полем), получено %d", len(msgs))
        }
        userMsg, _ := msgs[0].(map[string]interface{})
        userContent, _ := userMsg["content"].([]interface{})
        found := false
        for _, p := range userContent {
                pm, _ := p.(map[string]interface{})
                if pm["type"] == "text" && pm["text"] == "ТЕКСТ ЗАПИТА" {
                        found = true
                }
        }
        if !found {
                t.Fatalf("текстовая часть не найдена в запросе: %v", gotBody)
        }
}

// TestProxyChatMapStringParts — старая структура (parts = []map[string]string,
// как в GenerateFromDescription/ImproveRequest) тоже должна работать через прокси.
func TestProxyChatMapStringParts(t *testing.T) {
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/v1/messages" {
                        http.NotFound(w, r)
                        return
                }
                _, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"subject\":\"s\",\"body\":\"b\"}"}]}`))
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
        text, err := rot.proxyChat("", contents, false)
        if err != nil {
                t.Fatalf("proxyChat map-string parts: %v", err)
        }
        if !strings.Contains(text, "Опис: тест") && !strings.Contains(text, "subject") {
                t.Fatalf("ответ не получен: %q", text)
        }
}

// TestProxyChatFallsBackToOpenAI — если /v1/messages отвечает 404 (старый
// прокси), клиент откатывается на /v1/chat/completions и запоминает выбор:
// последующие запросы идут сразу в рабочий эндпоинт.
func TestProxyChatFallsBackToOpenAI(t *testing.T) {
        hits := map[string]int{}
        mux := http.NewServeMux()
        mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
                hits["/v1/messages"]++
                http.NotFound(w, r) // «консоль убрала новый эндпоинт»
        })
        mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
                hits["/v1/chat/completions"]++
                _, _ = w.Write([]byte(`{"choices":[{"message":{"content":"legacy-ok"}}]}`))
        })
        srv := httptest.NewServer(mux)
        defer srv.Close()

        rot := NewRotator([]string{"test-key"}, "m", "mf")
        rot.SetProxy(ProxyConfig{URL: srv.URL, Model: "proxy-model"})

        contents := []interface{}{
                map[string]interface{}{"role": "user", "parts": []interface{}{map[string]string{"text": "привіт"}}},
        }
        text, err := rot.proxyChat("", contents, false)
        if err != nil {
                t.Fatalf("proxyChat с откатом: %v", err)
        }
        if text != "legacy-ok" {
                t.Fatalf("ожидался текст legacy-эндпоинта, получено: %q", text)
        }

        // Липкий выбор: второй запрос не должен снова ходить на /v1/messages.
        _, err = rot.proxyChat("", contents, false)
        if err != nil {
                t.Fatalf("proxyChat второй вызов: %v", err)
        }
        if hits["/v1/messages"] != 1 {
                t.Fatalf("после первого успеха выбор эндпоинта должен быть липким, обращений к /v1/messages: %d", hits["/v1/messages"])
        }
        if hits["/v1/chat/completions"] != 2 {
                t.Fatalf("оба запроса должны пройти через /v1/chat/completions, обращений: %d", hits["/v1/chat/completions"])
        }
}

// TestProxyStreamSSE — стриминговый запрос /v1/messages: текстовые дельты
// уходят в onChunk по порядку, thinking-дельты пропускаются, полный текст
// собирается корректно.
func TestProxyStreamSSE(t *testing.T) {
        sse := "event: message_start\n" +
                `data: {"type":"message_start","message":{"id":"msg_1"}}` + "\n\n" +
                "event: content_block_start\n" +
                `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
                "event: content_block_delta\n" +
                `data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"думаю"}}` + "\n\n" +
                "event: content_block_delta\n" +
                `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Скарга "}}` + "\n\n" +
                "event: content_block_delta\n" +
                `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"на відмову"}}` + "\n\n" +
                "event: message_stop\n" +
                `data: {"type":"message_stop"}` + "\n\n"

        
        var reqCheck map[string]interface{}
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                raw, _ := io.ReadAll(r.Body)
                _ = json.Unmarshal(raw, &reqCheck)
                w.Header().Set("Content-Type", "text/event-stream")
                _, _ = w.Write([]byte(sse))
        }))
        defer srv.Close()

        rot := NewRotator([]string{"test-key"}, "m", "mf")
        rot.SetProxy(ProxyConfig{URL: srv.URL, Model: "proxy-model"})

        contents := []interface{}{
                map[string]interface{}{"role": "user", "parts": []interface{}{map[string]string{"text": "документ"}}},
        }
        var chunks []string
        text, err := rot.proxyChatStream("", contents, false, func(delta string) {
                chunks = append(chunks, delta)
        })
        if err != nil {
                t.Fatalf("proxyChatStream: %v", err)
        }
        if text != "Скарга на відмову" {
                t.Fatalf("полный текст не собран: %q", text)
        }
        if len(chunks) != 2 || chunks[0] != "Скарга " || chunks[1] != "на відмову" {
                t.Fatalf("дельты должны приходить по порядку без thinking: %v", chunks)
        }
        if st, _ := reqCheck["stream"].(bool); !st {
                t.Fatalf("в запросе должен быть stream=true: %v", reqCheck)
        }
}

// TestContentsHasAudio — аудио в contents определяется по mimeType;
// текстовые запросы и фото — не аудио.
func TestContentsHasAudio(t *testing.T) {
        audio := []interface{}{
                map[string]interface{}{
                        "role": "user",
                        "parts": []interface{}{
                                map[string]string{"text": "розшифруй"},
                                map[string]interface{}{"inlineData": map[string]string{"mimeType": "audio/ogg", "data": "AAAA"}},
                        },
                },
        }
        if !contentsHasAudio(audio) {
                t.Fatal("аудиозапись должна определяться")
        }
        photo := []interface{}{
                map[string]interface{}{
                        "role": "user",
                        "parts": []interface{}{
                                map[string]interface{}{"inlineData": map[string]string{"mimeType": "image/jpeg", "data": "AAAA"}},
                        },
                },
        }
        if contentsHasAudio(photo) {
                t.Fatal("фото — не аудио")
        }
        textOnly := []interface{}{
                map[string]interface{}{
                        "role":  "user",
                        "parts": []map[string]string{{"text": "привіт"}},
                },
        }
        if contentsHasAudio(textOnly) {
                t.Fatal("текстовый запрос — не аудио")
        }
}

// TestSplitDataURL — разбор data-URI на MIME и данные.
func TestSplitDataURL(t *testing.T) {
        mime, data := splitDataURL("data:image/jpeg;base64,QUJD")
        if mime != "image/jpeg" || data != "QUJD" {
                t.Fatalf("data:image/jpeg;base64,QUJD → (%q, %q)", mime, data)
        }
        mime, data = splitDataURL("data:audio/ogg;base64,REVG")
        if mime != "audio/ogg" || data != "REVG" {
                t.Fatalf("data:audio/ogg;base64,REVG → (%q, %q)", mime, data)
        }
        if m, d := splitDataURL("https://example.com/x.png"); m != "" || d != "" {
                t.Fatalf("не-data URI должен давать пустые значения, получено (%q, %q)", m, d)
        }
}
