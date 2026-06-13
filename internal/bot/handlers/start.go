package handlers

import (
	"fmt"
	"log"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/config"
	"info-bot-go/internal/session"
	"info-bot-go/internal/stats"
)

// safeHandler wraps a telebot handler with panic recovery and error logging.
func safeHandler(name string, fn tb.HandlerFunc) tb.HandlerFunc {
	return func(c tb.Context) error {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] handler %q: %v", name, r)
			}
		}()
		if err := fn(c); err != nil {
			log.Printf("[ERROR] handler %q: %v", name, err)
		}
		return nil
	}
}

// saveSession persists the session from context.
func saveSession(deps *Deps, c tb.Context) {
	sess := c.Get("session").(*session.SessionData)
	key := session.SessionKey(c.Sender().ID)
	if err := deps.Sessions.Set(key, sess); err != nil {
		log.Printf("[ERROR] save session %d: %v", c.Sender().ID, err)
	}
}

// ---------------------------------------------------------------------------
// StartModule
// ---------------------------------------------------------------------------

type StartModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewStartModule(deps *Deps) *StartModule {
	return &StartModule{deps: deps, bot: deps.Bot}
}

func (m *StartModule) Name() string { return "start" }

func (m *StartModule) Register() {
	m.bot.Handle("/start", safeHandler("start", m.handleStart))
}

func (m *StartModule) handleStart(c tb.Context) error {
	_ = c.Get("session").(*session.SessionData)

	// Set chat menu button
	if m.deps.Cfg.MiniAppURL != "" {
		_ = m.bot.SetMenuButton(c.Sender(), &tb.MenuButton{
			Type:   tb.MenuButtonWebApp,
			Text:   "Прозоро",
			WebApp: &tb.WebApp{URL: m.deps.Cfg.MiniAppURL},
		})
	}

	welcome := "👋 Вітаю! Я — *Прозоро*, бот для запитів на публічну інформацію.\n\n" +
		"🛡️ *БЕЗПЕКА ТА АНОНІМНІСТЬ:*\n" +
		"• *Ваші дані захищені:* ми не зберігаємо зайвої інформації.\n" +
		"• *Повна анонімність:* запити йдуть зі спільної пошти бота, ваше прізвище не обов'язкове.\n" +
		"• *Юридична сила:* все згідно Закону України № 2939-VI.\n\n" +
		"🔒 *Мінімум даних.* Достатньо вашого *імені* та *email*.\n\n" +
		"✍️ *Без підпису.* Електронні запити не потребують підпису.\n\n" +
		"📨 *Як це працює.* Відповідь прийде особисто вам у цей чат.\n\n" +
		"▶️ Почати: /profile, потім /new\n" +
		"📚 Готові шаблони: /templates\n" +
		"🔍 Пошук органу: надішліть назву прямо в чат"

	kb := MainMenuKeyboard(m.deps.Cfg, c.Sender().ID)
	return c.Send(welcome, kb, tb.ModeMarkdown)
}

// ---------------------------------------------------------------------------
// StatsModule — handles "📊 Статистика" button
// ---------------------------------------------------------------------------

type StatsModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewStatsModule(deps *Deps) *StatsModule {
	return &StatsModule{deps: deps, bot: deps.Bot}
}

func (m *StatsModule) Name() string { return "stats" }

func (m *StatsModule) Register() {
	m.bot.Handle("/stats", safeHandler("stats", m.handleStats))
	m.bot.Handle("📊 Статистика", safeHandler("stats_btn", m.handleStats))
}

func (m *StatsModule) handleStats(c tb.Context) error {
	isAdmin := m.deps.Cfg.AdminID != 0 && c.Sender().ID == m.deps.Cfg.AdminID

	if isAdmin {
		return m.handleAdminStats(c)
	}
	return m.handleUserStats(c)
}

