package ai

// Тесты двухэтапного разбора (вердикт → документ) и прямого стриминга
// Gemini. Пилот стриминга из дорожной карты аудита.

import (
        "fmt"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"
)

// TestParseDocumentDraft — первый ряд документа = тема, остальное = тело;
// markdown-заборы снимаются; пустой ввод не ошибка.
func TestParseDocumentDraft(t *testing.T) {
        subj, body, err := ParseDocumentDraft("Скарга на відмову\n\nПрошу надати інформацію...")
        if err != nil {
                t.Fatalf("ParseDocumentDraft: %v", err)
        }
        if subj != "Скарга на відмову" {
                t.Fatalf("тема должна быть первым рядом, получено: %q", subj)
        }
        if !strings.HasPrefix(body, "Прошу") {
                t.Fatalf("тело должно идти после первого ряда, получено: %q", body)
        }

        // Заборы ```text ... ``` снимаются.
        subj, body, err = ParseDocumentDraft("```\nТема документа\n\nТекст.\n```")
        if err != nil {
                t.Fatalf("ParseDocumentDraft с забором: %v", err)
        }
        if subj != "Тема документа" || body != "Текст." {
                t.Fatalf("заборы не сняты: subj=%q body=%q", subj, body)
        }

        // Одна строка без перевода — вся строка тема.
        subj, body, err = ParseDocumentDraft("Лише тема")
        if err != nil || subj != "Лише тема" || body != "" {
                t.Fatalf("одна строка: subj=%q body=%q err=%v", subj, body, err)
        }

        // Пусто — пусто, без ошибки.
        subj, body, err = ParseDocumentDraft("   ")
        if err != nil || subj != "" || body != "" {
                t.Fatalf("пустой ввод: subj=%q body=%q err=%v", subj, body, err)
        }
}

// TestRefusalPromptsSplit — промпт вердикта не просит документ, полный —
// просит; промпт документа знает вердикт и формат «первый ряд — тема».
func TestRefusalPromptsSplit(t *testing.T) {
        verdict := refusalVerdictSystemPrompt()
        full := refusalAnalysisSystemPrompt()

        if strings.Contains(verdict, "draftSubject") {
                t.Fatal("промпт вердикта не должен просить draftSubject — документ готовится отдельным этапом")
        }
        if !strings.Contains(verdict, "nextStep") {
                t.Fatal("промпт вердикта обязан определять nextStep — от него зависит этап документа")
        }
        if !strings.Contains(full, "draftSubject") {
                t.Fatal("полный промпт должен включать документ (обратная совместимость)")
        }

        a := &RefusalAnalysis{
                Type:     "refusal",
                IsLegal:  "illegal",
                NextStep: "complaint",
                Violations: []Violation{
                        {Article: "Стаття 18", Reason: "відмова без підстави"},
                        {Article: "Стаття 17", Reason: "прострочення"},
                },
        }
        doc := refusalDocumentSystemPrompt(a)
        for _, want := range []string{"refusal", "illegal", "complaint", "Стаття 18", "Стаття 17", "перший рядок — тема документа"} {
                if !strings.Contains(doc, want) {
                        t.Fatalf("промпт документа должен содержать %q", want)
                }
        }
        if strings.Contains(doc, "валідний JSON") {
                t.Fatal("промпт документа не должен просить вернуть JSON — только простой текст")
        }
}

// TestDocumentGoal — каждый следующий шаг получает свою цель документа.
func TestDocumentGoal(t *testing.T) {
        cases := map[string]string{
                "clarification": "уточнення",
                "complaint":     "скарга",
                "appeal":        "Звернення",
                "none":          "звернення",
        }
        for step, want := range cases {
                g := documentGoal(step)
                if !strings.Contains(strings.ToLower(g), strings.ToLower(want)) {
                        t.Fatalf("documentGoal(%q) должен содержать %q, получено: %q", step, want, g)
                }
        }
}

