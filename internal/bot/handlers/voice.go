package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/email"
	"info-bot-go/internal/osint"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
)

// VoiceModule handles voice messages.
type VoiceModule struct {
	deps      *Deps
	bot       *tb.Bot
	bugReport *BugReportModule
}

func NewVoiceModule(deps *Deps) *VoiceModule {
	return &VoiceModule{deps: deps, bot: deps.Bot}
}

// SetBugReportModule stores a reference to the BugReportModule for voice delegation.
func (m *VoiceModule) SetBugReportModule(br *BugReportModule) {
	m.bugReport = br
}

func (m *VoiceModule) Name() string       { return "voice" }
func (m *VoiceModule) StepPrefix() string { return "voice:" }

func (m *VoiceModule) Register() {
	// Voice message handler — also delegates to bugreport when step is bugreport:waiting
	m.bot.Handle(tb.OnVoice, safeHandler("voice", m.handleVoice))
	m.bot.Handle(tb.OnAudio, safeHandler("audio", m.handleVoice))

	// Edit draft button
	voiceEditBtn := tb.InlineButton{Unique: "vc_edit"}
	m.bot.Handle(&voiceEditBtn, safeHandler("vc_edit", func(c tb.Context) error {
		_ = c.Respond()
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "voice:waiting_refine"
		saveSession(m.deps, c)
		return c.Send("🎤 Диктуйте або пишіть правки:")
	}))

	// Fast send
	voiceSendBtn := tb.InlineButton{Unique: "vc_send"}
	m.bot.Handle(&voiceSendBtn, safeHandler("vc_send", m.handleFastSend))

	// Verify email via OSINT
	voiceVerifyBtn := tb.InlineButton{Unique: "vc_verify"}
	m.bot.Handle(&voiceVerifyBtn, safeHandler("vc_verify", m.handleVerify))

	// Cancel
	voiceCancelBtn := tb.InlineButton{Unique: "vc_cancel"}
	m.bot.Handle(&voiceCancelBtn, safeHandler("vc_cancel", func(c tb.Context) error {
		_ = c.Respond()
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "idle"
		sess.Draft = Draft{}
		saveSession(m.deps, c)
		_ = c.Edit("❌ Скасовано.")
		return nil
	}))

	// Verify email update callback
	voiceVerifyUpdBtn := tb.InlineButton{Unique: "vc_vupd"}
	m.bot.Handle(&voiceVerifyUpdBtn, safeHandler("vc_vupd", m.handleVerifyUpdate))

	// Keep old email (dismiss OSINT suggestion)
	voiceKeepBtn := tb.InlineButton{Unique: "vc_keep"}
	m.bot.Handle(&voiceKeepBtn, safeHandler("vc_keep", func(c tb.Context) error {
		_ = c.Respond()
		_ = c.Edit("↩️ Повернення до чернетки...")
		sess := c.Get("session").(*session.SessionData)
		return m.showDraft(c, sess)
	}))
}

func (m *VoiceModule) handleVoice(c tb.Context) error {
	sess := c.Get("session").(*session.SessionData)

	// Delegate to bug report module if user is reporting a bug via voice
	if sess.Step == "bugreport:waiting" && m.bugReport != nil {
		return m.bugReport.HandleBugReportMedia(c)
	}

	// If in refine mode, handle as refinement
	if sess.Step == "voice:waiting_refine" {
		return m.handleRefineVoice(c, sess)
	}

	// New voice request
	_ = c.Send("🎧 Слухаю та оброблюю ваше голосове...")

	audioData, err := m.downloadVoice(c)
	if err != nil {
		log.Printf("[VOICE] download error: %v", err)
		return c.Send("❌ Не вдалося завантажити аудіо. Спробуйте ще раз.")
	}

	if m.deps.Gemini == nil || !m.deps.Gemini.Available() {
		return c.Send("⚠️ Голосова обробка вимкнена (немає AI). Використовуйте /new для текстового запиту.")
	}

	_, hint, subject, body, err := m.deps.Gemini.VoiceToRequest(audioData, "audio/ogg")
	if err != nil {
		log.Printf("[VOICE] AI error: %v", err)
		return c.Send("❌ Не вдалося розпізнати. Спробуйте ще раз.")
	}

	// Search recipient in directory
	found := m.deps.Directory.Search(hint)
	recipientName := hint
	recipientEmail := ""
	if len(found) > 0 {
		recipientName = found[0].Name
		recipientEmail = found[0].Email
	}

	sess.Draft = Draft{
		Subject:          subject,
		Body:             body,
		RecipientName:    recipientName,
		RecipientEmail:   recipientEmail,
		UseSharedMailbox: true,
	}
	saveSession(m.deps, c)

	return m.showDraft(c, sess)
}

