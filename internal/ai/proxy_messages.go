package ai

// Клиент AI-прокси в формате Anthropic Messages API (POST /v1/messages).
//
// Почему: консоль Antigravity (прокси на проде) после обновления убрала
// OpenAI-совместимый эндпоинт /v1/chat/completions и теперь говорит только
// на /v1/messages. Старый клиент получал 404 на каждый запрос, молча
// переходил на прямой Gemini — прокси (локальный, быстрый, с новыми
// моделями) простаивал.
//
// Здесь же — стриминг (SSE): дельты текста приходят по мере генерации
// и передаются в колбэк onChunk (пилот «стриминга AI-ответов» из
// дорожной карты аудита). Блокирующий и стриминговый запрос делят один
// код построения тела; отличается только флаг stream и разбор ответа.
//
// Совместимость: если /v1/messages вернёт 404 (старый прокси или другой
// роутер, говорящий только на OpenAI-формате), клиент автоматически
// переключается на прежний /v1/chat/completions. Выбор эндпоинта
// «липкий»: запоминается, чтобы не ходить дважды на каждый запрос.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Эндпоинты прокси (липкий выбор хранится в proxyState.endpoint).
const (
	proxyEndpointUnknown  = "" // ещё не известно — пробуем современный первым
	proxyEndpointMessages = "messages"
	proxyEndpointOpenAI   = "openai"
)

// proxyHTTPError — HTTP-ошибка прокси с кодом состояния; по коду 404
// принимается решение об откате на другой эндпоинт.
type proxyHTTPError struct {
	Status int
	Body   string
}

func (e *proxyHTTPError) Error() string {
	return fmt.Sprintf("proxy error %d: %s", e.Status, e.Body)
}

// proxyEndpoint — текущий липкий выбор эндпоинта.
func (r *Rotator) proxyEndpoint() string {
	if r.proxyState == nil {
		return proxyEndpointUnknown
	}
	r.proxyState.mu.Lock()
	defer r.proxyState.mu.Unlock()
	return r.proxyState.endpoint
}

// setProxyEndpoint запоминает рабочий эндпоинт.
func (r *Rotator) setProxyEndpoint(e string) {
	if r.proxyState == nil {
		return
	}
	r.proxyState.mu.Lock()
	defer r.proxyState.mu.Unlock()
	r.proxyState.endpoint = e
}

// ---------------------------------------------------------------------------
// Формат Anthropic Messages API
// ---------------------------------------------------------------------------

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type   string                `json:"type"` // "text" | "image"
	Text   string                `json:"text,omitempty"`
	Source *anthropicImageSource `json:"source,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"` // "base64"
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// anthropicResponse — блокирующий ответ: content — список блоков;
// блоки "text" содержат текст, "thinking" — размышления модели (игнорируем).
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// proxyChatMessages — запрос к /v1/messages (стриминговый или блокирующий).
func (r *Rotator) proxyChatMessages(base, systemPrompt string, parts []proxyPart, media, stream bool, onChunk func(string)) (string, error) {
	blocks := make([]anthropicContentBlock, 0, len(parts))
	for _, p := range parts {
		if p.Text != "" {
			blocks = append(blocks, anthropicContentBlock{Type: "text", Text: p.Text})
			continue
		}
		if p.ImageURL != nil {
			mime, data := splitDataURL(p.ImageURL.URL)
			if mime == "" || data == "" {
				continue
			}
			blocks = append(blocks, anthropicContentBlock{
				Type:   "image",
				Source: &anthropicImageSource{Type: "base64", MediaType: mime, Data: data},
			})
		}
	}
	if len(blocks) == 0 {
		return "", fmt.Errorf("proxy: пустой запрос")
	}

	reqBody := anthropicRequest{
		Model:     r.proxyModelFor(media, false),
		MaxTokens: 8192,
		System:    systemPrompt,
		Stream:    stream,
		Messages:  []anthropicMessage{{Role: "user", Content: blocks}},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", base+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
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

	if stream {
		return readAnthropicSSE(resp.Body, onChunk)
	}

	var parsed anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("proxy: decode: %w", err)
	}
	var sb strings.Builder
	for _, b := range parsed.Content {
		if b.Type == "text" && b.Text != "" {
			sb.WriteString(b.Text)
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("proxy: пустой ответ")
	}
	return sb.String(), nil
}

// readAnthropicSSE разбирает SSE-поток /v1/messages. Текстовые дельты
// передаются в onChunk по мере поступления; блоки размышлений (thinking)
// пропускаются. Событие error прерывает чтение ошибкой.
func readAnthropicSSE(body io.Reader, onChunk func(string)) (string, error) {
	var full strings.Builder
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var streamErr error
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Delta *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue // незнакомое событие не должно ронять поток
		}
		switch {
		case ev.Error != nil && ev.Error.Message != "":
			streamErr = fmt.Errorf("proxy stream: %s", ev.Error.Message)
		case ev.Type == "content_block_delta" && ev.Delta != nil && ev.Delta.Type == "text_delta":
			if ev.Delta.Text != "" {
				full.WriteString(ev.Delta.Text)
				if onChunk != nil {
					onChunk(ev.Delta.Text)
				}
			}
		case ev.Type == "message_stop":
			// сервер закроет поток сам — выходим сразу
			if streamErr != nil {
				return full.String(), streamErr
			}
			if full.Len() == 0 {
				return "", fmt.Errorf("proxy: пустой ответ")
			}
			return full.String(), nil
		}
	}
	if streamErr != nil {
		return full.String(), streamErr
	}
	if full.Len() == 0 {
		return "", fmt.Errorf("proxy: пустой ответ")
	}
	return full.String(), nil
}

