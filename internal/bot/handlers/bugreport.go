package handlers

import (
	"fmt"
	"log"
	"strings"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/session"
)

// BugReportModule handles bug/error reports from users, forwarding them to the admin.
type BugReportModule struct {
	deps    *Deps
	bot     *tb.Bot
	analyze *AnalyzeModule // делегирование фото письма для розбора (ТЗ №6)
}

func NewBugReportModule(deps *Deps) *BugReportModule {
	return &BugReportModule{deps: deps, bot: deps.Bot}
}

// SetAnalyzeModule подключает модуль розбора: фото письма органа в шаге
// analyze:* идёт на AI-анализ, а не админу как «баг-репорт».
func (m *BugReportModule) SetAnalyzeModule(a *AnalyzeModule) {
	m.analyze = a
}

func (m *BugReportModule) Name() string       { return "bugreport" }
func (m *BugReportModule) StepPrefix() string { return "bugreport:" }

func (m *BugReportModule) Register() {
	// Text button in main menu
	m.bot.Handle("🐞 Повідомити про помилку", safeHandler("bugreport_btn", m.handleBugReportBtn))

	// Media handlers for photo, video, document (OnVoice is owned by voice.go)
	m.bot.Handle(tb.OnPhoto, safeHandler("bugreport_photo", m.handleBugReportMedia))
	m.bot.Handle(tb.OnVideo, safeHandler("bugreport_video", m.handleBugReportMedia))
	m.bot.Handle(tb.OnDocument, safeHandler("bugreport_doc", m.handleBugReportMedia))
}

func (m *BugReportModule) handleBugReportBtn(c tb.Context) error {
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "bugreport:waiting"
	saveSession(m.deps, c)

	return c.Send(
		"🐞 Опишіть проблему або надішліть скріншот/голосове повідомлення.\n\n" +
			"Ви можете надіслати текст, фото, голосове або відео.\n" +
			"Для скасування: /cancel",
	)
}

// buildBugReportAdminHTML — карточка багрепорта для админа в HTML
// с экранированием ВСЕХ пользовательских данных. Раньше текст собирался
// в Markdown без экранирования: имя/юзернейм с «_», «*» или «[» ломали
// парсинг (Bad Request: can't parse entities), и репорт ТИХО терялся —
// пользователь видел «Дякуємо! Ваш звіт відправлено», а админ ничего
// не получал (реальный случай 30.08.2026, 22:20).
func buildBugReportAdminHTML(username string, userID int64, firstName, lastName, report string) string {
	name := strings.TrimSpace(firstName + " " + lastName)
	if name == "" {
		name = "—"
	}
	var at string
	if username != "" {
		at = "@" + username
	} else {
		at = "—"
	}
	return fmt.Sprintf(
		"🐞 <b>Звіт про помилку</b>\n\n👤 Від: %s (ID: %d)\n👤 Ім'я: %s\n\n📝 %s",
		htmlEscape(at), userID, htmlEscape(name), htmlEscape(report))
}

// sendAdminReport — доставка репорта админу с гарантией: сначала HTML
// (экранированный), при сетевой/API ошибке — повтор без форматирования,
// чтобы репорт не терялся никогда.
func (m *BugReportModule) sendAdminReport(text string) error {
	if _, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), text, tb.ModeHTML); err != nil {
		log.Printf("[BUGREPORT] HTML send failed (%v), retrying plain", err)
		_, err2 := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), text)
		if err2 != nil {
			return err2
		}
	}
	return nil
}

// HandleBugReportMedia processes media messages when step is "bugreport:waiting".
// This is called both from our own OnPhoto/OnVideo/OnDocument handlers AND
// from voice.go's OnVoice handler (delegated).
func (m *BugReportModule) HandleBugReportMedia(c tb.Context) error {
	if m.deps.Cfg.AdminID == 0 {
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "idle"
		saveSession(m.deps, c)
		return c.Send("⚠️ Адміністратор не налаштований. Звіт не може бути відправлений.")
	}

	user := c.Sender()
	msg := c.Message()

	// Карточка для админа: HTML + экранирование (см. buildBugReportAdminHTML)
	prefix := buildBugReportAdminHTML(user.Username, user.ID, user.FirstName, user.LastName, "(медіа — див. нижче)")

	// Send text-based notification to admin first
	if err := m.sendAdminReport(prefix); err != nil {
		log.Printf("[BUGREPORT] failed to send prefix to admin: %v", err)
	}

	// Forward the actual message (photo/voice/video/document) to admin
	if msg.Photo != nil {
		caption := msg.Caption
		_, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), msg.Photo, caption)
		if err != nil {
			log.Printf("[BUGREPORT] failed to forward photo to admin: %v", err)
		}
	} else if msg.Voice != nil {
		_, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), msg.Voice)
		if err != nil {
			log.Printf("[BUGREPORT] failed to forward voice to admin: %v", err)
		}
	} else if msg.Video != nil {
		caption := msg.Caption
		_, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), msg.Video, caption)
		if err != nil {
			log.Printf("[BUGREPORT] failed to forward video to admin: %v", err)
		}
	} else if msg.Document != nil {
		caption := msg.Caption
		_, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), msg.Document, caption)
		if err != nil {
			log.Printf("[BUGREPORT] failed to forward document to admin: %v", err)
		}
	}

	// Reset session and notify user
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "idle"
	saveSession(m.deps, c)

	kb := MainMenuKeyboard(m.deps.Cfg, c.Sender().ID)
	return c.Send("Дякуємо! Ваш звіт відправлено розробникам. 🙏", kb)
}

// handleBugReportMedia is the handler for OnPhoto/OnVideo/OnDocument callbacks.
// It checks the session step and only acts when in "bugreport:waiting".
func (m *BugReportModule) handleBugReportMedia(c tb.Context) error {
	sess := c.Get("session").(*session.SessionData)
	if sess.Step != "bugreport:waiting" {
		// ТЗ №6: фото/видео в шаге розбора — передаём анализатору
		if m.analyze != nil && strings.HasPrefix(sess.Step, "analyze:") {
			return m.analyze.HandleMedia(c)
		}
		return nil // not in bug report mode — ignore
	}
	return m.HandleBugReportMedia(c)
}

// HandleText processes text-based bug reports when step is bugreport:waiting.
func (m *BugReportModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	if step != "bugreport:waiting" {
		return false, nil
	}

	if m.deps.Cfg.AdminID == 0 {
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "idle"
		saveSession(m.deps, c)
		return true, c.Send("⚠️ Адміністратор не налаштований. Звіт не може бути відправлений.")
	}

	user := c.Sender()

	// Forward text report to admin: HTML + экранирование + plain-fallback
	adminText := buildBugReportAdminHTML(user.Username, user.ID, user.FirstName, user.LastName, text)
	if err := m.sendAdminReport(adminText); err != nil {
		log.Printf("[BUGREPORT] failed to send text report to admin: %v", err)
	}

	// Reset session and notify user
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "idle"
	saveSession(m.deps, c)

	kb := MainMenuKeyboard(m.deps.Cfg, c.Sender().ID)
	return true, c.Send("Дякуємо! Ваш звіт відправлено розробникам. 🙏", kb)
}
