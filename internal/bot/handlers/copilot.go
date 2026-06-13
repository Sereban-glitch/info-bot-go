package handlers

import (
        "fmt"

        tb "gopkg.in/telebot.v3"

        "info-bot-go/internal/session"
)

// CopilotModule handles PR copilot flow for social posts.
type CopilotModule struct {
        deps *Deps
        bot  *tb.Bot
}

func NewCopilotModule(deps *Deps) *CopilotModule {
        return &CopilotModule{deps: deps, bot: deps.Bot}
}

func (m *CopilotModule) Name() string       { return "copilot" }
func (m *CopilotModule) StepPrefix() string { return "copilot:" }

func (m *CopilotModule) Register() {
        shareBtn := tb.InlineButton{Unique: "pr_share"}
        m.bot.Handle(&shareBtn, safeHandler("pr_share", func(c tb.Context) error {
                _ = c.Respond()
                sess := c.Get("session").(*session.SessionData)
                sess.Step = "copilot:waiting_photo"
                sess.PRDraft = &session.PRDraft{IsAnonymous: true}
                saveSession(m.deps, c)
                return c.Send("📢 *Ви вирішили поділитися відповіддю з громадою!*\n\n1️⃣ Зробіть скріншот документа.\n2️⃣ Замажте своє ПІБ, адресу та телефон.\n3️⃣ Надішліть фото сюди.\n\n_Ваш пост буде опубліковано анонімно після модерації._", tb.ModeMarkdown)
        }))

        toneSharpBtn := tb.InlineButton{Unique: "pr_tone_sharp"}
        toneFormalBtn := tb.InlineButton{Unique: "pr_tone_formal"}
        toneGrammarBtn := tb.InlineButton{Unique: "pr_tone_grammar"}
        toneCancelBtn := tb.InlineButton{Unique: "pr_cancel"}

        m.bot.Handle(&toneSharpBtn, safeHandler("pr_sharp", func(c tb.Context) error {
                _ = c.Respond()
                return m.generatePost(c, "sharp")
        }))
        m.bot.Handle(&toneFormalBtn, safeHandler("pr_formal", func(c tb.Context) error {
                _ = c.Respond()
                return m.generatePost(c, "formal")
        }))
        m.bot.Handle(&toneGrammarBtn, safeHandler("pr_grammar", func(c tb.Context) error {
                _ = c.Respond()
                return m.generatePost(c, "grammar")
        }))
        m.bot.Handle(&toneCancelBtn, safeHandler("pr_cancel", func(c tb.Context) error {
                _ = c.Respond()
                sess := c.Get("session").(*session.SessionData)
                sess.Step = "idle"
                sess.PRDraft = nil
                saveSession(m.deps, c)
                _ = c.Edit("❌ Скасовано.")
                return nil
        }))

        prAdminBtn := tb.InlineButton{Unique: "pr_admin"}
        m.bot.Handle(&prAdminBtn, safeHandler("pr_admin", func(c tb.Context) error {
                _ = c.Respond()
                sess := c.Get("session").(*session.SessionData)
                draft := sess.PRDraft
                if draft == nil {
                        return nil
                }

                adminText := fmt.Sprintf("🔔 *НОВА ПРОПОЗИЦІЯ У КАНАЛ*\n\n👤 Від: Анонімно\n🛡 Вердикт ІІ: %s\n\n--- ТЕКСТ ---\n%s", draft.AIVerdict, draft.FinalText)
                kb := &tb.ReplyMarkup{}
                kb.InlineKeyboard = [][]tb.InlineButton{
                        {{Unique: "pr_pub", Text: "✅ Опублікувати в канал"}},
                        {{Unique: "pr_reject", Text: "❌ Відхилити"}},
                }
                _, _ = m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), adminText, kb, tb.ModeMarkdown)
                _ = c.Edit("✅ Вашу пропозицію надіслано адміну. Дякуємо!")
                sess.Step = "idle"
                saveSession(m.deps, c)
                return nil
        }))

        prPubBtn := tb.InlineButton{Unique: "pr_pub"}
        m.bot.Handle(&prPubBtn, safeHandler("pr_pub", func(c tb.Context) error {
                if c.Sender().ID != m.deps.Cfg.AdminID {
                        _ = c.Respond(&tb.CallbackResponse{Text: "⛔ Тільки для адміністратора", ShowAlert: true})
                        return nil
                }
                _ = c.Respond()

                msg := c.Callback().Message
                if msg == nil {
                        return nil
                }
                rawText := msg.Text
                content := rawText
                if idx := findStr(rawText, "--- ТЕКСТ ---"); idx >= 0 {
                        content = rawText[idx+len("--- ТЕКСТ ---"):]
                }
                content = trimSpaces(content)

                _, _ = m.bot.Send(chatRecipient(m.deps.Cfg.ChannelID), content, tb.ModeMarkdown)
                _ = c.Edit("🚀 *ОПУБЛІКОВАНО У КАНАЛІ!*", tb.ModeMarkdown)
                return nil
        }))

        prRejectBtn := tb.InlineButton{Unique: "pr_reject"}
        m.bot.Handle(&prRejectBtn, safeHandler("pr_reject", func(c tb.Context) error {
                _ = c.Respond()
                _ = c.Edit("❌ Відхилено.")
                return nil
        }))
}

