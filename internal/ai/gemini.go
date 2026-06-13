package ai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxFailuresBeforeBlacklist = 5
	defaultCooldownMs          = 60000
)

// Rotator manages multiple Gemini API keys with automatic rotation and cooldown.
type Rotator struct {
	keys          []keyState
	cursor        int
	model         string
	fallbackModel string
	mu            sync.Mutex
	client        *http.Client
}

type keyState struct {
	key           string
	cooldownUntil int64
	failures      int
	blacklisted   bool
}

// NewRotator creates a new key rotator.
func NewRotator(keys []string, model string, fallbackModel string) *Rotator {
	states := make([]keyState, len(keys))
	for i, k := range keys {
		states[i] = keyState{key: k}
	}
	return &Rotator{
		keys:          states,
		model:         model,
		fallbackModel: fallbackModel,
		client:        &http.Client{Timeout: 60 * time.Second},
	}
}

// Available returns true if at least one key is usable.
func (r *Rotator) Available() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.keys {
		if !s.blacklisted {
			return true
		}
	}
	return false
}

func (r *Rotator) pick() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixMilli()
	for i := 0; i < len(r.keys); i++ {
		idx := (r.cursor + i) % len(r.keys)
		s := &r.keys[idx]
		if s.blacklisted || s.cooldownUntil > now {
			continue
		}
		r.cursor = (idx + 1) % len(r.keys)
		return s.key, nil
	}

	var earliest *keyState
	for i := range r.keys {
		s := &r.keys[i]
		if s.blacklisted {
			continue
		}
		if earliest == nil || s.cooldownUntil < earliest.cooldownUntil {
			earliest = s
		}
	}
	if earliest != nil {
		return earliest.key, nil
	}
	return "", fmt.Errorf("all API keys blacklisted")
}

func (r *Rotator) markRateLimited(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.keys {
		if r.keys[i].key == key {
			r.keys[i].cooldownUntil = time.Now().UnixMilli() + defaultCooldownMs
			r.keys[i].failures++
			if r.keys[i].failures >= maxFailuresBeforeBlacklist {
				r.keys[i].blacklisted = true
				masked := key[:4] + "..." + key[len(key)-4:]
				log.Printf("[AI] Key %s blacklisted after %d failures", masked, r.keys[i].failures)
			}
			return
		}
	}
}

func (r *Rotator) markSuccess(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.keys {
		if r.keys[i].key == key {
			r.keys[i].failures = 0
			return
		}
	}
}

func (r *Rotator) geminiRequest(systemPrompt string, contents []interface{}, responseMIME string) (string, error) {
	key, err := r.pick()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", r.model, key)

	payload := map[string]interface{}{
		"contents": contents,
	}
	if systemPrompt != "" {
		payload["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": systemPrompt}},
		}
	}
	if responseMIME != "" {
		payload["generationConfig"] = map[string]interface{}{
			"responseMimeType": responseMIME,
		}
	}

	body, _ := json.Marshal(payload)
	resp, err := r.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		r.markRateLimited(key)
		return "", fmt.Errorf("gemini API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 || resp.StatusCode == 503 {
		r.markRateLimited(key)
		// Try fallback model if available
		if r.fallbackModel != "" && r.fallbackModel != r.model {
			log.Printf("[AI] Primary model rate limited, trying fallback: %s", r.fallbackModel)
			return r.geminiRequestWithModel(systemPrompt, contents, responseMIME, r.fallbackModel)
		}
		return "", fmt.Errorf("gemini API rate limited (status %d)", resp.StatusCode)
	}
	if resp.StatusCode == 403 {
		// 403 = permanently denied, blacklist this key immediately
		r.mu.Lock()
		for i := range r.keys {
			if r.keys[i].key == key {
				r.keys[i].blacklisted = true
				masked := key[:4] + "..." + key[len(key)-4:]
				log.Printf("[AI] Key %s permanently denied (403), blacklisted", masked)
				break
			}
		}
		r.mu.Unlock()
		// Try with another key + fallback model
		if r.fallbackModel != "" && r.fallbackModel != r.model {
			return r.geminiRequestWithModel(systemPrompt, contents, responseMIME, r.fallbackModel)
		}
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gemini API denied (403): %s", string(respBody))
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		r.markRateLimited(key)
		return "", fmt.Errorf("gemini API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode Gemini response: %w", err)
	}

	r.markSuccess(key)

	candidates, ok := result["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		return "", fmt.Errorf("no candidates in Gemini response")
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

// geminiRequestWithModel makes a request using a specific model (for fallback).
func (r *Rotator) geminiRequestWithModel(systemPrompt string, contents []interface{}, responseMIME string, model string) (string, error) {
	key, err := r.pick()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, key)

	payload := map[string]interface{}{
		"contents": contents,
	}
	if systemPrompt != "" {
		payload["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": systemPrompt}},
		}
	}
	if responseMIME != "" {
		payload["generationConfig"] = map[string]interface{}{
			"responseMimeType": responseMIME,
		}
	}

	body, _ := json.Marshal(payload)
	resp, err := r.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("gemini fallback request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			r.markRateLimited(key)
			return "", fmt.Errorf("gemini fallback rate limited (status %d)", resp.StatusCode)
		}
		return "", fmt.Errorf("gemini fallback error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode Gemini fallback response: %w", err)
	}
	r.markSuccess(key)

	candidates, ok := result["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		return "", fmt.Errorf("no candidates in Gemini fallback response")
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
	log.Printf("[AI] Fallback model %s succeeded", model)
	return text, nil
}

