package handlers

// Модуль антиспровокаційного скринінгу (ТЗ №10, 26-й модуль).
//
// Працює разом з internal/moderation:
//   • Check() — чиста функція, вирішує «чутливий/звичайний»;
//   • Store — черга moderation_queue.json (переживає рестарт);
//   • цей модуль — інтерфейс: картки власнику, кнопки ✅/❌,
//     /moderation, повідомлення користувачам, сповіщення про
//     кожну відправку.
//
// Потік: handleSubmit (портал) і handleSendConfirm (email) перед
// відправкою викликають Screen(). Чутливий запит → HoldDostup/
// HoldEmail: черга + картка власнику + «поставлено на перевірку»
// користувачу. ✅ → відправка тим самим механізмом, що й завжди
// (журнал, статистика, гілки, дедлайн), ❌ → користувач отримує
// готовий текст листа для самостійної подачі.
//
// ВАЖЛИВО (регресія Stars, Task 26): у telebot обробники
// зберігаються map-присвоєнням. Уніки кнопок mod_ok/mod_no і
// команда /moderation — нові, ні з ким не перетинаються.

import (
	"fmt"
	"log"
	"strings"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/moderation"
	"info-bot-go/internal/safego"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
)

// ModerationModule — 26-й модуль: скринінг + черга + інтерфейс рішень.
type ModerationModule struct {
	deps   *Deps
	bot    *tb.Bot
	store  *moderation.Store
	dostup *DostupModule // для відправки на портал після ✅
}

func NewModerationModule(deps *Deps) *ModerationModule {
	path := ""
	if deps.Cfg != nil && deps.Cfg.SessionDir != "" {
		path = deps.Cfg.SessionDir
	}
	if path != "" && !strings.HasSuffix(path, "/") {
		path += "/"
	}
	if path != "" {
		path += "moderation_queue.json"
	} else {
		path = "" // тести: тільки пам'ять
	}
	return &ModerationModule{
		deps:  deps,
		bot:   deps.Bot,
		store: moderation.NewStore(path),
	}
}

func (m *ModerationModule) Name() string { return "moderation" }

func (m *ModerationModule) Register() {
	okBtn := tb.InlineButton{Unique: "mod_ok"}
	m.bot.Handle(&okBtn, safeHandler("mod_ok", m.handleApprove))

	noBtn := tb.InlineButton{Unique: "mod_no"}
	m.bot.Handle(&noBtn, safeHandler("mod_no", m.handleReject))

	m.bot.Handle("/moderation", safeHandler("moderation", m.handleList))
}

// SetDostupModule — зв'язок для відправки схвалених запитів на портал
// (виклик після створення обох модулів, як voiceMod.SetBugReportModule).
func (m *ModerationModule) SetDostupModule(d *DostupModule) { m.dostup = d }

// Screening — увімкнений чи скринінг (MODERATION_ENABLED, за замовчуванням так).
func (m *ModerationModule) Screening() bool {
	return m.deps.Cfg == nil || m.deps.Cfg.ModerationEnabled
}

// Enabled — магазин черги існує (завжди true на проді).
func (m *ModerationModule) Store() *moderation.Store { return m.store }

// Start — після старту бота нагадати власнику про незрішені запити
// (наприклад, бот перезапущено, а рішення так і не прийнято).
func (m *ModerationModule) Start() {
	if m.deps.Cfg == nil || m.deps.Cfg.AdminID == 0 {
		return
	}
	safego.Go("moderation-boot", func() {
		time.Sleep(15 * time.Second) // дати боту піднятися
		n := m.store.PendingCount()
		if n == 0 {
			return
		}
		_, _ = m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID),
			fmt.Sprintf("🛡 <b>Нагадування:</b> на перевірці %d запит(ів) — /moderation", n), tb.ModeHTML)
	})
}

// ---------------------------------------------------------------------------
// Постановка на перевірку
// ---------------------------------------------------------------------------

