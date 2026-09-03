package handlers

// FollowUpModule — уточнення (follow-up) у ту саму гілку запиту на порталі.
//
// Сценарії входа:
//   1. Команда /followup — список гилок пользователя → выбор → текст/голос;
//   2. Сгруппированное предложение от DostupSync (OfferOverdueReminders): строк ответа минул —
//      пользователь сразу получает кнопку «Дописати у гілку запиту»;
//   3. После ответа органа по существу — можно дописать вопрос в ту же гилку.
//
// Текст уточнения можно продиктовать голосом (HandleVoice → Gemini).
// Отправка — через dostup.SubmitFollowUp (та же публичная гилка, орган
// получает письмо, ответ виден на портале без регистрации).

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/dostup"
	"info-bot-go/internal/session"
)

// FollowUpModule handles follow-up messages to request threads.
type FollowUpModule struct {
	deps *Deps
	bot  *tb.Bot
}

// NewFollowUpModule создаёт модуль уточнений.
func NewFollowUpModule(deps *Deps) *FollowUpModule {
	return &FollowUpModule{deps: deps, bot: deps.Bot}
}

func (m *FollowUpModule) Name() string       { return "followup" }
func (m *FollowUpModule) StepPrefix() string { return "followup:" }

func (m *FollowUpModule) Register() {
	m.bot.Handle("/followup", safeHandler("followup", m.handleStart))

	pickBtn := tb.InlineButton{Unique: "fu_pick"}
	m.bot.Handle(&pickBtn, safeHandler("fu_pick", m.handlePick))

	sendBtn := tb.InlineButton{Unique: "fu_send"}
	m.bot.Handle(&sendBtn, safeHandler("fu_send", m.handleSend))

	editBtn := tb.InlineButton{Unique: "fu_edit"}
	m.bot.Handle(&editBtn, safeHandler("fu_edit", m.handleEdit))

	cancelBtn := tb.InlineButton{Unique: "fu_cancel"}
	m.bot.Handle(&cancelBtn, safeHandler("fu_cancel", m.handleCancel))

	writeBtn := tb.InlineButton{Unique: "fup_write"}
	m.bot.Handle(&writeBtn, safeHandler("fup_write", m.handleWriteMore))
}

// handleStart — /followup: показывает гилки для дописывания.
func (m *FollowUpModule) handleStart(c tb.Context) error {
	return m.OfferThreads(c, "Оберіть запит, до якого хочете дописати повідомлення — воно буде додано у ту саму гілку переписки з органом на порталі.")
}

// OfferThreads показывает список гилок пользователя с кнопками.
func (m *FollowUpModule) OfferThreads(c tb.Context, intro string) error {
	if m.deps.Dostup == nil {
		return c.Send("⚠️ Канал «Доступ до правди» не налаштований.")
	}
	if m.deps.FollowUps == nil {
		return c.Send("ℹ️ У вас ще немає гілок запитів для дописування. Створіть запит: /new")
	}
	threads := m.deps.FollowUps.List(c.Sender().ID, 8)
	if len(threads) == 0 {
		return c.Send("ℹ️ У вас ще немає гілок запитів для дописування. Створіть запит: /new")
	}

	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	for i, th := range threads {
		label := th.Subject
		if utf8RuneCount(label) > 48 {
			label = string([]rune(label)[:45]) + "..."
		}
		rows = append(rows, []tb.InlineButton{
			{Unique: "fu_pick", Text: fmt.Sprintf("💬 %s", label), Data: fmt.Sprintf("%d", i)},
		})
	}
	rows = append(rows, []tb.InlineButton{{Unique: "fu_cancel", Text: "❌ Закрити"}})
	kb.InlineKeyboard = rows

	sess := c.Get("session").(*session.SessionData)
	sess.Step = "followup:pick"
	saveSession(m.deps, c)
	return c.Send(fmt.Sprintf("<b>Гілки ваших запитів</b>\n\n%s", intro), kb, tb.ModeHTML)
}

// handlePick — выбрана гилка; спрашиваем текст уточнения.
func (m *FollowUpModule) handlePick(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	if m.deps.FollowUps == nil {
		return c.Send("❌ Гілки недоступні.")
	}
	idx := atoi(c.Callback().Data)
	threads := m.deps.FollowUps.List(c.Sender().ID, 8)
	if idx < 0 || idx >= len(threads) {
		return c.Send("❌ Гілку не знайдено. Спробуйте /followup ще раз.")
	}
	th := threads[idx]

	sess.FollowUp = &FollowUpDraft{
		RequestSlug: th.Slug,
		Subject:     th.Subject,
		Organ:       th.Organ,
		URL:         th.URL,
		PickIdx:     idx,
	}
	sess.Step = "followup:ask_text"
	saveSession(m.deps, c)

	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "fu_cancel", Text: "❌ Скасувати"}},
	}
	return c.Edit(fmt.Sprintf("🏛 <b>%s</b>\n📂 «%s»\n\n✍️ Коротко опишіть, що ви хочете уточнити чи попросити (можна голосом):",
		htmlEscape(th.Organ), htmlEscape(th.Subject)), kb, tb.ModeHTML)
}

