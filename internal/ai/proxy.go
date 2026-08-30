package ai

// AI-прокси: маршрут запросов к Gemini через локальный/удалённый роутер
// (например antigravity-claude-proxy). Прокси принимает OpenAI-совместимый
// chat/completions-запрос и сам ходит к модели; при сбое прокси бот
// автоматически возвращается к прямому доступу к Gemini API.
//
// Логика соответствия бинарнику phase2:
//   - proxyAvailable — прокси включён и не «на паузе» после серии сбоев;
//   - proxyChat      — POST <base>/v1/chat/completions c Bearer-ключом;
//   - proxyModelFor  — выбор модели: media (голос/фото) или текстовая;
//   - proxyToken     — выдача ключа прокси (или прямого ключа Gemini).

import (
	"bytes"
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
	proxyPauseAfterFailures = 3                // подряд — и прокси уходит на паузу
	proxyPauseDuration      = 10 * time.Minute // пауза перед новой попыткой
)

// ProxyConfig — настройки прокси (передаются из config.Config).
type ProxyConfig struct {
	URL           string // базовый URL, например http://127.0.0.1:18080
	Key           string // Bearer-ключ (опционально)
	Model         string // модель для текстовых запросов
	FallbackModel string // запасная модель
	MediaModel    string // модель для мультимедиа (голос, фото)
}

// proxyState — состояние прокси (счётчик сбоев, пауза, липкий выбор
// эндпоинта: messages | openai).
type proxyState struct {
	mu             sync.Mutex
	consecFailures int
	pausedUntil    time.Time
	endpoint       string
}

// SetProxy включает прокси-маршрутизацию.
func (r *Rotator) SetProxy(cfg ProxyConfig) {
	if cfg.URL == "" {
		return
	}
	r.proxy = &cfg
	r.proxyState = &proxyState{}
	model, fb, media := cfg.Model, cfg.FallbackModel, cfg.MediaModel
	if model == "" {
		model = r.model
	}
	if fb == "" {
		fb = r.fallbackModel
	}
	if media == "" {
		media = model
	}
	log.Printf("[AI] proxy enabled: %s (model=%s, fallback=%s, media=%s)", cfg.URL, model, fb, media)
}

// proxyAvailable — прокси настроен и не на паузе.
func (r *Rotator) proxyAvailable() bool {
	if r.proxy == nil || r.proxy.URL == "" {
		return false
	}
	r.proxyState.mu.Lock()
	defer r.proxyState.mu.Unlock()
	return time.Now().After(r.proxyState.pausedUntil)
}

// proxyBase — базовый URL без хвостового слэша.
func (r *Rotator) proxyBase() string {
	if r.proxy == nil {
		return ""
	}
	return strings.TrimRight(r.proxy.URL, "/")
}

// proxyModelFor — модель прокси для запроса; media=true — мультимедийная модель.
func (r *Rotator) proxyModelFor(media bool, fallback bool) string {
	if r.proxy == nil {
		return ""
	}
	if fallback {
		if r.proxy.FallbackModel != "" {
			return r.proxy.FallbackModel
		}
		return r.proxy.Model
	}
	if media && r.proxy.MediaModel != "" {
		return r.proxy.MediaModel
	}
	if r.proxy.Model != "" {
		return r.proxy.Model
	}
	return r.model
}

// proxyToken — ключ прокси (может быть пустым — тогда без Authorization).
func (r *Rotator) proxyToken() string {
	if r.proxy == nil {
		return ""
	}
	return r.proxy.Key
}

// markProxyFailure фиксирует сбой прокси; после N сбоев подряд — пауза.
func (r *Rotator) markProxyFailure() {
	if r.proxyState == nil {
		return
	}
	r.proxyState.mu.Lock()
	defer r.proxyState.mu.Unlock()
	r.proxyState.consecFailures++
	if r.proxyState.consecFailures >= proxyPauseAfterFailures {
		r.proxyState.pausedUntil = time.Now().Add(proxyPauseDuration)
		log.Printf("[AI] proxy paused for %s after %d consecutive failures", proxyPauseDuration, r.proxyState.consecFailures)
	}
}

