package handlers

import (
	"fmt"
	"log"
	"strings"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/directory"
	"info-bot-go/internal/dostup"
	"info-bot-go/internal/session"
)

// SearchModule handles inline text search for government agencies.
// When user is in "idle" step and sends text that doesn't match any command:
//  1. ищем распорядителей в каталоге портала «Доступ до правди» (главный канал);
//  2. дополняем результатами локального справочника;
//  3. если ничего не найдено — OSINT-поиск email в интернете (fallback).
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

	dpBtn := tb.InlineButton{Unique: "srch_dostup"}
	m.bot.Handle(&dpBtn, safeHandler("srch_dostup", m.handleDostupSelect))
}

func (m *SearchModule) HandleSearch(c tb.Context, query string) error {
	// --- 1. Каталог портала «Доступ до правди» (главный канал) ---
	if m.deps.Dostup != nil {
		if err := m.deps.Dostup.EnsureSession(); err == nil {
			bodies, err := m.deps.Dostup.SearchBodies(query)
			if err != nil {
				log.Printf("[SEARCH] dostup search error for %q: %v", query, err)
			}
			if len(bodies) > 0 {
				return m.showDostupResults(c, query, bodies)
			}
		}
	}

	// --- 2. Локальный справочник ---
	results := m.deps.Directory.Search(query)
	if len(results) > 0 {
		return m.showLocalResults(c, query, results)
	}

	// --- 3. OSINT fallback ---
	if m.deps.OSINT == nil {
		return c.Send("🔍 Нічого не знайдено. Спробуйте іншу назву або /directory.")
	}

	msg, _ := c.Bot().Send(c.Chat(), "🔍 *Шукаю інформацію в інтернеті...*", tb.ModeMarkdown)

	result, err := m.deps.OSINT.FindEmail(query)
	if err != nil {
		log.Printf("[SEARCH] OSINT error for %q: %v", query, err)
		c.Bot().Edit(msg, "🔍 Нічого не знайдено. Спробуйте іншу назву або /directory.")
		return nil
	}

	if result.Email == "" {
		c.Bot().Edit(msg, "🔍 На жаль, не вдалося знайти орган. Спробуйте іншу назву.")
		return nil
	}

	sessDir := m.deps.Cfg.SessionDir
	if sessDir == "" {
		sessDir = ".sessions_go"
	}
	id := m.deps.Directory.AddLearned(sessDir, result.AgencyName, result.Email)

	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "srch_sel", Text: "✅ Використати цей орган", Data: id}},
		{{Unique: "nr_cancel", Text: "❌ Скасувати"}},
	}

	text := fmt.Sprintf("🌐 *Знайдено в інтернеті!*\n\n🏛 *Орган:* %s\n📧 *Email:* %s\n\nБуде збережено в базу для майбутніх пошуків.",
		result.AgencyName, result.Email)
	c.Bot().Edit(msg, text, kb, tb.ModeMarkdown)
	return nil
}

// showDostupResults показывает найденных на портале распорядителей.
func (m *SearchModule) showDostupResults(c tb.Context, query string, bodies []dostup.Body) error {
	if len(bodies) > 10 {
		bodies = bodies[:10]
	}

	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	for _, b := range bodies {
		name := b.Name
		if len(name) > 55 {
			name = name[:52] + "..."
		}
		rows = append(rows, []tb.InlineButton{
			{Unique: "srch_dostup", Text: "🌐 " + name, Data: b.Slug},
		})
	}
	rows = append(rows, []tb.InlineButton{
		{Unique: "nr_cancel", Text: "❌ Скасувати"},
	})
	kb.InlineKeyboard = rows

	text := fmt.Sprintf("🔍 Знайдено *%d* розпорядників на порталі «Доступ до правди» для «%s»:\n\nОберіть орган — запит буде подано через портал з публічною сторінкою відстеження:",
		len(bodies), query)
	return c.Send(text, kb, tb.ModeMarkdown)
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

// handleDostupSelect — пользователь выбрал распорядителя с портала.
func (m *SearchModule) handleDostupSelect(c tb.Context) error {
	_ = c.Respond()
	slug := c.Callback().Data
	sess := c.Get("session").(*session.SessionData)

	if !session.IsProfileReady(sess.Profile) {
		sess.Step = "profile:firstName"
		sess.Draft.DostupSlug = slug
		saveSession(m.deps, c)
		_ = c.Edit("✅ Розпорядника з порталу обрано.\n\n👋 Спочатку заповнимо профіль.")
		return c.Send("1️⃣ Введіть ваше *ім'я*:", tb.ModeMarkdown)
	}

	sess.Draft.DostupSlug = slug
	sess.Draft.RecipientName = "" // уточним по странице портала
	sess.Step = "new:ask_subject"
	saveSession(m.deps, c)

	// Уточняем название со страницы портала
	dm := NewDostupModule(m.deps)
	if nm := dm.bodyNameBySlug(slug); nm != "" {
		sess.Draft.RecipientName = nm
		saveSession(m.deps, c)
		_ = c.Edit(fmt.Sprintf("✅ Обрано: <b>%s</b>\n🌐 Запит буде опубліковано на dostup.org.ua", htmlEscape(nm)), tb.ModeHTML)
	} else {
		_ = c.Edit("✅ Розпорядника обрано. Введіть назву для підпису в запиті:")
	}

	return c.Send("Коротка тема запиту (наприклад: «Витрати на ремонт доріг у 2025 році»):")
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
		_ = c.Edit(fmt.Sprintf("✅ Обрано: %s\n\n👋 Спочатку заповнимо профіль.", r.Name))
		return c.Send("1️⃣ Введіть ваше *ім'я*:", tb.ModeMarkdown)
	}

	sess.Draft.RecipientName = r.Name
	sess.Draft.RecipientEmail = r.Email
	sess.Step = "new:ask_subject"
	saveSession(m.deps, c)

	// dostup-first: ищем орган на портале
	dm := NewDostupModule(m.deps)
	if m.deps.Dostup != nil {
		_ = c.Edit(fmt.Sprintf("✅ Обрано: %s\n🔎 Шукаю на порталі «Доступ до правди»...", r.Name))
		return dm.BindDostupBody(c, r.Name)
	}
	_ = c.Edit(fmt.Sprintf("✅ Обрано: %s", r.Name))
	return c.Send("Коротка тема запиту (наприклад: «Витрати на ремонт доріг у 2025 році»):")
}

func (m *SearchModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	_ = strings.TrimSpace(text)
	return false, nil
}
