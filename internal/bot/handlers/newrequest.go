package handlers

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/email"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
	"info-bot-go/internal/stats"
)

// NewRequestModule handles /new.
type NewRequestModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewNewRequestModule(deps *Deps) *NewRequestModule {
	return &NewRequestModule{deps: deps, bot: deps.Bot}
}

func (m *NewRequestModule) Name() string       { return "new-request" }
func (m *NewRequestModule) StepPrefix() string { return "new:" }

func (m *NewRequestModule) Register() {
	m.bot.Handle("/new", safeHandler("new", m.startNew))
	m.bot.Handle("📝 Новий запит", safeHandler("new_btn", m.startNew))

	catBtn := tb.InlineButton{Unique: "nr_cat"}
	m.bot.Handle(&catBtn, safeHandler("nr_cat", m.handleCategory))

	backBtn := tb.InlineButton{Unique: "nr_back"}
	m.bot.Handle(&backBtn, safeHandler("nr_back", func(c tb.Context) error {
		_ = c.Respond()
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "new:pick_category"
		saveSession(m.deps, c)
		_ = c.Edit("Оберіть категорію розпорядника:", m.categoriesKeyboard())
		return nil
	}))

	rcpBtn := tb.InlineButton{Unique: "nr_rcp"}
	m.bot.Handle(&rcpBtn, safeHandler("nr_rcp", m.handleRecipient))

	manualBtn := tb.InlineButton{Unique: "nr_manual"}
	m.bot.Handle(&manualBtn, safeHandler("nr_manual", func(c tb.Context) error {
		_ = c.Respond()
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "new:ask_recipient_name"
		saveSession(m.deps, c)
		_ = c.Edit("Введіть назву органу/установи вручну:")
		return nil
	}))

	sendBtn := tb.InlineButton{Unique: "nr_send"}
	m.bot.Handle(&sendBtn, safeHandler("nr_send", m.handleSendConfirm))

	improveBtn := tb.InlineButton{Unique: "nr_improve"}
	m.bot.Handle(&improveBtn, safeHandler("nr_improve", m.handleImprove))

	toggleBtn := tb.InlineButton{Unique: "nr_toggle"}
	m.bot.Handle(&toggleBtn, safeHandler("nr_toggle", func(c tb.Context) error {
		_ = c.Respond()
		sess := c.Get("session").(*session.SessionData)
		sess.Draft.UseSharedMailbox = !sess.Draft.UseSharedMailbox
		saveSession(m.deps, c)
		return m.showConfirm(c, false)
	}))

	cancelBtn := tb.InlineButton{Unique: "nr_cancel"}
	m.bot.Handle(&cancelBtn, safeHandler("nr_cancel", func(c tb.Context) error {
		_ = c.Respond()
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "idle"
		sess.Draft = Draft{}
		saveSession(m.deps, c)
		_ = c.Edit("❌ Скасовано.")
		return nil
	}))

	verifyBtn := tb.InlineButton{Unique: "nr_verify"}
	m.bot.Handle(&verifyBtn, safeHandler("nr_verify", m.handleVerify))

	vupdBtn := tb.InlineButton{Unique: "nr_vupd"}
	m.bot.Handle(&vupdBtn, safeHandler("nr_vupd", m.handleVerifyUpdate))

	keepBtn := tb.InlineButton{Unique: "nr_keep"}
	m.bot.Handle(&keepBtn, safeHandler("nr_keep", func(c tb.Context) error {
		_ = c.Respond()
		_ = c.Edit("ℹ️ Залишено стару адресу.")
		return m.showConfirm(c, false)
	}))
}

func (m *NewRequestModule) startNew(c tb.Context) error {
	// Rate limit check: 3 requests per hour
	if m.deps.RateLimit != nil && !m.deps.RateLimit.Allow(c.Sender().ID) {
		remaining := m.deps.RateLimit.Remaining(c.Sender().ID)
		return c.Send(fmt.Sprintf("⏳ Ліміт запитів: не більше 3 на годину. Зачекайте трохи. (Залишилось: %d)", remaining))
	}

	sess := c.Get("session").(*session.SessionData)
	if !session.IsProfileReady(sess.Profile) {
		sess.Step = "profile:firstName"
		saveSession(m.deps, c)
		return c.Send("👋 Спочатку заповнимо профіль.\n\n1️⃣ Введіть ваше *ім'я*:", tb.ModeMarkdown)
	}
	sess.Draft = Draft{}
	sess.Step = "new:pick_category"
	saveSession(m.deps, c)
	return c.Send("Оберіть категорію розпорядника інформації:", m.categoriesKeyboard())
}