// GenerateText generates text from a prompt using Gemini.
func (r *Rotator) GenerateText(prompt string) (string, error) {
	contents := []interface{}{
		map[string]interface{}{
			"role": "user",
			"parts": []map[string]string{
				{"text": prompt},
			},
		},
	}
	return r.geminiRequest("", contents, "")
}

// ImproveRequest uses AI to improve a request's subject and body.
func (r *Rotator) ImproveRequest(subject, body string) (string, string, error) {
	systemPrompt := "Ти — професійний юрист. Твоє завдання: сформулювати СУТЬ запиту на публічну інформацію. ПРАВИЛА: 1. Тільки українська мова. 2. КАТЕГОРИЧНО ЗАБОРОНЕНО вказувати email-адреси. Використовуй фразу: 'Відповідь прошу надіслати в електронному вигляді на адресу електронної пошти, з якої надіслано цей запит'. 3. Без зайвих технічних деталей. 4. Поверни JSON: {\"subject\": \"тема\", \"body\": \"текст запиту\"}"

	contents := []interface{}{
		map[string]interface{}{
			"role": "user",
			"parts": []map[string]string{
				{"text": "ТЕМА: " + subject + "\nОПИС: " + body},
			},
		},
	}

	text, err := r.geminiRequest(systemPrompt, contents, "application/json")
	if err != nil {
		return subject, body, err
	}

	var result struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		return subject, body, fmt.Errorf("failed to parse AI response: %w", err)
	}
	return result.Subject, result.Body, nil
}

// AnalyzeReply analyzes a government reply and provides a summary.
func (r *Rotator) AnalyzeReply(subject, body string) (string, error) {
	systemPrompt := "Ти — експерт. Опиши суть відповіді у 2-3 реченнях."
	if len(body) > 5000 {
		body = body[:5000]
	}

	contents := []interface{}{
		map[string]interface{}{
			"role": "user",
			"parts": []map[string]string{
				{"text": "Тема: " + subject + "\nТекст: " + body},
			},
		},
	}

	return r.geminiRequest(systemPrompt, contents, "")
}

