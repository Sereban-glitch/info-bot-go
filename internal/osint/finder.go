package osint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

var cascadeModels = []string{
	"gemini-2.5-flash",
	"gemini-2.5-flash-lite",
}

const systemPrompt = `Ти — OSINT-дослідник. Твоє завдання — знайти офіційну електронну пошту державного органу України.
Поточний рік: 2026. Шукай найсвіжіші контакти.

ВИКОРИСТОВУЙ Google Пошук (grounding) для знаходження актуальної інформації.
Перевіряй офіційний сайт органу, сторінки "Контакти" або "Звернення громадян".

Якщо знайдеш email — поверни JSON:
{"email": "знайдений_email@example.com", "agency": "Повна офіційна назва органу", "confidence": "high"}

Якщо не впевнений — confidence: "medium".
Якщо не знайдеш жодного email — confidence: "not_found", email: "".

ВАЖЛИВО: Повертай ТІЛЬКИ JSON, без пояснень.`

type Result struct {
	Email       string   `json:"email"`
	AgencyName  string   `json:"agency"`
	Confidence  string   `json:"confidence"`
	SourceURLs  []string `json:"sourceURLs,omitempty"`
	RawResponse string   `json:"-"`
}

type keyState struct {
	key           string
	cooldownUntil time.Time
}

type Finder struct {
	keys   []keyState
	mu     sync.Mutex
	client *http.Client
}

func NewFinder(apiKeys []string) *Finder {
	ks := make([]keyState, len(apiKeys))
	for i, k := range apiKeys {
		ks[i] = keyState{key: k}
	}
	return &Finder{
		keys:   ks,
		client: &http.Client{Timeout: 90 * time.Second},
	}
}

func (f *Finder) FindEmail(agencyName string) (*Result, error) {
	userPrompt := fmt.Sprintf("Знайди офіційну електронну пошту для \"%s\".", agencyName)

	payload := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []map[string]string{
					{"text": userPrompt},
				},
			},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"google_search": map[string]interface{}{},
			},
		},
	}
	payload["systemInstruction"] = map[string]interface{}{
		"parts": []map[string]string{{"text": systemPrompt}},
	}

	body, _ := json.Marshal(payload)

	for _, model := range cascadeModels {
		for attempt := 0; attempt < 3; attempt++ {
			f.mu.Lock()
			available := f.availableKeys()
			if len(available) == 0 {
				earliest := f.earliestReady()
				f.mu.Unlock()
				if earliest.After(time.Now()) {
					wait := time.Until(earliest) + time.Second
					log.Printf("[OSINT] All keys in cooldown — waiting %.0fs for %s", wait.Seconds(), model)
					time.Sleep(wait)
				}
				continue
			}
			ki := rand.Intn(len(available))
			key := available[ki]
			f.mu.Unlock()

			url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, key.key)
			masked := key.key[:6] + "..." + key.key[len(key.key)-4:]
			log.Printf("[OSINT] model: %s, key: %s — searching: %s (attempt %d)", model, masked, agencyName, attempt+1)

			resp, err := f.client.Post(url, "application/json", bytes.NewReader(body))
			if err != nil {
				log.Printf("[OSINT] Network error on %s with %s: %v", model, masked, err)
				continue
			}

			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == 200 {
				return f.parseResponse(respBody)
			}

			if resp.StatusCode == 429 {
				log.Printf("[OSINT] Key %s rate-limited (429) on %s — cooldown 60s", masked, model)
				f.markCooldown(key.key, 60*time.Second)
				continue
			}

			if resp.StatusCode == 403 {
				log.Printf("[OSINT] Key %s forbidden (403) on %s — cooldown 300s", masked, model)
				f.markCooldown(key.key, 300*time.Second)
				continue
			}

			if resp.StatusCode == 400 || resp.StatusCode == 404 {
				log.Printf("[OSINT] Model %s unsupported (code %d) — trying next model", model, resp.StatusCode)
				break
			}

			respBodyStr := string(respBody)
			log.Printf("[OSINT] Unexpected error %d key %s on %s: %s", resp.StatusCode, masked, model, respBodyStr)
		}
	}

	return nil, fmt.Errorf("OSINT exhausted all models without success")
}

func (f *Finder) availableKeys() []keyState {
	var available []keyState
	now := time.Now()
	for _, ks := range f.keys {
		if now.After(ks.cooldownUntil) {
			available = append(available, ks)
		}
	}
	return available
}

func (f *Finder) earliestReady() time.Time {
	earliest := time.Now()
	for _, ks := range f.keys {
		if ks.cooldownUntil.After(earliest) {
			earliest = ks.cooldownUntil
		}
	}
	return earliest
}

func (f *Finder) markCooldown(badKey string, duration time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, ks := range f.keys {
		if ks.key == badKey {
			f.keys[i].cooldownUntil = time.Now().Add(duration)
			return
		}
	}
}

func (f *Finder) parseResponse(respBody []byte) (*Result, error) {
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	text, err := extractText(result)
	if err != nil {
		return nil, err
	}

	urls := extractSourceURLs(result)
	parsed := &Result{RawResponse: text}
	cleaned := cleanJSON(text)
	if err := json.Unmarshal([]byte(cleaned), parsed); err != nil {
		parsed.Email = extractEmailFromText(text)
		parsed.Confidence = "low"
	}
	parsed.SourceURLs = urls
	return parsed, nil
}

func extractText(result map[string]interface{}) (string, error) {
	candidates, ok := result["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		return "", fmt.Errorf("no candidates in response")
	}
	candidate, ok := candidates[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid candidate format")
	}
	content, ok := candidate["content"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid content format")
	}
	parts, ok := content["parts"].([]interface{})
	if !ok || len(parts) == 0 {
		return "", fmt.Errorf("no parts in content")
	}
	part, ok := parts[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid part format")
	}
	text, ok := part["text"].(string)
	if !ok {
		return "", fmt.Errorf("no text in part")
	}
	return text, nil
}

func extractSourceURLs(result map[string]interface{}) []string {
	gm, ok := result["groundingMetadata"].(map[string]interface{})
	if !ok {
		return nil
	}
	chunks, ok := gm["groundingChunks"].([]interface{})
	if !ok {
		return nil
	}
	var urls []string
	for _, c := range chunks {
		chunk, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		web, ok := chunk["web"].(map[string]interface{})
		if !ok {
			continue
		}
		if uri, ok := web["uri"].(string); ok {
			urls = append(urls, uri)
		}
	}
	return urls
}

func extractEmailFromText(text string) string {
	text = strings.ReplaceAll(text, " ", "")
	text = strings.ReplaceAll(text, "\n", "")
	text = strings.ReplaceAll(text, "\"", "")
	for _, sep := range []string{",", ";", "|", "(", ")"} {
		parts := strings.Split(text, sep)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if strings.Contains(p, "@") && strings.Contains(p, ".") {
				return p
			}
		}
	}
	return ""
}

func cleanJSON(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
