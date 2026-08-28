package dostup

// Подача уточнення (follow-up) в ту же гілку запиту на порталі
// «Доступ до правди». Протокол (Alavetelli, проверен на живом портале):
//
//   1) GET  /request/<slug>/followups/new
//        → форма: action=/request/<slug>/followups/preview?request_id=<id>,
//          authenticity_token, префикс подписи в textarea;
//   2) POST preview (outgoing_message[body], what_doing=normal_sort,
//      submitted_followup=1) → страница предпросмотра с новым токеном;
//   3) POST /request/<slug>/followups?request_id=<id>
//      (preview=0, submit=«Надіслати повідомлення») → сообщение уходит
//      в ту же публичную гілку.
//
// Важно: followup доступен только владельцу запроса (аккаунту бота) —
// на чужие запросы портал отвечает «Sorry, but only … can do that».

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	reFollowupForm    = regexp.MustCompile(`(?s)<form[^>]*id="followup_form"[^>]*action="([^"]+)"`)
	reFollowupToken   = regexp.MustCompile(`name="authenticity_token" value="([^"]+)"`)
	reFollowupPreview = regexp.MustCompile(`(?s)<form[^>]*id="preview_form"[^>]*action="([^"]+)"`)
	reRequestID       = regexp.MustCompile(`request_id=(\d+)`)
)

// ErrNotFollowupOwner — запрос принадлежит другому аккаунту портала.
var ErrNotFollowupOwner = fmt.Errorf("dostup: followup доступен лише власнику запиту (запит подано з іншого акаунта)")

// SubmitFollowUp публикует уточнение в гилку запроса requestSlug.
// text — сообщение от пользователя (подпись добавляет бот).
// Возвращает URL гилки (без #followup).
func (c *Client) SubmitFollowUp(requestSlug, text string) (string, error) {
	if err := c.EnsureSession(); err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("dostup: пустое сообщение для гілки")
	}

	// Шаг 1: страница формы
	page, code, err := c.get("/request/" + requestSlug + "/followups/new")
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("dostup: форма followup: HTTP %d", code)
	}
	if isRateLimited(page) {
		return "", ErrRateLimited
	}
	if strings.Contains(page, "can do that") {
		return "", ErrNotFollowupOwner
	}
	mf := reFollowupForm.FindStringSubmatch(page)
	mt := reFollowupToken.FindStringSubmatch(page)
	if mf == nil || mt == nil {
		return "", fmt.Errorf("dostup: форма followup не найдена (запрос уже закрыт или принадлежит другому аккаунту)")
	}
	previewAction := mf[1]
	token1 := mt[1]

	// Шаг 2: предпросмотр
	previewForm := url.Values{
		"utf8":                         {"✓"},
		"authenticity_token":           {token1},
		"outgoing_message[body]":       {text},
		"outgoing_message[what_doing]": {"normal_sort"},
		"submitted_followup":           {"1"},
		"commit":                       {"Попередній перегляд повідомлення"},
	}
	previewPage, code, err := c.post(previewAction, previewForm)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("dostup: preview followup: HTTP %d", code)
	}
	if isRateLimited(previewPage) {
		return "", ErrRateLimited
	}
	mf2 := reFollowupPreview.FindStringSubmatch(previewPage)
	mt2 := reFollowupToken.FindStringSubmatch(previewPage)
	if mf2 == nil || mt2 == nil {
		return "", fmt.Errorf("dostup: предпросмотр followup не прошёл валидацию")
	}
	finalAction := mf2[1]
	token2 := mt2[1]

	// Шаг 3: финальная отправка
	sendForm := url.Values{
		"utf8":                         {"✓"},
		"authenticity_token":           {token2},
		"outgoing_message[body]":       {text},
		"outgoing_message[what_doing]": {"normal_sort"},
		"submitted_followup":           {"1"},
		"preview":                      {"0"},
		"submit":                       {"Надіслати повідомлення"},
	}
	sendPage, code, err := c.post(finalAction, sendForm)
	if err != nil {
		return "", err
	}
	if code == 500 || isRateLimited(sendPage) {
		return "", ErrRateLimited
	}
	if strings.Contains(sendPage, "can do that") {
		return "", ErrNotFollowupOwner
	}

	// Сообщение добавлено в гилку — возвращаем публичную ссылку на запрос.
	return c.BaseURL + "/request/" + requestSlug, nil
}