func (m *CopilotModule) HandleText(c tb.Context, step string, text string) (bool, error) {
        sess := c.Get("session").(*session.SessionData)
        if !startsWith(step, "copilot:") {
                return false, nil
        }

        if step == "copilot:waiting_photo" {
                sess.PRDraft.Text = text
                sess.Step = "copilot:waiting_tone"
                saveSession(m.deps, c)

                kb := &tb.ReplyMarkup{}
                kb.InlineKeyboard = [][]tb.InlineButton{
                        {{Unique: "pr_tone_sharp", Text: "🔥 Гостро (Активіст)"}},
                        {{Unique: "pr_tone_formal", Text: "👔 Офіційно (Юрист)"}},
                        {{Unique: "pr_tone_grammar", Text: "✍️ Тільки помилки"}},
                        {{Unique: "pr_cancel", Text: "❌ Скасувати"}},
                }
                return true, c.Send("✨ Оберіть стиль, у якому ІІ підготує пост:", kb)
        }

        return false, nil
}

func (m *CopilotModule) generatePost(c tb.Context, tone string) error {
        sess := c.Get("session").(*session.SessionData)
        draft := sess.PRDraft
        if draft == nil {
                return c.Send("⚠️ Немає чернетки.")
        }

        _ = c.Respond()
        _ = c.Send("✨ Генерую пост через ІІ… (5–15 секунд)")

        postText, err := m.deps.Gemini.GenerateSocialPost(draft.Text, tone, nil)
        if err != nil {
                return c.Send(fmt.Sprintf("❌ Помилка генерації: %s", err))
        }

        isSafe, reason, _ := m.deps.Gemini.ValidateSubmission(postText, nil)
        draft.FinalText = postText
        draft.Tone = tone
        draft.AIVerdict = reason
        saveSession(m.deps, c)

        safeIcon := "✅ Безпечно"
        if !isSafe {
                safeIcon = "⚠️ Увага"
        }

        text := fmt.Sprintf("📝 *Готовий проект посту:*\n\n%s\n\n🛡 *Вердикт безпеки ІІ:* %s\n💬 *Чому:* %s",
                postText, safeIcon, reason)

        kb := &tb.ReplyMarkup{}
        kb.InlineKeyboard = [][]tb.InlineButton{
                {{Unique: "pr_admin", Text: "🚀 Надіслати адміну"}},
                {{Unique: "pr_cancel", Text: "❌ Скасувати"}},
        }
        return c.Send(text, kb, tb.ModeMarkdown)
}

func findStr(s, sub string) int {
        for i := 0; i <= len(s)-len(sub); i++ {
                if s[i:i+len(sub)] == sub {
                        return i
                }
        }
        return -1
}

func trimSpaces(s string) string {
        result := s
        for len(result) > 0 && (result[0] == ' ' || result[0] == '\n') {
                result = result[1:]
        }
        for len(result) > 0 && (result[len(result)-1] == ' ' || result[len(result)-1] == '\n') {
                result = result[:len(result)-1]
        }
        return result
}

func startsWith(s, prefix string) bool {
        return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