// VoiceToRequest transcribes voice and structures it as an information request.
func (r *Rotator) VoiceToRequest(audioData []byte, mimeType string) (transcript, recipientHint, subject, body string, err error) {
	systemPrompt := `Ти — асистент для складання інформаційних запитів за Законом України "Про доступ до публічної інформації".
Користувач надсилає голосове повідомлення, де описує ситуацію. Твоя задача — витягнути структуровані дані для запиту.

ОБОВ'ЯЗКОВО поверни валідний JSON у такому форматі:
{
  "transcript": "повна транскрипція",
  "recipientHint": "конкретний орган або 'Офіс Генерального прокурора'",
  "subject": "тема запиту",
  "body": "сформульований предмет запиту українською, юридично грамотно",
  "deliveryMethod": "електронна на e-mail",
  "language": "uk"
}

ПРАВИЛА:
- Якщо орган не згаданий — пиши 'Офіс Генерального прокурора'.
- body має містити чіткі питання без вступних фраз.
- Якщо мова голосового не українська — body перекладай на українську.`

	encoded := base64.StdEncoding.EncodeToString(audioData)

	contents := []interface{}{
		map[string]interface{}{
			"role": "user",
			"parts": []interface{}{
				map[string]interface{}{
					"inlineData": map[string]string{
						"mimeType": "audio/ogg",
						"data":     encoded,
					},
				},
				map[string]string{
					"text": "Розшифруй це голосове та сформуй структурований JSON для інформаційного запиту згідно з інструкціями.",
				},
			},
		},
	}

	text, err := r.geminiRequest(systemPrompt, contents, "application/json")
	if err != nil {
		return "", "", "", "", err
	}

	var result struct {
		Transcript    string `json:"transcript"`
		RecipientHint string `json:"recipientHint"`
		Subject       string `json:"subject"`
		Body          string `json:"body"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		return "", "", "", "", fmt.Errorf("failed to parse voice response: %w", err)
	}
	return result.Transcript, result.RecipientHint, result.Subject, result.Body, nil
}

// RefineRequest refines a draft request based on instructions.
func (r *Rotator) RefineRequest(draftJSON string, instructions string, audioData []byte) (string, error) {
	systemPrompt := "Ти — юрист. Внеси ПРАВКИ у цей чернетка запиту на основі інструкцій. Поверни оновлений JSON."

	parts := []interface{}{
		map[string]string{"text": "Поточний запит: " + draftJSON},
		map[string]string{"text": "Інструкція з виправлення: " + instructions},
	}

	if audioData != nil {
		encoded := base64.StdEncoding.EncodeToString(audioData)
		parts = append(parts, map[string]interface{}{
			"inlineData": map[string]string{
				"mimeType": "audio/ogg",
				"data":     encoded,
			},
		})
	}

	contents := []interface{}{
		map[string]interface{}{
			"role":  "user",
			"parts": parts,
		},
	}

	return r.geminiRequest(systemPrompt, contents, "application/json")
}

// GenerateSocialPost generates a social media post from user text.
func (r *Rotator) GenerateSocialPost(text, tone string, photoBase64 []byte) (string, error) {
	toneMap := map[string]string{
		"sharp":   "ГОСТРО",
		"formal":  "ОФІЦІЙНО",
		"grammar": "КОРЕКТУРА",
	}
	toneLabel, ok := toneMap[tone]
	if !ok {
		toneLabel = "ОФІЦІЙНО"
	}

	systemPrompt := "Ти — копірайтер. Пиши тільки Markdown."

	parts := []interface{}{
		map[string]string{"text": "СТИЛЬ: " + toneLabel + "\n\nТекст:\n" + text},
	}

	if photoBase64 != nil {
		parts = append(parts, map[string]interface{}{
			"inlineData": map[string]string{
				"mimeType": "image/jpeg",
				"data":     string(photoBase64),
			},
		})
	}

	contents := []interface{}{
		map[string]interface{}{
			"role":  "user",
			"parts": parts,
		},
	}

	return r.geminiRequest(systemPrompt, contents, "")
}

// ValidateSubmission checks a post for safety.
func (r *Rotator) ValidateSubmission(text string, photoBase64 []byte) (bool, string, error) {
	parts := []interface{}{
		map[string]string{"text": "Перевір пост на безпеку: " + text},
	}

	contents := []interface{}{
		map[string]interface{}{
			"role":  "user",
			"parts": parts,
		},
	}

	result, err := r.geminiRequest("Перевіряй на наявність ПІБ, адрес, телефонів та інших персональних даних. Поверни JSON: {\"isSafe\": bool, \"reason\": string}", contents, "application/json")
	if err != nil {
		return true, "Default pass", nil
	}

	var parsed struct {
		IsSafe bool   `json:"isSafe"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(result)), &parsed); err != nil {
		return true, "Default pass", nil
	}
	return parsed.IsSafe, parsed.Reason, nil
}

// cleanJSON removes markdown code fences from AI responses.
func cleanJSON(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

// GenerateFromDescription generates a complete FOI request template from a short description,
// with references to the Ukrainian Law on Access to Public Information (ЗУ № 2939-VI).
func (r *Rotator) GenerateFromDescription(description string) (subject, body string, lawRefs []map[string]string, recipientHint string, err error) {
	systemPrompt := `Ти — юрист-помічник з доступу до публічної інформації в Україні.
На основі короткого опису користувача, згенеруй повний запит на інформацію згідно Закону України "Про доступ до публічної інформації" № 2939-VI.

ОБОВ'ЯЗКОВО поверни валідний JSON у такому форматі:
{
  "subject": "тема запиту (коротко, формально)",
  "body": "текст запиту (формальний стиль, з посиланнями на конкретні статті Закону № 2939-VI)",
  "lawRefs": [
    {"article": "Стаття N", "title": "Назва статті", "relevance": "Чому вона стосується цього запиту"}
  ],
  "recipientHint": "тип органу (наприклад: місцева рада, ОДА, поліція, міністерство)"
}

ПРАВИЛА:
1. Текст запиту має бути формальним і юридично коректним українською мовою.
2. ОБОВ'ЯЗКОВО посилайся на конкретні статті ЗУ "Про доступ до публічної інформації" № 2939-VI.
3. Згадай строк відповіді — 5 робочих днів (ст. 17).
4. Згадай відповідальність за порушення (ст. 22).
5. НЕ включай персональні дані (ім'я, адресу) — вони додаються окремо.
6. lawRefs має містити мінімум 3 релевантні статті.
7. КАТЕГОРИЧНО ЗАБОРОНЕНО вказувати email-адреси. Використовуй фразу: "Відповідь прошу надіслати в електронному вигляді на адресу електронної пошти, з якої надіслано цей запит".`

	contents := []interface{}{
		map[string]interface{}{
			"role": "user",
			"parts": []map[string]string{
				{"text": "Опис: " + description},
			},
		},
	}

	text, err := r.geminiRequest(systemPrompt, contents, "application/json")
	if err != nil {
		return "", "", nil, "", err
	}

	var result struct {
		Subject       string              `json:"subject"`
		Body          string              `json:"body"`
		LawRefs       []map[string]string `json:"lawRefs"`
		RecipientHint string              `json:"recipientHint"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(text)), &result); err != nil {
		return "", "", nil, "", fmt.Errorf("failed to parse AI response: %w", err)
	}
	return result.Subject, result.Body, result.LawRefs, result.RecipientHint, nil
}
