package handlers

import (
	"fmt"
	"time"

	tb "gopkg.in/telebot.v3"
)

// MyRequestsModule handles /my.
type MyRequestsModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewMyRequestsModule(deps *Deps) *MyRequestsModule {
	return &MyRequestsModule{deps: deps, bot: deps.Bot}
}

func (m *MyRequestsModule) Name() string { return "my-requests" }

func (m *MyRequestsModule) Register() {
	handler := safeHandler("my-requests", func(c tb.Context) error {
		requests := m.deps.SentLog.ListByUser(c.Sender().ID)
		if len(requests) == 0 {
			return c.Send("У вас ще немає надісланих запитів через цього бота.")
		}

		text := fmt.Sprintf("📨 *Ваші запити (останні %d):*\n\n", len(requests))
		for i, r := range requests {
			sentDate, _ := time.Parse(time.RFC3339, r.Date)
			deadline := addWorkingDays(sentDate, 5).Format("02.01.2006")
			formatted := sentDate.Format("02.01.2006")

			status := "⏳"
			if r.Delivered {
				status = "✅"
			} else if r.Status == "bounced" {
				status = "❌"
			}

			text += fmt.Sprintf("%d. %s 🗓 *%s*\n🏛 *Кому:* %s\n📂 *Тема:* %s\n⏰ *Дедлайн:* %s\n\n",
				i+1, status, formatted, r.RecipientName, r.Subject, deadline)
		}

		if len(text) > 4000 {
			text = text[:4000] + "..."
		}
		return c.Send(text, tb.ModeMarkdown)
	})

	m.bot.Handle("/my", handler)
	m.bot.Handle("📨 Мої запити", handler)
}