func (m *VoiceModule) handleRefineVoice(c tb.Context, sess *session.SessionData) error {
	_ = c.Send("🔄 Оновлюю чернетку голосом...")

	audioData, err := m.downloadVoice(c)
	if err != nil {
		return c.Send("⚠️ Помилка завантаження аудіо.")
	}

	draftJSON, _ := json.Marshal(sess.Draft)
	result, err := m.deps.Gemini.RefineRequest(string(draftJSON), "Онови текст за голосом", audioData)
	if err != nil {
		return c.Send("⚠️ Помилка оновлення.")
	}

	var updated struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(result)), &updated); err == nil {
		sess.Draft.Subject = updated.Subject
		sess.Draft.Body = updated.Body
	}

	sess.Step = "idle"
	saveSession(m.deps, c)
	return m.showDraft(c, sess)
}

func (m *VoiceModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	if step != "voice:waiting_refine" {
		return false, nil
	}

	sess := c.Get("session").(*session.SessionData)
	_ = c.Send("🔄 Оновлюю чернетку...")

	draftJSON, _ := json.Marshal(sess.Draft)
	result, err := m.deps.Gemini.RefineRequest(string(draftJSON), text, nil)
	if err != nil {
		return true, c.Send("⚠️ Помилка оновлення.")
	}

	var updated struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(result)), &updated); err == nil {
		sess.Draft.Subject = updated.Subject
		sess.Draft.Body = updated.Body
	}

	sess.Step = "idle"
	saveSession(m.deps, c)
	return true, m.showDraft(c, sess)
}

func (m *VoiceModule) handleFastSend(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	d := sess.Draft
	p := sess.Profile

	data := email.BuildRequestDataFromSession(p, d, m.deps.Cfg.SharedMailbox)
	if data == nil {
		return c.Send("❌ Чернетка неповна.")
	}

	// FIX: Use shared mailbox as Reply-To so IMAP watcher catches replies
	replyTo := m.deps.Cfg.SharedMailbox
	cc := ""
	if !data.UseSharedMailbox && data.Email != "" {
		replyTo = data.Email
		cc = data.Email
	}
	msgID, err := m.deps.Email.Send(d.RecipientEmail, email.BuildSubject(d.Subject), email.BuildRequestText(*data), replyTo, cc)
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Помилка пошти: %s", err))
	}

	_ = m.deps.SentLog.Append(sentlog.SentEntry{
		MessageID:      msgID,
		ChatID:         c.Chat().ID,
		UserID:         c.Sender().ID,
		RecipientName:  data.RecipientName,
		RecipientEmail: d.RecipientEmail,
		Subject:        email.BuildSubject(d.Subject),
		Date:           time.Now().Format(time.RFC3339),
	})

	sess.Step = "idle"
	sess.Draft = Draft{}
	saveSession(m.deps, c)

	kb := MainMenuKeyboard(m.deps.Cfg, c.Sender().ID)
	return c.Send("✅ *Запит успішно надіслано!*", kb, tb.ModeMarkdown)
}

