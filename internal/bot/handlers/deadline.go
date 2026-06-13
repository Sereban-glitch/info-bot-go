package handlers

import (
	"fmt"
	"log"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/sentlog"
)

// DeadlineModule tracks the 5-working-day deadline for government replies
// and sends reminder notifications to users.
type DeadlineModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewDeadlineModule(deps *Deps) *DeadlineModule {
	return &DeadlineModule{deps: deps, bot: deps.Bot}
}

func (m *DeadlineModule) Name() string { return "deadline" }

func (m *DeadlineModule) Register() {
	// /deadline command — show deadline status for your requests
	m.bot.Handle("/deadline", safeHandler("deadline", m.handleDeadline))
	m.bot.Handle("⏰ Терміни", safeHandler("deadline_btn", m.handleDeadline))

	// Background checker — runs every hour
	go m.runChecker()
}

func (m *DeadlineModule) handleDeadline(c tb.Context) error {
	entries := m.deps.SentLog.ListByUser(c.Sender().ID)
	if len(entries) == 0 {
		return c.Send("📭 У вас ще немає відправлених запитів.\n\nПочніть: /new")
	}

	text := "⏰ *Статус ваших запитів:*\n\n"
	hasActive := false

	for _, e := range entries {
		if e.Status == "replied" || e.ReplyReceivedAt != "" {
			text += fmt.Sprintf("✅ %s — відповідь отримано\n", e.RecipientName)
			continue
		}
		if e.Status == "bounced" {
			text += fmt.Sprintf("⚠️ %s — не доставлено\n", e.RecipientName)
			continue
		}

		hasActive = true
		deadline := calcWorkingDaysDeadline(e.Date)
		remaining := time.Until(deadline)
		daysLeft := int(remaining.Hours() / 24)

		if remaining <= 0 {
			text += fmt.Sprintf("🔴 %s — термін порушено! (%s)\n", e.RecipientName, formatDate(deadline))
		} else if daysLeft <= 1 {
			text += fmt.Sprintf("🟠 %s — останній день! (%s)\n", e.RecipientName, formatDate(deadline))
		} else if daysLeft <= 2 {
			text += fmt.Sprintf("🟡 %s — залишилось %d дні (%s)\n", e.RecipientName, daysLeft, formatDate(deadline))
		} else {
			text += fmt.Sprintf("🟢 %s — залишилось %d днів (%s)\n", e.RecipientName, daysLeft, formatDate(deadline))
		}
	}

	if !hasActive {
		text += "\n✅ Всі запити отримали відповідь!"
	}

	return c.Send(text, tb.ModeMarkdown)
}

// runChecker periodically checks for approaching/expired deadlines
// and notifies users proactively.
func (m *DeadlineModule) runChecker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Initial delay
	time.Sleep(30 * time.Second)

	for range ticker.C {
		m.checkDeadlines()
	}
}

func (m *DeadlineModule) checkDeadlines() {
	entries := m.deps.SentLog.ListAll()

	for _, e := range entries {
		// Skip if already replied or bounced
		if e.Status == "replied" || e.Status == "bounced" || e.ReplyReceivedAt != "" {
			continue
		}

		deadline := calcWorkingDaysDeadline(e.Date)
		remaining := time.Until(deadline)
		daysLeft := int(remaining.Hours() / 24)

		chatID := e.ChatID
		if chatID == 0 {
			chatID = e.UserID
		}

		switch {
		case remaining <= 0:
			// Deadline expired — notify user about their right to appeal
			text := fmt.Sprintf(
				"🔴 *Термін відповіді порушено!*\n\n"+
					"Ваш запит до **%s** залишився без відповіді понад 5 робочих днів.\n\n"+
					"За ст. 22 Закону України «Про доступ до публічної інформації» "+
					"ви маєте право:\n"+
					"• Звернутися зі скаргою до керівника органу\n"+
					"• Звернутися до Уповноваженого з прав людини\n"+
					"• Оскаржити в судовому порядку\n\n"+
					"Строк розгляду скарги — 5 робочих днів.",
				e.RecipientName)

			if _, err := m.bot.Send(tb.ChatID(chatID), text, tb.ModeMarkdown); err != nil {
				log.Printf("[DEADLINE] failed to notify user %d: %v", chatID, err)
			}
			// Mark as expired so we don't re-notify
			_ = m.deps.SentLog.MarkExpired(e.MessageID)

		case daysLeft == 1:
			// Last working day warning
			text := fmt.Sprintf(
				"🟠 *Останній робочий день!*\n\n"+
					"Завтра закінчується строк відповіді на ваш запит до **%s**.\n"+
					"Якщо відповідь не надійде — ви матимете право на скаргу.",
				e.RecipientName)

			if _, err := m.bot.Send(tb.ChatID(chatID), text, tb.ModeMarkdown); err != nil {
				log.Printf("[DEADLINE] failed to notify user %d: %v", chatID, err)
			}
		}
	}
}

// calcWorkingDaysDeadline adds 5 Ukrainian working days to the send date.
// Simple implementation: add 7 calendar days (covers at most 1 weekend).
// For full accuracy, use ukrainian holidays calendar.
func calcWorkingDaysDeadline(dateStr string) time.Time {
	t, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		// Try alternative format
		t, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			// Fallback: 7 days from now
			return time.Now().Add(7 * 24 * time.Hour)
		}
	}

	// Add 5 working days (skip weekends)
	daysAdded := 0
	current := t
	for daysAdded < 5 {
		current = current.AddDate(0, 0, 1)
		weekday := current.Weekday()
		if weekday != time.Saturday && weekday != time.Sunday {
			daysAdded++
		}
	}

	return current
}

func formatDate(t time.Time) string {
	return t.Format("02.01.2006")
}

// Ensure sentlog types are used
var _ = sentlog.SentEntry{}
