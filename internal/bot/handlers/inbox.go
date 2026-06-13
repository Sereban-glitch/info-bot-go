package handlers

import (
	"fmt"

	tb "gopkg.in/telebot.v3"
)

// InboxModule handles /inbox.
type InboxModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewInboxModule(deps *Deps) *InboxModule {
	return &InboxModule{deps: deps, bot: deps.Bot}
}

func (m *InboxModule) Name() string { return "inbox" }

func (m *InboxModule) Register() {
	m.bot.Handle("/inbox", safeHandler("inbox", func(c tb.Context) error {
		if m.deps.Watcher == nil {
			return c.Send("📭 Сканер пошти вимкнено (немає налаштувань пошти).\n\nВідповіді приходять напряму на вказаний email.")
		}

		enabled, interval, lastScan, lastCount := m.deps.Watcher.Status()
		if !enabled {
			return c.Send("📭 Сканер пошти вимкнено.")
		}

		text := fmt.Sprintf("📥 *Сканер відповідей*\n\n🟢 Активний, перевіряє раз на %d хв.\n🕒 Остання перевірка: %s\n📨 Знайдено відповідей: %d\n\nЗараз виконую позачергову перевірку…",
			interval, lastScan, lastCount)

		_ = c.Send(text, tb.ModeMarkdown)

		processed, matched, _ := m.deps.Watcher.TriggerScan()
		if c.Sender().ID == m.deps.Cfg.AdminID {
			return c.Send(fmt.Sprintf("✅ *Звіт для адміністратора:*\nПереглянуто: %d\nЗнайдено відповідей: %d", processed, matched), tb.ModeMarkdown)
		}
		if matched > 0 {
			return c.Send(fmt.Sprintf("✅ Знайдено %d нових відповідей на ваші запити.", matched))
		}
		return c.Send("✅ Перевірку завершено. Нових відповідей не знайдено.")
	}))
}