// ---------------------------------------------------------------------------
// Разбор contents (структура Gemini) в части прокси + вспомогательные
// ---------------------------------------------------------------------------

// collectProxyParts собирает текстовые и мультимедийные части из contents.
// Части приходят в двух видах: map[string]string (текстовые методы —
// ImproveRequest, GenerateFromDescription, AnalyzeRefusal…) и
// map[string]interface{} (мультимедийные — VoiceToRequest и др.).
func collectProxyParts(contents []interface{}) ([]proxyPart, error) {
	var parts []proxyPart
	for _, cItf := range contents {
		cm, ok := cItf.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := cm["role"].(string)
		if role == "" || role == "user" {
			role = "user"
		}
		var inner []interface{}
		switch p := cm["parts"].(type) {
		case []interface{}:
			inner = p
		case []map[string]string:
			for _, m := range p {
				inner = append(inner, m)
			}
		}
		for _, pItf := range inner {
			var txt string
			var inline map[string]string
			switch pm := pItf.(type) {
			case map[string]string:
				txt = pm["text"]
			case map[string]interface{}:
				if t, ok := pm["text"].(string); ok {
					txt = t
				}
				if inl, ok := pm["inlineData"].(map[string]string); ok {
					inline = inl
				} else if inlAny, ok := pm["inlineData"].(map[string]interface{}); ok {
					mime, _ := inlAny["mimeType"].(string)
					data, _ := inlAny["data"].(string)
					if mime != "" && data != "" {
						inline = map[string]string{"mimeType": mime, "data": data}
					}
				}
			default:
				continue
			}
			if txt != "" {
				parts = append(parts, proxyPart{Type: "text", Text: txt})
				continue
			}
			if len(inline) > 0 {
				mime := inline["mimeType"]
				if mime == "" {
					mime = "audio/ogg"
				}
				parts = append(parts, proxyPart{
					Type:     "input_audio",
					ImageURL: &proxyImageURL{URL: "data:" + mime + ";base64," + inline["data"]},
				})
			}
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("proxy: пустой запрос")
	}
	return parts, nil
}

// contentsHasAudio — есть ли в запросе аудио (голосовые сообщения).
// Формат Anthropic Messages API официально поддерживает только картинки,
// поэтому аудио идёт напрямую в Gemini (а прокси — лишь запасной вариант,
// без влияния на общий счётчик сбоев).
func contentsHasAudio(contents []interface{}) bool {
	for _, cItf := range contents {
		cm, ok := cItf.(map[string]interface{})
		if !ok {
			continue
		}
		var inner []interface{}
		switch p := cm["parts"].(type) {
		case []interface{}:
			inner = p
		case []map[string]string:
			continue // только текст
		}
		for _, pItf := range inner {
			pm, ok := pItf.(map[string]interface{})
			if !ok {
				continue
			}
			mime := ""
			if inl, ok := pm["inlineData"].(map[string]string); ok {
				mime = inl["mimeType"]
			} else if inlAny, ok := pm["inlineData"].(map[string]interface{}); ok {
				mime, _ = inlAny["mimeType"].(string)
			}
			if strings.HasPrefix(mime, "audio/") {
				return true
			}
		}
	}
	return false
}

// splitDataURL разбирает "data:image/jpeg;base64,AAAA" на MIME и данные.
func splitDataURL(u string) (mime, data string) {
	if !strings.HasPrefix(u, "data:") {
		return "", ""
	}
	rest := strings.TrimPrefix(u, "data:")
	idx := strings.Index(rest, ",")
	if idx < 0 {
		return "", ""
	}
	meta := rest[:idx]
	data = rest[idx+1:]
	if i := strings.Index(meta, ";"); i >= 0 {
		mime = meta[:i]
	} else {
		mime = meta
	}
	return mime, data
}

// ---------------------------------------------------------------------------
// Диспетчер запросов к прокси
// ---------------------------------------------------------------------------

// proxyChat выполняет запрос через прокси: сначала современный
// /v1/messages, при 404 — откат на /v1/chat/completions (старые прокси).
// Выбор эндпоинта липкий: после первого успеха ходим сразу в рабочий.
func (r *Rotator) proxyChat(systemPrompt string, contents []interface{}, media bool) (string, error) {
	base := r.proxyBase()
	if base == "" {
		return "", fmt.Errorf("proxy: не настроен")
	}
	parts, err := collectProxyParts(contents)
	if err != nil {
		return "", err
	}

	if r.proxyEndpoint() != proxyEndpointOpenAI {
		text, merr := r.proxyChatMessages(base, systemPrompt, parts, media, false, nil)
		if merr == nil {
			r.setProxyEndpoint(proxyEndpointMessages)
			return text, nil
		}
		var httpErr *proxyHTTPError
		if merr != nil && asProxyHTTPError(merr, &httpErr) && httpErr.Status == 404 {
			// Прокси не знает нового формата — пробуем прежний эндпоинт.
			if text2, oerr := r.proxyChatOpenAI(base, systemPrompt, parts, media); oerr == nil {
				r.setProxyEndpoint(proxyEndpointOpenAI)
				return text2, nil
			}
		}
		return "", merr
	}
	return r.proxyChatOpenAI(base, systemPrompt, parts, media)
}

// asProxyHTTPError — errors.As для proxyHTTPError (без импорта errors
// в каждом месте).
func asProxyHTTPError(err error, target **proxyHTTPError) bool {
	for err != nil {
		if e, ok := err.(*proxyHTTPError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// proxyChatStream — стриминговый запрос через прокси (/v1/messages, SSE).
// Дельты текста уходят в onChunk по мере генерации.
func (r *Rotator) proxyChatStream(systemPrompt string, contents []interface{}, media bool, onChunk func(string)) (string, error) {
	base := r.proxyBase()
	if base == "" {
		return "", fmt.Errorf("proxy: не настроен")
	}
	parts, err := collectProxyParts(contents)
	if err != nil {
		return "", err
	}
	if r.proxyEndpoint() == proxyEndpointOpenAI {
		// Старый эндпоинт стриминга не умеет — вызывающий код пойдёт
		// напрямую в Gemini.
		return "", &proxyHTTPError{Status: 501, Body: "openai endpoint: streaming not supported"}
	}
	return r.proxyChatMessages(base, systemPrompt, parts, media, true, onChunk)
}