// OverdueItem — просроченная гилка для сгруппированного напоминания.
type OverdueItem struct {
	Thread   FollowUpThread
	Deadline time.Time
}

// OfferOverdueReminders — сгруппированное приглашение дописать в
// просроченные гилки: ОДНО сообщение со всеми запитами пользователя
// (вызывается DostupSync не чаще раза в сутки на пользователя).
func (m *FollowUpModule) OfferOverdueReminders(userID int64, items []OverdueItem) {
	if len(items) == 0 {
		return
	}
	// Помечаем ДО отправки: даже если Telegram упадёт, не спамим
	// повторами каждые 20 минут — следующее напоминание завтра.
	if m.deps.FollowUps != nil {
		now := time.Now().Format(time.RFC3339)
		for _, it := range items {
			m.deps.FollowUps.MarkReminded(userID, it.Thread.Slug, now)
		}
	}
	target := userID
	if target == 0 {
		target = m.deps.Cfg.AdminID
	}
	if target == 0 {
		return
	}
	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "fu_pick", Text: "✍️ Дописати у гілку запиту", Data: "0"}},
		{{Unique: "fu_cancel", Text: "❌ Закрити"}},
	}

	var b strings.Builder
	b.WriteString("⚠️ <b>Запити потребують уваги</b>\n\n")
	for i, it := range items {
		b.WriteString(fmt.Sprintf("%d. 🏛 <b>%s</b> — «%s» (строк минув %s)\n   🔗 %s\n",
			i+1, htmlEscape(it.Thread.Organ), htmlEscape(it.Thread.Subject),
			it.Deadline.Format("02.01.2006"), htmlLink(it.Thread.URL)))
	}
	b.WriteString("\nМожна дописати повідомлення у ту ж гілку запиту — нагадати органу про себе чи уточнити запит. Це часто прискорює відповідь.")
	_, err := m.bot.Send(tb.ChatID(target), b.String(), kb, tb.ModeHTML, tb.NoPreview)
	if err != nil {
		log.Printf("[FOLLOWUP] grouped reminder send user=%d: %v", target, err)
	}
}

// handleWriteMore — «✍️ Дописати ще» после отправки: снова выбор гилки.
func (m *FollowUpModule) handleWriteMore(c tb.Context) error {
	_ = c.Respond()
	return m.OfferThreads(c, "Оберіть запит, до якого хочете дописати повідомлення — воно буде додано у ту саму гілку переписки з органом на порталі.")
}

// HandleText — текст уточнения (или новый текст при редактировании).
func (m *FollowUpModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	if step != "followup:ask_text" {
		return false, nil
	}
	sess := c.Get("session").(*session.SessionData)
	if sess.FollowUp == nil {
		return true, c.Send("❌ Чернетку втрачено. Почніть заново: /followup")
	}
	text = strings.TrimSpace(text)
	if utf8RuneCount(text) < 5 {
		return true, c.Send("❌ Повідомлення занадто коротке. Напишіть докладніше (можна голосом):")
	}
	if utf8RuneCount(text) > 3000 {
		text = text[:3000]
	}
	sess.FollowUp.Body = text
	saveSession(m.deps, c)
	return true, m.showConfirm(c, sess)
}

// HandleVoice — голосовое уточнение: расшифровка через Gemini.
func (m *FollowUpModule) HandleVoice(c tb.Context) (bool, error) {
	sess := c.Get("session").(*session.SessionData)
	if sess.Step != "followup:ask_text" || sess.FollowUp == nil {
		return false, nil
	}
	if m.deps.Gemini == nil || !m.deps.Gemini.Available() {
		return true, c.Send("⚠️ Голосова обробка вимкнена (немає AI). Напишіть текстом:")
	}

	vm := NewVoiceModule(m.deps)
	_ = c.Send("🎧 Слухаю та оброблюю ваше голосове...")
	audioData, err := vm.downloadVoice(c)
	if err != nil {
		return true, c.Send("⚠️ Не вдалося завантажити аудіо. Напишіть текстом:")
	}
	_, _, _, body, err := m.deps.Gemini.VoiceToRequest(audioData, "audio/ogg")
	if err != nil {
		return true, c.Send("⚠️ Не вдалося розпізнати. Напишіть текстом:")
	}
	if utf8RuneCount(body) > 3000 {
		body = body[:3000]
	}
	sess.FollowUp.Body = body
	saveSession(m.deps, c)
	return true, m.showConfirm(c, sess)
}

