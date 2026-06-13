package handlers

import (
	"fmt"
	"log"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/directory"
	"info-bot-go/internal/session"
)

// SearchModule handles inline text search for government agencies.
// When user is in "idle" step and sends text that doesn't match any command,
// it tries to find matching agencies from the directory.
// If directory has no results, falls back to OSINT internet search.
type SearchModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewSearchModule(deps *Deps) *SearchModule {
	return &SearchModule{deps: deps, bot: deps.Bot}
}

func (m *SearchModule) Name() string       { return "search" }
func (m *SearchModule) StepPrefix() string { return "search:" }

func (m *SearchModule) Register() {
	srchBtn := tb.InlineButton{Unique: "srch_sel"}
	m.bot.Handle(&srchBtn, safeHandler("srch_sel", m.handleSearchSelect))
}

func (m *SearchModule) HandleSearch(c tb.Context, query string) error {
	results := m.deps.Directory.Search(query)
	if len(results) > 0 {
		return m.showLocalResults(c, query, results)
	}

	if m.deps.OSINT == nil {
		return c.Send("🔍 Ничего не найдено в базе. Попробуйте другое название или /directory.")
	}

	msg, _ := c.Bot().Send(c.Chat(), "🔍 *Ищу информацию в интернете...*", tb.ModeMarkdown)

	result, err := m.deps.OSINT.FindEmail(query)
	if err != nil {
		log.Printf("[SEARCH] OSINT error for %q: %v", query, err)
		c.Bot().Edit(msg, "🔍 Ничего не найдено. Попробуйте другое название или /directory.")
		return nil
	}

	if result.Email == "" {
		c.Bot().Edit(msg, "🔍 К сожалению, не удалось найти email даже в интернете. Попробуйте другое название.")
		return nil
	}

	sessDir := m.deps.Cfg.SessionDir
	if sessDir == "" {
		sessDir = ".sessions_go"
	}
	id := m.deps.Directory.AddLearned(sessDir, result.AgencyName, result.Email)

	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "srch_sel", Text: "✅ Использовать этот email", Data: id}},
		{{Unique: "nr_cancel", Text: "❌ Отмена"}},
	}

	text := fmt.Sprintf("🌐 *Найдено в интернете!*\n\n🏛 *Орган:* %s\n📧 *Email:* %s\n\nБудет сохранено в базу для будущих поисков.",
		result.AgencyName, result.Email)
	c.Bot().Edit(msg, text, kb, tb.ModeMarkdown)
	return nil
}
func (m *SearchModule) showLocalResults(c tb.Context, query string, results []directory.Recipient) error {
	if len(results) > 10 {
		results = results[:10]
	}

	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	for _, r := range results {
		name := r.Name
		if len(name) > 40 {
			name = name[:37] + "..."
		}
		rows = append(rows, []tb.InlineButton{
			{Unique: "srch_sel", Text: name, Data: r.ID},
		})
	}
	rows = append(rows, []tb.InlineButton{
		{Unique: "nr_cancel", Text: "❌ Скасувати"},
	})
	kb.InlineKeyboard = rows

	text := fmt.Sprintf("🔍 Знайдено *%d* результатів для «%s»:\n\nОберіть орган:", len(results), query)
	return c.Send(text, kb, tb.ModeMarkdown)
}

func (m *SearchModule) handleSearchSelect(c tb.Context) error {
	_ = c.Respond()
	id := c.Callback().Data
	r := m.deps.Directory.FindByID(id)
	if r == nil {
		_ = c.Edit("Не знайдено.")
		return nil
	}

	sess := c.Get("session").(*session.SessionData)

	if !session.IsProfileReady(sess.Profile) {
		sess.Step = "profile:firstName"
		sess.Draft.RecipientName = r.Name
		sess.Draft.RecipientEmail = r.Email
		saveSession(m.deps, c)
		_ = c.Edit(fmt.Sprintf("✅ Обрано: %s\n📧 %s\n\n👋 Спочатку заповнимо профіль.", r.Name, r.Email))
		return c.Send("1️⃣ Введіть ваше *ім'я*:", tb.ModeMarkdown)
	}

	sess.Draft.RecipientName = r.Name
	sess.Draft.RecipientEmail = r.Email
	sess.Step = "new:ask_subject"
	saveSession(m.deps, c)

	_ = c.Edit(fmt.Sprintf("✅ Обрано: %s\n📧 %s", r.Name, r.Email))
	return c.Send("Коротка тема запиту (наприклад: «Витрати на ремонт доріг у 2025 році»):")
}

func (m *SearchModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	return false, nil
}
