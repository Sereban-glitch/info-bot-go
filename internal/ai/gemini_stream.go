package ai

// Прямой стриминг Gemini (streamGenerateContent?alt=sse): текст приходит
// порциями по мере генерации и передаётся в onChunk. Используется пилотом
// стриминга как запасной путь, когда прокси недоступен, — и основным,
// когда прокси вовсе не настроен.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// geminiBaseURL — базовый URL прямого Gemini (переопределяется в тестах).
func (r *Rotator) geminiBaseURL() string {
	if r.geminiBase == "" {
		return "https://generativelanguage.googleapis.com/v1beta"
	}
	return r.geminiBase
}

// geminiRequestStream — стриминговый generateContent. Дельты текста
// передаются в onChunk по мере поступления; возвращается полный текст.
// Ошибки HTTP и лимиты не «доламываются» здесь: вызывающий код при сбое
// стрима уходит в обычный блокирующий запрос (geminiRequest), где есть
// полный разбор 429/403 и ротация ключей.
func (r *Rotator) geminiRequestStream(systemPrompt string, contents []interface{}, responseMIME string, onChunk func(string)) (string, error) {
	key, err := r.pick()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", r.geminiBaseURL(), r.model, key)

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
		return "", fmt.Errorf("gemini stream request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return "", fmt.Errorf("gemini stream error %d: %s", resp.StatusCode, strings.TrimSpace(buf.String()))
	}

	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		chunk := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if chunk == "" {
			continue
		}
		var ev struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal([]byte(chunk), &ev); err != nil {
			continue // незнакомое событие не должно ронять поток
		}
		for _, cand := range ev.Candidates {
			for _, p := range cand.Content.Parts {
				if p.Text != "" {
					full.WriteString(p.Text)
					if onChunk != nil {
						onChunk(p.Text)
					}
				}
			}
		}
	}
	if full.Len() == 0 {
		return "", fmt.Errorf("gemini stream: пустой ответ")
	}
	r.markSuccess(key)
	return full.String(), nil
}