func (m *NewRequestModule) categoriesKeyboard() *tb.ReplyMarkup {
	kb := &tb.ReplyMarkup{}
	labels := m.deps.Directory.CategoryLabels()
	keys := m.deps.Directory.CategoryKeys()

	var rows [][]tb.InlineButton
	for _, k := range keys {
		label, ok := labels[k]
		if !ok {
			label = k
		}
		rows = append(rows, []tb.InlineButton{
			{Unique: "nr_cat", Text: label, Data: k},
		})
	}
	rows = append(rows, []tb.InlineButton{
		{Unique: "nr_manual", Text: "✍️ Ввести вручну"},
	})
	kb.InlineKeyboard = rows
	return kb
}

func (m *NewRequestModule) handleCategory(c tb.Context) error {
	_ = c.Respond()
	cat := c.Callback().Data
	sess := c.Get("session").(*session.SessionData)

	items := m.deps.Directory.ByCategory(cat)
	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	for _, r := range items {
		rows = append(rows, []tb.InlineButton{
			{Unique: "nr_rcp", Text: r.Name, Data: r.ID},
		})
	}
	rows = append(rows, []tb.InlineButton{
		{Unique: "nr_back", Text: "⬅️ Назад"},
	})
	kb.InlineKeyboard = rows
	sess.Step = "new:pick_recipient"
	saveSession(m.deps, c)
	_ = c.Edit("Оберіть розпорядника:", kb)
	return nil
}

func (m *NewRequestModule) handleRecipient(c tb.Context) error {
	_ = c.Respond()
	id := c.Callback().Data
	r := m.deps.Directory.FindByID(id)
	if r == nil {
		_ = c.Edit("Не знайдено.")
		return nil
	}
	sess := c.Get("session").(*session.SessionData)
	sess.Draft.RecipientName = r.Name
	sess.Draft.RecipientEmail = r.Email
	sess.Step = "new:ask_subject"
	saveSession(m.deps, c)
	_ = c.Edit(fmt.Sprintf("✅ Обрано: %s\n📧 %s", r.Name, r.Email))
	return c.Send("Коротка тема запиту (наприклад: «Витрати на ремонт доріг у 2025 році»):")
}

func (m *NewRequestModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	sess := c.Get("session").(*session.SessionData)
	text = strings.TrimSpace(text)

	switch step {
	case "new:ask_recipient_name":
		sess.Draft.RecipientName = text
		sess.Step = "new:ask_recipient_email"
		saveSession(m.deps, c)
		return true, c.Send("Введіть e-mail розпорядника інформації:")

	case "new:ask_recipient_email":
		if !strings.Contains(text, "@") || !strings.Contains(text, ".") {
			return true, c.Send("❌ Невірний формат e-mail. Спробуйте ще раз:")
		}
		sess.Draft.RecipientEmail = text
		sess.Step = "new:ask_subject"
		saveSession(m.deps, c)
		return true, c.Send("Коротка тема запиту:")

	case "new:ask_subject":
		sess.Draft.Subject = text
		sess.Step = "new:ask_body"
		saveSession(m.deps, c)
		return true, c.Send("Опишіть детально, яку саме інформацію ви хочете отримати:")

	case "new:ask_body":
		sess.Draft.Body = text
		sess.Draft.UseSharedMailbox = sess.Profile.Email == ""
		saveSession(m.deps, c)
		return true, m.showConfirm(c, false)

	case "new:confirm":
		return true, c.Send("Натисніть кнопку вище: ✅ Надіслати / ❌ Скасувати / 🔄 Перемкнути.")
	}
	return false, nil
}