func (m *VoiceModule) handleVerify(c tb.Context) error {
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
		log.Printf("[VOICE:VERIFY] OSINT error for %q: %v", sess.Draft.RecipientName, err)
		if errors.Is(err, osint.ErrOSINTCooldown) {
			_ = c.Edit("⏳ Всі ліміти API вичерпано. Будь ласка, спробуйте за кілька хвилин.")
		} else {
			_ = c.Edit(fmt.Sprintf("❌ Не вдалося виконати перевірку: %s", err))
		}
		return nil
	}

	if result.Email == "" {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "vc_keep", Text: "⬅️ Назад до чернетки"}},
		}
		_ = c.Edit(fmt.Sprintf("ℹ️ Не вдалося знайти email для «%s» в інтернеті.\nПоточний адрес у чернетці: `%s`", sess.Draft.RecipientName, sess.Draft.RecipientEmail), kb, tb.ModeMarkdown)
		return nil
	}

	sessDir := m.deps.Cfg.SessionDir
	if sessDir == "" {
		sessDir = ".sessions_go"
	}
	m.deps.Directory.AddLearned(sessDir, result.AgencyName, result.Email)
	sess.Draft.OSINTSuggestedName = result.AgencyName

	if result.Email == sess.Draft.RecipientEmail {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "vc_keep", Text: "⬅️ Назад до чернетки"}},
		}
		_ = c.Edit(fmt.Sprintf("✅ *Фактчекінг пройдено!*\n\nEmail `%s` є актуальним для «%s».", result.Email, result.AgencyName), kb, tb.ModeMarkdown)
	} else {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "vc_vupd", Text: "✅ Оновити на новий адрес", Data: result.Email}},
			{{Unique: "vc_keep", Text: "❌ Залишити старий"}},
		}
		_ = c.Edit(fmt.Sprintf("⚠️ *Знайдено новий email!*\n\n🏛 *%s*\n📧 Було: `%s`\n📧 Стало: `%s`\n\nОновити адресу в чернетці?",
			result.AgencyName, sess.Draft.RecipientEmail, result.Email), kb, tb.ModeMarkdown)
	}
	return nil
}

func (m *VoiceModule) handleVerifyUpdate(c tb.Context) error {
	_ = c.Respond()
	_ = c.Edit("✅ Адресу оновлено!")
	newEmail := c.Callback().Data
	sess := c.Get("session").(*session.SessionData)

	sess.Draft.RecipientEmail = newEmail
	sess.Draft.RecipientName = sess.Draft.OSINTSuggestedName
	saveSession(m.deps, c)

	return m.showDraft(c, sess)
}

func (m *VoiceModule) showDraft(c tb.Context, sess *session.SessionData) error {
	d := sess.Draft
	if d.RecipientName == "" || d.Subject == "" || d.Body == "" {
		return c.Send("❌ Чернетка порожня або застаріла. Будь ласка, створіть новий запит.")
	}
	text := fmt.Sprintf("🎙 *Чернетка готова!*\n\n🏛 *Отримувач:* %s\n📧 *Email:* %s\n📂 *Тема:* %s\n\n📝 *Текст:* _%s_",
		d.RecipientName, d.RecipientEmail, d.Subject, d.Body)

	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "vc_send", Text: "🚀 Відправити зараз"}},
		{{Unique: "vc_verify", Text: "🔍 Перевірити email"}},
		{{Unique: "vc_edit", Text: "✏️ Змінити"}},
		{{Unique: "vc_cancel", Text: "❌ Скасувати"}},
	}
	return c.Send(text, kb, tb.ModeMarkdown)
}

func (m *VoiceModule) downloadVoice(c tb.Context) ([]byte, error) {
	var fileID string
	if c.Message().Voice != nil {
		fileID = c.Message().Voice.FileID
	} else if c.Message().Audio != nil {
		fileID = c.Message().Audio.FileID
	} else {
		return nil, fmt.Errorf("no voice/audio in message")
	}

	file, err := c.Bot().FileByID(fileID)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", m.deps.Cfg.BotToken, file.FilePath)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func cleanJSON(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}