// HoldDostup — чутливий запит каналом «Доступ до правди»: черга +
// картка власнику + повідомлення користувачу. Чернетку очищаємо
// (текст збережено в черзі — рішення по ньому приймається з картки).
func (m *ModerationModule) HoldDostup(c tb.Context, sess *session.SessionData, title, body string, v moderation.Verdict) error {
	it := m.store.Enqueue(moderation.Item{
		UserID:    c.Sender().ID,
		ChatID:    c.Chat().ID,
		TGName:    telegramName(c),
		TGUser:    telegramUsername(c),
		Signature: session.SignatureName(sess.Profile),
		Channel:   moderation.ChannelDostup,
		Slug:      sess.Draft.DostupSlug,
		Organ:     sess.Draft.RecipientName,
		Title:     title,
		Body:      body,
		Reasons:   v.Reasons,
	})
	log.Printf("[MODERATION] запит поставлено на перевірку: %q (user %d, id %s)", title, it.UserID, it.ID)

	sess.Step = "idle"
	sess.Draft = Draft{}
	saveSession(m.deps, c)

	_ = c.Edit(holdUserText())
	m.sendReviewCard(it)
	return nil
}

// HoldEmail — те саме для email-каналу (спільна скринька).
func (m *ModerationModule) HoldEmail(c tb.Context, sess *session.SessionData, subject, body, recipientEmail, replyTo, cc string, v moderation.Verdict) error {
	it := m.store.Enqueue(moderation.Item{
		UserID:         c.Sender().ID,
		ChatID:         c.Chat().ID,
		TGName:         telegramName(c),
		TGUser:         telegramUsername(c),
		Signature:      session.SignatureName(sess.Profile),
		Channel:        moderation.ChannelEmail,
		Organ:          sess.Draft.RecipientName,
		Title:          sess.Draft.Subject,
		Body:           body,
		RecipientEmail: recipientEmail,
		MailSubject:    subject,
		ReplyTo:        replyTo,
		CC:             cc,
		Reasons:        v.Reasons,
	})
	log.Printf("[MODERATION] email-запит поставлено на перевірку: %q (user %d, id %s)", it.Title, it.UserID, it.ID)

	sess.Step = "idle"
	sess.Draft = Draft{}
	saveSession(m.deps, c)

	_ = c.Edit(holdUserText())
	m.sendReviewCard(it)
	return nil
}

