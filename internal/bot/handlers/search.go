package handlers

import (
	"fmt"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/session"
)

// SearchModule handles inline text search for government agencies.
// When user is in "idle" step and sends text that doesn't match any command,
// it tries to find matching agencies from the directory.
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
	// Search result selection callback
	srchBtn := tb.InlineButton{Unique: "srch_sel"}
	m.bot.Handle(&srchBtn, safeHandler("srch_sel", m.handleSearchSelect))
}

// HandleSearch performs a directory search and shows results.
// Called from the text dispatcher when user is in idle state.
func (m *SearchModule) HandleSearch(c tb.Context, query string) error {
	results := m.deps.Directory.Search(query)
	if len(results) == 0 {
		return c.Send("🔍 Нічого не знайдено. Спробуйте іншу назву або /directory для категорій.")
	}

	// Limit to 10 results for Telegram inline keyboard
	if len(results) > 10 {
		results = results[:10]
	}

	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	for _, r := range results {
		// Truncate long names for button text
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

	// If profile is not ready, redirect to profile setup
	if !session.IsProfileReady(sess.Profile) {
		sess.Step = "profile:firstName"
		sess.Draft.RecipientName = r.Name
		sess.Draft.RecipientEmail = r.Email
		saveSession(m.deps, c)
		_ = c.Edit(fmt.Sprintf("✅ Обрано: %s\n📧 %s\n\n👋 Спочатку заповнимо профіль.", r.Name, r.Email))
		return c.Send("1️⃣ Введіть ваше *ім'я*:", tb.ModeMarkdown)
	}

	// Set draft recipient and proceed to subject
	sess.Draft.RecipientName = r.Name
	sess.Draft.RecipientEmail = r.Email
	sess.Step = "new:ask_subject"
	saveSession(m.deps, c)

	_ = c.Edit(fmt.Sprintf("✅ Обрано: %s\n📧 %s", r.Name, r.Email))
	return c.Send("Коротка тема запиту (наприклад: «Витрати на ремонт доріг у 2025 році»):")
}

func (m *SearchModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	// No step-based text handling needed for search module
	return false, nil
}
