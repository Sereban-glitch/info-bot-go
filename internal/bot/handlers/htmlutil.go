package handlers

import (
	"fmt"
	"strings"
	"time"
)

// HTML-хелперы для Telegram-сообщений.
//
// Раньше сообщения отправлялись в ModeMarkdown — и подчёркивания
// в URL публичных страниц портала (…/request/pierielik_ieliektronnikh_adries)
// Telegram съедал как маркеры курсива, ломая ссылку. HTML-режим
// парсит только теги, поэтому URL и произвольный текст органов
// передаются без искажений.

// htmlEscape экранирует &, < и > для HTML-режима Telegram.
func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// htmlLink возвращает кликабельную ссылку, у которой видимый текст —
// сам URL (удобно копировать). Подчёркивания в слаге сохраняются.
func htmlLink(url string) string {
	esc := htmlEscape(url)
	return fmt.Sprintf(`<a href="%s">%s</a>`, esc, esc)
}

// formatReplyDeadline считает дедлайн ответа по существу:
// дата подачи + 5 рабочих дней (ст. 20 ЗУ «Про доступ до публічної інформації»).
func formatReplyDeadline(sentDateISO string) string {
	sent := time.Now()
	if t, err := time.Parse(time.RFC3339, sentDateISO); err == nil {
		sent = t
	}
	return addWorkingDays(sent, 5).Format("02.01.2006")
}