// sendReviewCard — картка власнику з кнопками ✅/❌ (не блокує
// відповідь користувачу — у фоні).
func (m *ModerationModule) sendReviewCard(it moderation.Item) {
	if m.deps.Cfg == nil || m.deps.Cfg.AdminID == 0 {
		return
	}
	safego.Go("moderation-card", func() {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{
				{Unique: "mod_ok", Text: "✅ Надіслати", Data: it.ID},
				{Unique: "mod_no", Text: "❌ Відхилити", Data: it.ID},
			},
		}
		if _, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), buildReviewCard(it), kb, tb.ModeHTML); err != nil {
			log.Printf("[MODERATION] картку не надіслано: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Рішення власника
// ---------------------------------------------------------------------------

// handleApprove — ✅: відправляємо запит тим самим механізмом,
// що й звичайна відправка (журнал + статистика + гілка + дедлайн).
func (m *ModerationModule) handleApprove(c tb.Context) error {
	if !m.isAdmin(c) {
		_ = c.Respond(&tb.CallbackResponse{Text: "Тільки власник bot-а вирішує"})
		return nil
	}
	it, ok := m.store.Claim(c.Callback().Data)
	if !ok {
		_ = c.Respond(&tb.CallbackResponse{Text: "Цей запит уже вирішено"})
		_ = c.Edit("ℹ️ Запит уже вирішено або не знайдено. Список: /moderation")
		return nil
	}
	_ = c.Respond(&tb.CallbackResponse{Text: "Надсилаю…"})
	_ = c.Edit(fmt.Sprintf("⏳ Надсилаю запит «%s» …", htmlEscape(it.Title)))

	var err error
	switch it.Channel {
	case moderation.ChannelDostup:
		err = m.submitDostup(it)
	case moderation.ChannelEmail:
		err = m.submitEmail(it)
	default:
		err = fmt.Errorf("невідомий канал %q", it.Channel)
	}
	if err != nil {
		// Відправка не вдалася: повертаємо в очікування — власник
		// зможе натиснути ✅ ще раз (наприклад, після ліміту порталу).
		m.store.Release(it.ID)
		log.Printf("[MODERATION] approve submit error: %v", err)
		_ = c.Edit(fmt.Sprintf("❌ Не вдалося надіслати: %s\n\nКнопка повернулася в /moderation — спробуйте ще раз.", htmlEscape(err.Error())))
		return nil
	}
	m.store.SetStatus(it.ID, moderation.StatusApproved, it.ResultURL)
	_ = c.Edit(fmt.Sprintf("✅ <b>Надіслано.</b>\n🔗 %s\n👤 Автор повідомлений.", htmlLink(it.ResultURL)))
	return nil
}

// submitDostup — відправка схваленого запиту на портал + журнал,
// статистика, гілка + повідомлення автору.
func (m *ModerationModule) submitDostup(it moderation.Item) error {
	if m.deps.Dostup == nil {
		return fmt.Errorf("канал порталу не налаштований")
	}
	info, err := m.deps.Dostup.SubmitRequest(it.Slug, it.Title, it.Body)
	if err != nil {
		// «Неожиданный ответ сервера»: запит міг створитися — той самий
		// сценарій, що й у handleSubmit (реальний випадок 30.08.2026).
		if m.dostup != nil {
			if resolved := m.dostup.resolveByTitle(it.Title); resolved != nil {
				info, err = resolved, nil
			}
		}
	}
	if err != nil {
		return err
	}

	_ = m.deps.SentLog.Append(sentlog.SentEntry{
		MessageID:      "dostup:" + info.Slug,
		ChatID:         it.ChatID,
		UserID:         it.UserID,
		RecipientName:  it.Organ,
		RecipientEmail: "dostup.org.ua",
		Subject:        it.Title,
		Date:           time.Now().Format(time.RFC3339),
		Channel:        "dostup",
		URL:            info.URL,
		DostupBody:     it.Organ,
		Delivered:      true,
	})
	if m.deps.Stats != nil {
		m.deps.Stats.IncrementRequests()
		m.deps.Stats.IncrementModule("dostup")
	}
	if m.deps.FollowUps != nil {
		m.deps.FollowUps.Upsert(it.UserID, FollowUpThread{
			Slug:    info.Slug,
			Subject: it.Title,
			Organ:   it.Organ,
			URL:     info.URL,
		})
	}
	it.ResultURL = info.URL
	m.sendUserApproved(it, info.URL)
	return nil
}

// submitEmail — відправка схваленого email-запиту (текстом, без PDF:
// PDF генерується з сесії на льоту, тут сесії вже немає; текст листа
// самодостатній).
func (m *ModerationModule) submitEmail(it moderation.Item) error {
	if m.deps.Email == nil {
		return fmt.Errorf("email-канал не налаштований")
	}
	msgID, err := m.deps.Email.Send(it.RecipientEmail, it.MailSubject, it.Body, it.ReplyTo, it.CC)
	if err != nil {
		return err
	}
	_ = m.deps.SentLog.Append(sentlog.SentEntry{
		MessageID:      msgID,
		ChatID:         it.ChatID,
		UserID:         it.UserID,
		RecipientName:  it.Organ,
		RecipientEmail: it.RecipientEmail,
		Subject:        it.MailSubject,
		Date:           time.Now().Format(time.RFC3339),
	})
	if m.deps.Stats != nil {
		m.deps.Stats.IncrementRequests()
		m.deps.Stats.IncrementModule("new_request")
	}
	it.ResultURL = ""
	m.sendUserApproved(it, "")
	return nil
}

// sendUserApproved — повідомлення автору: запит врешті надіслано.
func (m *ModerationModule) sendUserApproved(it moderation.Item, url string) {
	safego.Go("moderation-user-ok", func() {
		deadline := addWorkingDays(time.Now(), 5).Format("02.01.2006")
		text := fmt.Sprintf("✅ <b>Перевірку пройдено — ваш запит надіслано!</b>\n\n📩 Тема: %s\n🏛 Орган: %s\n⏰ Дедлайн відповіді: <b>%s</b> (до 5 робочих днів).\n\nЯ повідомлю, коли орган відповість по суті.",
			htmlEscape(it.Title), htmlEscape(it.Organ), deadline)
		if url != "" {
			text += fmt.Sprintf("\n🔗 Публічне посилання: %s", htmlLink(url))
		}
		if _, err := m.bot.Send(tb.ChatID(it.ChatID), text, tb.ModeHTML); err != nil {
			log.Printf("[MODERATION] повідомлення автору (%d) не надіслано: %v", it.UserID, err)
		}
	})
}

// handleReject — ❌: спільний акаунт не використовуємо, але право
// на запит зберігається — автор отримує ПОВНИЙ текст листа для
// самостійної подачі (реєстрація на dostup.org.ua — 2 хвилини).
func (m *ModerationModule) handleReject(c tb.Context) error {
	if !m.isAdmin(c) {
		_ = c.Respond(&tb.CallbackResponse{Text: "Тільки власник bot-а вирішує"})
		return nil
	}
	it, ok := m.store.Claim(c.Callback().Data)
	if !ok {
		_ = c.Respond(&tb.CallbackResponse{Text: "Цей запит уже вирішено"})
		_ = c.Edit("ℹ️ Запит уже вирішено або не знайдено. Список: /moderation")
		return nil
	}
	_ = c.Respond(&tb.CallbackResponse{Text: "Відхилено"})
	m.store.SetStatus(it.ID, moderation.StatusRejected, "")
	_ = c.Edit(fmt.Sprintf("❌ <b>Відхилено.</b> Автор отримав текст листа для самостійної подачі.\n📩 %s", htmlEscape(it.Title)))

	safego.Go("moderation-user-reject", func() {
		if _, err := m.bot.Send(tb.ChatID(it.ChatID), rejectUserText(it), tb.ModeHTML); err != nil {
			log.Printf("[MODERATION] повідомлення автору (%d) не надіслано: %v", it.UserID, err)
		}
	})
	return nil
}

// handleList — /moderation: список запитів на очікуванні (тільки власник).
func (m *ModerationModule) handleList(c tb.Context) error {
	if !m.isAdmin(c) {
		return c.Send("ℹ️ Команда доступна лише власнику bot-а.")
	}
	pending := m.store.Pending()
	if len(pending) == 0 {
		return c.Send("✅ На перевірці зараз нічого немає.")
	}
	_ = c.Send(fmt.Sprintf("🛡 <b>Запити на перевірці: %d</b>", len(pending)), tb.ModeHTML)
	for _, it := range pending {
		m.sendReviewCard(it)
	}
	return nil
}

func (m *ModerationModule) isAdmin(c tb.Context) bool {
	return m.deps.Cfg != nil && m.deps.Cfg.AdminID != 0 && c.Sender().ID == m.deps.Cfg.AdminID
}

// ---------------------------------------------------------------------------
// Сповіщення про КОЖНУ відправку (повна видимість у реальному часі)
// ---------------------------------------------------------------------------

// NotifySent — короткий рядок власнику після кожної успішної відправки
// (запити самого власця не дублюємо — він і так у курсі своїх тестів).
func (m *ModerationModule) NotifySent(userID int64, tgName, tgUsername, signature, organ, title, url string) {
	if m.deps.Cfg == nil || m.deps.Cfg.AdminID == 0 || userID == m.deps.Cfg.AdminID {
		return
	}
	safego.Go("moderation-notify", func() {
		text := fmt.Sprintf("📤 <b>Запит надіслано через спільний канал</b>\n\n👤 %s\n🔗 %s\n🆔 %d\n✍️ Підпис: %s\n🏛 %s\n📩 «%s»",
			htmlEscape(tgName), htmlEscape(tgUsername), userID, htmlEscape(signature), htmlEscape(organ), htmlEscape(title))
		if url != "" {
			text += fmt.Sprintf("\n🔗 %s", htmlLink(url))
		}
		if _, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), text, tb.ModeHTML); err != nil {
			log.Printf("[MODERATION] notify не надіслано: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Тексти (чисті функції — під тестами)
// ---------------------------------------------------------------------------

// holdUserText — що бачить автор чутливого запиту одразу.
func holdUserText() string {
	return "🛡 <b>Ваш запит поставлено на перевірку</b>\n\n" +
		"Він стосується теми, яка у воєнний час може мати обмежений доступ " +
		"(ст. 6 ЗУ «Про доступ до публічної інформації» — інформація з обмеженим доступом). " +
		"Такі запити ми надсилаємо після короткої перевірки — зазвичай це до кількох годин.\n\n" +
		"⏳ Я повідомлю вас, щойно запит буде надіслано. Дякую за розуміння: " +
		"перевірка захищає спільний канал, через який працюють усі користувачі."
}

// rejectUserText — відмова з повагою до права на запит: повний текст
// листа + як подати самостійно. Без цензури — з альтернативою.
func rejectUserText(it moderation.Item) string {
	return "❌ <b>Запит не надіслано через спільний акаунт</b>\n\n" +
		"Ваша тема стосується інформації, доступ до якої може бути обмежений " +
		"у воєнний час, тому через спільний канал порталу ми її не надсилаємо — " +
		"це захищає акаунт, через який працюють усі користувачі.\n\n" +
		"Але ваше право на запит нікуди не дівається: ви можете подати його " +
		"<b>самостійно від свого імені</b> — реєстрація на dostup.org.ua займає " +
		"2 хвилини. Ось готовий текст листа:\n\n" +
		"<pre>" + htmlEscape(it.Body) + "</pre>\n\n" +
		"Або створіть запит іншої теми: /new"
}

// buildReviewCard — картка для власника: хто, що, ознаки ризику, текст.
func buildReviewCard(it moderation.Item) string {
	var b strings.Builder
	b.WriteString("🛡 <b>Запит на перевірці — антиспровокаційний скринінг</b>\n\n")
	author := strings.TrimSpace(it.TGName)
	if author == "" {
		author = "без імені"
	}
	who := author
	if it.TGUser != "" {
		who += " (@" + it.TGUser + ")"
	}
	who += fmt.Sprintf(" · ID %d", it.UserID)
	b.WriteString("👤 Автор: " + htmlEscape(who) + "\n")
	b.WriteString("✍️ Підпис у листі: " + htmlEscape(it.Signature) + "\n")
	b.WriteString("🏛 Орган: " + htmlEscape(it.Organ) + "\n")
	b.WriteString("📩 Тема: " + htmlEscape(it.Title) + "\n")
	channel := "«Доступ до правди» (спільний акаунт порталу)"
	if it.Channel == moderation.ChannelEmail {
		channel = "email (спільна скринька)"
	}
	b.WriteString("🌐 Канал: " + channel + "\n\n")
	b.WriteString("⚠️ <b>Ознаки ризику:</b>\n")
	for _, r := range it.Reasons {
		b.WriteString("• " + htmlEscape(r) + "\n")
	}
	b.WriteString("\n📄 <b>Текст листа:</b>\n<pre>")
	body := it.Body
	if len([]rune(body)) > 1500 {
		body = string([]rune(body)[:1500]) + "…"
	}
	b.WriteString(htmlEscape(body))
	b.WriteString("</pre>\n\n")
	b.WriteString("Ваше рішення: ✅ — надіслати через спільний акаунт; ❌ — відхилити " +
		"(автор отримає текст листа для самостійної подачі).")
	return b.String()
}

// telegramName — ім'я+прізвище з Telegram (для картки і сповіщень).
func telegramName(c tb.Context) string {
	if c.Sender() == nil {
		return ""
	}
	s := c.Sender()
	return strings.TrimSpace(strings.TrimSpace(s.FirstName) + " " + strings.TrimSpace(s.LastName))
}

// telegramUsername — @username без собачки (порожньо, якщо немає).
func telegramUsername(c tb.Context) string {
	if c.Sender() == nil {
		return ""
	}
	return c.Sender().Username
}