// markProxySuccess сбрасывает счётчик сбоев.
func (r *Rotator) markProxySuccess() {
	if r.proxyState == nil {
		return
	}
	r.proxyState.mu.Lock()
	defer r.proxyState.mu.Unlock()
	r.proxyState.consecFailures = 0
}

// proxyMessage — сообщение в формате OpenAI chat.
type proxyMessage struct {
	Role    string      `json:"role"`
	Content []proxyPart `json:"content,omitempty"`
}

type proxyPart struct {
	Type     string         `json:"type,omitempty"`
	Text     string         `json:"text,omitempty"`
	ImageURL *proxyImageURL `json:"image_url,omitempty"`
}

type proxyImageURL struct {
	URL string `json:"url"`
}

// proxyChatOpenAI выполняет запрос через прежний эндпоинт
// /v1/chat/completions (OpenAI-совместимый формат). Основной путь —
// /v1/messages (proxy_messages.go); сюда откатываемся, если прокси
// старый и нового формата не знает.
func (r *Rotator) proxyChatOpenAI(base, systemPrompt string, parts []proxyPart, media bool) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("proxy: пустой запрос")
	}

	messages := make([]proxyMessage, 0, 2)
	if systemPrompt != "" {
		messages = append(messages, proxyMessage{
			Role:    "system",
			Content: []proxyPart{{Type: "text", Text: systemPrompt}},
		})
	}
	messages = append(messages, proxyMessage{Role: "user", Content: parts})

	body := map[string]interface{}{
		"model":    r.proxyModelFor(media, false),
		"messages": messages,
	}
	raw, _ := json.Marshal(body)

	url := base + "/v1/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := r.proxyToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("proxy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return "", &proxyHTTPError{Status: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("proxy: decode: %w", err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("proxy: пустой ответ")
	}
	return parsed.Choices[0].Message.Content, nil
}

// tryProxyThenDirect — сначала прокси (если доступен), затем прямой Gemini.
// Аудио (голосовые) идёт напрямую: формат /v1/messages официально
// поддерживает только изображения, и провал аудио на прокси не должен
// «паузить» прокси для текстовых запросов. Если прямой Gemini не справился
// (например, лимит) — аудио всё же пробуем через прокси как последний шанс,
// не засчитывая сбой.
func (r *Rotator) tryProxyThenDirect(systemPrompt string, contents []interface{}, responseMIME string, media bool) (string, error) {
	audio := contentsHasAudio(contents)
	if r.proxyAvailable() && !audio {
		text, err := r.proxyChat(systemPrompt, contents, media)
		if err == nil {
			r.markProxySuccess()
			return text, nil
		}
		r.markProxyFailure()
		log.Printf("[AI] proxy failed (%v), falling back to direct Gemini", err)
	}
	text, derr := r.geminiRequest(systemPrompt, contents, responseMIME)
	if derr == nil {
		return text, nil
	}
	if audio && r.proxyAvailable() {
		if ptext, perr := r.proxyChat(systemPrompt, contents, media); perr == nil {
			r.markProxySuccess()
			return ptext, nil
		}
	}
	return "", derr
}

// tryProxyThenDirectStream — как tryProxyThenDirect, но с живой отдачей
// текста: дельты генерации передаются в onChunk по мере поступления
// (пилот стриминга). Если ни прокси, ни прямой Gemini не смогли
// стримить — прозрачно уходим в блокирующий вызов (без колбэка).
func (r *Rotator) tryProxyThenDirectStream(systemPrompt string, contents []interface{}, responseMIME string, media bool, onChunk func(string)) (string, error) {
	if onChunk == nil {
		return r.tryProxyThenDirect(systemPrompt, contents, responseMIME, media)
	}
	if r.proxyAvailable() && !contentsHasAudio(contents) {
		text, err := r.proxyChatStream(systemPrompt, contents, media, onChunk)
		if err == nil {
			r.markProxySuccess()
			return text, nil
		}
		r.markProxyFailure()
		log.Printf("[AI] proxy stream failed (%v), falling back to direct Gemini", err)
	}
	text, err := r.geminiRequestStream(systemPrompt, contents, responseMIME, onChunk)
	if err == nil {
		return text, nil
	}
	log.Printf("[AI] direct stream failed (%v), falling back to blocking call", err)
	return r.tryProxyThenDirect(systemPrompt, contents, responseMIME, media)
}