// showConfirm — экран подтверждения перед отправкой в гилку.
func (m *FollowUpModule) showConfirm(c tb.Context, sess *session.SessionData) error {
	fu := sess.FollowUp
	sign := session.SignatureName(sess.Profile)
	if sign == "" {
		sign = "Громадянин України"
	}
	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "fu_send", Text: "✅ Надіслати у гілку"}},
		{{Unique: "fu_edit", Text: "✏️ Змінити текст"}},
		{{Unique: "fu_cancel", Text: "❌ Скасувати"}},
	}
	sess.Step = "followup:confirm"
	saveSession(m.deps, c)
	return c.Send(fmt.Sprintf("<b>Повідомлення у гілку запиту</b>\n\n🏛 <b>%s</b>\n📂 «%s»\n\n--- <b>ТЕКСТ ПОВІДОМЛЕННЯ</b> ---\n%s\n-----------------\n✍️ Підпис: <b>%s</b>\n\nПовідомлення буде опубліковано у тій самій гілці на порталі. Після відправки я продовжу слідкувати за гілкою і повідомлю тут про нову відповідь органу.",
		htmlEscape(fu.Organ), htmlEscape(fu.Subject), htmlEscape(fu.Body), htmlEscape(sign)), kb, tb.ModeHTML)
}

// handleEdit — «✏️ Змінити текст».
func (m *FollowUpModule) handleEdit(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	if sess.FollowUp == nil {
		return c.Send("❌ Чернетку втрачено. Почніть заново: /followup")
	}
	sess.Step = "followup:ask_text"
	saveSession(m.deps, c)
	return c.Edit("✏️ Напишіть новий текст повідомлення (можна голосом):")
}

// handleCancel — «❌ Скасувати».
func (m *FollowUpModule) handleCancel(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "idle"
	sess.FollowUp = nil
	saveSession(m.deps, c)
	_ = c.Edit("❌ Запит відкликано.")
	return nil
}

// handleSend — финальная отправка уточнения через портал.
func (m *FollowUpModule) handleSend(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	if sess.FollowUp == nil || sess.FollowUp.RequestSlug == "" {
		return c.Send("❌ Чернетку втрачено. Почніть заново: /followup")
	}
	if m.deps.Dostup == nil {
		return c.Send("❌ Канал «Доступ до правди» не налаштований.")
	}
	fu := sess.FollowUp

	_ = c.Edit("⏳ Публікую повідомлення у гілку запиту...")

	// Текст с подписью (как в письмах органа)
	sign := session.SignatureName(sess.Profile)
	if sign == "" {
		sign = "Громадянин України"
	}
	text := fu.Body + "\n\nЗ повагою,\n" + sign + "\n" + time.Now().Format("02.01.2006")

	url, err := m.deps.Dostup.SubmitFollowUp(fu.RequestSlug, text)
	if err != nil {
		if errors.Is(err, dostup.ErrRateLimited) {
			return c.Edit("⏳ Портал обмежив частоту запитів («Забагато запитів»).\nНатисніть кнопку повторно через 3–5 хвилин — чернетка збережена.")
		}
		if errors.Is(err, dostup.ErrNotFollowupOwner) {
			return c.Edit("❌ Це запит подано з іншого акаунта порталу — дописати може лише власник. Оберіть іншу гілку: /followup")
		}
		log.Printf("[FOLLOWUP] send error %s: %v", fu.RequestSlug, err)
		return c.Edit(fmt.Sprintf("❌ Помилка надсилання: %s\n\nЧернетка збережена, спробуйте ще раз.", err))
	}

	// Обновляем гилку
	if m.deps.FollowUps != nil {
		m.deps.FollowUps.MarkFollowUpSent(c.Sender().ID, fu.RequestSlug, time.Now().Format(time.RFC3339))
	}

	sess.Step = "idle"
	sess.FollowUp = nil
	saveSession(m.deps, c)

	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "fup_write", Text: "✍️ Дописати ще"}},
	}
	return c.Edit(fmt.Sprintf("✅ <b>Дописано у гілку запиту</b>\n\n🏛 <b>%s</b>\n📂 «%s»\n\n🔗 Повна переписка (без реєстрації):\n%s\n\nЯ продовжую слідкувати за цією гілкою — повідомлю, коли орган надішле нову відповідь. Якщо прийде відповідь на ваше дописання, ви побачите її тут.",
		htmlEscape(fu.Organ), htmlEscape(fu.Subject), htmlLink(url)), kb, tb.ModeHTML)
}