func (m *NewRequestModule) showConfirm(c tb.Context, improved bool) error {
	sess := c.Get("session").(*session.SessionData)
	data := email.BuildRequestDataFromSession(sess.Profile, sess.Draft, m.deps.Cfg.SharedMailbox)
	if data == nil || data.RecipientName == "" || data.Subject == "" || data.Body == "" {
		return c.Send("❌ Помилка: чернетка неповна. Спробуйте /new ще раз.")
	}

	preview := email.BuildRequestText(*data)
	target := "🤝 спільний канал"
	if !data.UseSharedMailbox {
		target = fmt.Sprintf("📧 ваш email (%s)", data.Email)
	}

	aiBadge := ""
	if improved {
		aiBadge = " ✨ (Покращено AI)"
	}

	text := fmt.Sprintf("📝 *Перегляд запиту%s:*\n\n🏛 *Орган:* %s\n📨 *Кому:* %s\n📩 *Відповідь на:* %s\n\n--- *ТЕКСТ ЗАПИТУ* ---\n%s",
		aiBadge, data.RecipientName, sess.Draft.RecipientEmail, target, preview)

	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	rows = append(rows, []tb.InlineButton{{Unique: "nr_send", Text: "✅ Надіслати запит"}})
	if m.deps.Gemini != nil && m.deps.Gemini.Available() && !improved {
		rows = append(rows, []tb.InlineButton{{Unique: "nr_improve", Text: "✨ Покращити з AI"}})
	}
	rows = append(rows, []tb.InlineButton{{Unique: "nr_verify", Text: "🔍 Перевірити email"}})
	if sess.Profile.Email != "" {
		toggleText := "🔄 Перемкнути на: 📧 мій email"
		if !sess.Draft.UseSharedMailbox {
			toggleText = "🔄 Перемкнути на: 🤝 спільна пошта"
		}
		rows = append(rows, []tb.InlineButton{{Unique: "nr_toggle", Text: toggleText}})
	}
	rows = append(rows, []tb.InlineButton{{Unique: "nr_cancel", Text: "❌ Скасувати"}})
	kb.InlineKeyboard = rows

	sess.Step = "new:confirm"
	saveSession(m.deps, c)
	return c.Send(text, kb, tb.ModeMarkdown)
}

func (m *NewRequestModule) handleSendConfirm(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	data := email.BuildRequestDataFromSession(sess.Profile, sess.Draft, m.deps.Cfg.SharedMailbox)
	if data == nil {
		return c.Send("❌ Чернетка порожня.")
	}

	// Rate limit check
	if m.deps.RateLimit != nil && !m.deps.RateLimit.Allow(c.Sender().ID) {
		return c.Send("⏳ Ліміт запитів вичерпано (3/год). Зачекайте трохи.")
	}

	// Daily email limit (Brevo: 300/day, cap at 280)
	if m.deps.Stats != nil && m.deps.Stats.DailyLimitReached(280) {
		return c.Send("❌ Добовий ліміт надсилання вичерпано (280/280). Спробуйте завтра.")
	}

	_ = c.Edit("⏳ Генеруємо PDF та надсилаємо...")

	replyTo := os.Getenv("GMAIL_USER")
	if replyTo == "" {
		replyTo = "publicinquiry69@gmail.com"
	}
	cc := ""
	if !data.UseSharedMailbox {
		cc = data.Email
	}

	subject := email.BuildSubject(data.Subject)
	bodyText := email.BuildRequestText(*data)
	pdfBytes, pdfErr := generateFOIRequestPDF(*data)
	var msgID string
	var err error
	if pdfErr == nil && len(pdfBytes) > 0 {
		msgID, err = m.deps.Email.SendWithAttachment(sess.Draft.RecipientEmail, subject, bodyText, replyTo, cc, pdfBytes, fmt.Sprintf("zapit_%s.pdf", time.Now().Format("20060102_150405")))
	} else {
		log.Printf("[PDF] PDF generation failed (%v), falling back to plain text", pdfErr)
		msgID, err = m.deps.Email.Send(sess.Draft.RecipientEmail, subject, bodyText, replyTo, cc)
	}
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Помилка надсилання: %s. Спробуйте ще раз.", err))
	}

	deadline := addWorkingDays(time.Now(), 5).Format("02.01.2006")

	_ = m.deps.SentLog.Append(sentlog.SentEntry{
		MessageID:      msgID,
		ChatID:         c.Chat().ID,
		UserID:         c.Sender().ID,
		RecipientName:  data.RecipientName,
		RecipientEmail: sess.Draft.RecipientEmail,
		Subject:        email.BuildSubject(data.Subject),
		Date:           time.Now().Format(time.RFC3339),
	})

	if m.deps.Stats != nil {
		m.deps.Stats.IncrementRequests()
		m.deps.Stats.IncrementModule("new_request")
	}

	sess.Step = "idle"
	sess.Draft = Draft{}
	saveSession(m.deps, c)

	text := fmt.Sprintf("✅ *Запит успішно надіслано!*\n\n🆔 ID: `%s`\n📄 Формат: PDF-документ\n⏰ Дедлайн відповіді: *%s* (5 робочих днів).\n\nЯ повідомлю вас, коли отримаю відповідь.", msgID, deadline)
	kb := MainMenuKeyboard(m.deps.Cfg, c.Sender().ID)
	return c.Send(text, kb, tb.ModeMarkdown)
}