func (m *StatsModule) handleUserStats(c tb.Context) error {
	requests := m.deps.SentLog.ListByUser(c.Sender().ID)
	sent := len(requests)
	replied := 0
	pending := 0

	for _, r := range requests {
		if r.ReplyReceivedAt != "" || r.Status == "replied" {
			replied++
		} else {
			pending++
		}
	}

	text := fmt.Sprintf("📊 *Ваша статистика:*\n\n"+
		"📨 Запитів надіслано: %d\n"+
		"✅ Відповідей отримано: %d\n"+
		"⏳ Очікуєте відповідь: %d\n\n"+
		"Деталі: /my",
		sent, replied, pending)

	return c.Send(text, tb.ModeMarkdown)
}

func (m *StatsModule) handleAdminStats(c tb.Context) error {
	gs := m.deps.Stats.Get()

	replyRate := 0
	if gs.TotalRequestsSent > 0 {
		replyRate = gs.TotalRepliesReceived * 100 / gs.TotalRequestsSent
	}

	dailyLimit := 280
	dailyRemaining := m.deps.Stats.DailyRemaining(dailyLimit)
	dailyUsed := dailyLimit - dailyRemaining
	if dailyUsed < 0 {
		dailyUsed = 0
	}

	moduleText := ""
	if len(gs.ModuleUsage) > 0 {
		moduleText = "\n📈 *По модулях:*\n"
		for _, name := range []string{"new_request", "voice", "copilot", "templates", "hotlines"} {
			if count, ok := gs.ModuleUsage[name]; ok && count > 0 {
				moduleText += fmt.Sprintf("  • %s: %d\n", moduleLabel(name), count)
			}
		}
	}

	updatedStr := ""
	if gs.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, gs.UpdatedAt); err == nil {
			updatedStr = t.Format("02.01.2006 15:04")
		}
	}

	text := fmt.Sprintf("📊 *Глобальний дашборд:*\n\n"+
		"👥 Унікальних користувачів: %d\n"+
		"📨 Всього запитів: %d\n"+
		"✅ Отримано відповідей: %d (%d%%)\n"+
		"❌ Bounced: %d\n"+
		"📧 Сьогодні надіслано: %d/%d (Brevo ліміт)\n"+
		"%s\n"+
		"🔄 Оновлено: %s",
		gs.TotalUsers, gs.TotalRequestsSent, gs.TotalRepliesReceived,
		replyRate, gs.TotalBounced,
		dailyUsed, dailyLimit,
		moduleText, updatedStr)

	return c.Send(text, tb.ModeMarkdown)
}

func moduleLabel(name string) string {
	labels := map[string]string{
		"new_request": "Нові запити",
		"voice":       "Голосові",
		"copilot":     "Copilot",
		"templates":   "Шаблони",
		"hotlines":    "Гарячі лінії",
	}
	if l, ok := labels[name]; ok {
		return l
	}
	return name
}

// ---------------------------------------------------------------------------
// MainMenuKeyboard — shared helper (REBRANDED: Прозоро)
// ---------------------------------------------------------------------------

func MainMenuKeyboard(cfg *config.Config, userID int64) *tb.ReplyMarkup {
	kb := &tb.ReplyMarkup{ResizeKeyboard: true}

	rows := []tb.Row{
		kb.Row(kb.Text("📝 Новий запит"), kb.Text("📚 Шаблони")),
		kb.Row(kb.Text("📨 Мої запити"), kb.Text("📊 Статистика")),
		kb.Row(kb.WebApp("🚪 Прозоро", &tb.WebApp{URL: cfg.MiniAppURL})),
		kb.Row(kb.Text("📞 Гарячі лінії"), kb.Text("👤 Мій профіль"), kb.Text("ℹ️ Довідка")),
		kb.Row(kb.Text("🐞 Повідомити про помилку"), kb.Text("🌟 Підтримати проект")),
	}

	if cfg.AdminID != 0 && userID == cfg.AdminID {
		rows = append(rows, kb.Row(kb.Text("💾 Бекап проєкту")))
	}

	kb.Reply(rows...)
	return kb
}

var _ = stats.GlobalStats{}