// TestGeminiRequestStream — прямой стриминг Gemini: SSE-события
// streamGenerateContent собираются в полный текст, дельты уходят в onChunk.
func TestGeminiRequestStream(t *testing.T) {
        sse := "data: " + `{"candidates":[{"content":{"parts":[{"text":"Перший "}]}},{"content":{"parts":[{"text":"абзац."}]}}]}` + "\n\n" +
                "data: " + `{"candidates":[{"content":{"parts":[{"text":"Другий."}]}}]}` + "\n\n"

        var gotPath, gotQuery string
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                gotPath = r.URL.Path
                gotQuery = r.URL.RawQuery
                w.Header().Set("Content-Type", "text/event-stream")
                _, _ = w.Write([]byte(sse))
        }))
        defer srv.Close()

        rot := NewRotator([]string{"k"}, "gemini-test", "fb")
        rot.geminiBase = srv.URL

        var chunks []string
        text, err := rot.geminiRequestStream("система", []interface{}{
                map[string]interface{}{
                        "role":  "user",
                        "parts": []interface{}{map[string]string{"text": "запит"}},
                },
        }, "", func(delta string) {
                chunks = append(chunks, delta)
        })
        if err != nil {
                t.Fatalf("geminiRequestStream: %v", err)
        }
        if text != "Перший абзац.Другий." {
                t.Fatalf("полный текст не собран: %q", text)
        }
        if len(chunks) != 3 || chunks[0] != "Перший " || chunks[2] != "Другий." {
                t.Fatalf("дельты должны приходить по частям в порядке поступления: %v", chunks)
        }
        if !strings.HasPrefix(gotPath, "/models/gemini-test:streamGenerateContent") {
                t.Fatalf("путь стриминга неверен: %s", gotPath)
        }
        if !strings.Contains(gotQuery, "alt=sse") {
                t.Fatalf("запрос должен требовать SSE (alt=sse): %s", gotQuery)
        }
}

// TestAnalyzeRefusalDocumentNoDocumentNeeded — при nextStep=none документ
// не запрашивается вовсе (пустой результат без похода в сеть).
func TestAnalyzeRefusalDocumentNoDocumentNeeded(t *testing.T) {
        rot := NewRotator([]string{"k"}, "m", "fb")
        // nil-прокси и мёртвый base: если метод полезет в сеть — тест зависнет
        // или упадёт; правильное поведение — сразу пустой результат.
        rot.geminiBase = "http://127.0.0.1:1"
        a := &RefusalAnalysis{NextStep: "none"}
        subj, body, err := rot.AnalyzeRefusalDocument(a, "орган", "тема", "текст", nil)
        if err != nil || subj != "" || body != "" {
                t.Fatalf("nextStep=none: subj=%q body=%q err=%v — документ не должен запрашиваться", subj, body, err)
        }
}

// TestAnalyzeRefusalTwoStagesThroughProxy — полный разбор через мок-прокси:
// вердикт (JSON) и документ (plain text) собираются из двух запросов.
func TestAnalyzeRefusalTwoStagesThroughProxy(t *testing.T) {
        var nReq int
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/v1/messages" {
                        http.NotFound(w, r)
                        return
                }
                nReq++
                if nReq == 1 {
                        // Этап 1: вердикт.
                        _, _ = fmt.Fprint(w, `{"content":[{"type":"text","text":"{\"type\":\"refusal\",\"summary\":\"s\",\"isLegal\":\"illegal\",\"nextStep\":\"complaint\",\"recommendation\":\"r\"}"}]}`)
                        return
                }
                // Этап 2: документ — простой текст (первый ряд — тема).
                _, _ = fmt.Fprint(w, `{"content":[{"type":"text","text":"Скарга на відмову Органу\n\nПрошу надати..."}]}`)
        }))
        defer srv.Close()

        rot := NewRotator([]string{"k"}, "m", "fb")
        rot.SetProxy(ProxyConfig{URL: srv.URL, Model: "proxy-model"})

        a, err := rot.AnalyzeRefusal("Орган", "Тема", "Відмова без підстав", nil)
        if err != nil {
                t.Fatalf("AnalyzeRefusal: %v", err)
        }
        if a.Type != "refusal" || a.IsLegal != "illegal" || a.NextStep != "complaint" {
                t.Fatalf("вердикт не разобран: %+v", a)
        }
        if a.DraftSubject != "Скарга на відмову Органу" {
                t.Fatalf("тема документа не разобрана: %q", a.DraftSubject)
        }
        if !strings.HasPrefix(a.DraftBody, "Прошу") {
                t.Fatalf("тело документа не разобрано: %q", a.DraftBody)
        }
        if nReq != 2 {
                t.Fatalf("ожидалось 2 запроса к модели (вердикт + документ), было %d", nReq)
        }
}