func generateFOIRequestPDF(data email.RequestData) ([]byte, error) {
	cmd := exec.Command(
		"/home/u0_a566/pdf_gen_env/bin/python3",
		"/home/u0_a566/tools/generate_foi_pdf.py",
		"--recipient", data.RecipientName,
		"--subject", data.Subject,
		"--body", data.Body,
		"--requester", data.FullName,
		"--output", "/tmp/foi_request.pdf",
	)
	if data.Email != "" && !data.UseSharedMailbox {
		cmd.Args = append(cmd.Args, "--email", data.Email)
	}
	if data.PostalAddress != "" && !data.UseSharedMailbox {
		cmd.Args = append(cmd.Args, "--postal", data.PostalAddress)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("PDF generation failed: %s: %w", string(output), err)
	}

	return os.ReadFile("/tmp/foi_request.pdf")
}

func (m *NewRequestModule) handleVerify(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)

	if m.deps.OSINT == nil {
		return c.Send("⚠️ Фактчекінг вимкнено (немає API ключів).")
	}

	if sess.Draft.RecipientEmail == "" || sess.Draft.RecipientName == "" {
		_ = c.Edit("⚠️ Спочатку заповніть чернетку.")
		return nil
	}

	_ = c.Edit("🔍 *Перевіряю актуальність email...*", tb.ModeMarkdown)
	_ = c.Bot().Notify(c.Sender(), tb.Typing)

	result, err := m.deps.OSINT.FindEmail(sess.Draft.RecipientName)
	if err != nil {
		log.Printf("[VERIFY] OSINT error for %q: %v", sess.Draft.RecipientName, err)
		_ = c.Edit(fmt.Sprintf("❌ Не вдалося виконати перевірку: %s", err))
		return nil
	}

	if result.Email == "" {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "nr_keep", Text: "⬅️ Назад до чернетки"}},
		}
		_ = c.Edit(fmt.Sprintf("ℹ️ Не вдалося знайти email для «%s» в інтернеті.\nПоточний адрес у чернетці: `%s`", sess.Draft.RecipientName, sess.Draft.RecipientEmail), kb, tb.ModeMarkdown)
		return nil
	}

	sessDir := m.deps.Cfg.SessionDir
	if sessDir == "" {
		sessDir = ".sessions_go"
	}

	// Always save to learned cache (even if same — refreshes confidence)
	m.deps.Directory.AddLearned(sessDir, result.AgencyName, result.Email)
	sess.Draft.OSINTSuggestedName = result.AgencyName

	if result.Email == sess.Draft.RecipientEmail {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "nr_keep", Text: "⬅️ Назад до чернетки"}},
		}
		_ = c.Edit(fmt.Sprintf("✅ *Фактчекінг пройдено!*\n\nEmail `%s` є актуальним для «%s».", result.Email, result.AgencyName), kb, tb.ModeMarkdown)
	} else {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "nr_vupd", Text: "✅ Оновити на новий адрес", Data: result.Email}},
			{{Unique: "nr_keep", Text: "❌ Залишити старий"}},
		}
		_ = c.Edit(fmt.Sprintf("⚠️ *Знайдено новий email!*\n\n🏛 *%s*\n📧 Було: `%s`\n📧 Стало: `%s`\n\nОновити адресу в чернетці?",
			result.AgencyName, sess.Draft.RecipientEmail, result.Email), kb, tb.ModeMarkdown)
	}
	return nil
}

func (m *NewRequestModule) handleVerifyUpdate(c tb.Context) error {
	_ = c.Respond()
	_ = c.Edit("✅ Адресу оновлено!")
	newEmail := c.Callback().Data
	sess := c.Get("session").(*session.SessionData)

	sess.Draft.RecipientEmail = newEmail
	sess.Draft.RecipientName = sess.Draft.OSINTSuggestedName
	saveSession(m.deps, c)

	return m.showConfirm(c, false)
}

func (m *NewRequestModule) handleImprove(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	if m.deps.Gemini == nil || !m.deps.Gemini.Available() {
		return c.Send("⚠️ AI-покращення вимкнено.")
	}

	subj := sess.Draft.Subject
	body := sess.Draft.Body
	if subj == "" || body == "" {
		return c.Send("⚠️ Немає чернетки для покращення.")
	}

	_ = c.Send("✨ Покращую текст через Gemini AI… (5–15 секунд)")
	newSubj, newBody, err := m.deps.Gemini.ImproveRequest(subj, body)
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Не вдалось покращити: %s\n\nЗапит лишається без змін.", err))
	}
	sess.Draft.Subject = newSubj
	sess.Draft.Body = newBody
	saveSession(m.deps, c)
	return m.showConfirm(c, true)
}

func addWorkingDays(start time.Time, days int) time.Time {
	d := start
	added := 0
	for added < days {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			added++
		}
	}
	return d
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

var _ = stats.GlobalStats{}
