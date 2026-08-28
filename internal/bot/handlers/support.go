package handlers

import (
	"fmt"
	"log"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/session"
)

// SupportModule handles /support and Stars donations.
type SupportModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewSupportModule(deps *Deps) *SupportModule {
	return &SupportModule{deps: deps, bot: deps.Bot}
}

func (m *SupportModule) Name() string       { return "support" }
func (m *SupportModule) StepPrefix() string { return "support:" }

func (m *SupportModule) Register() {
	handler := safeHandler("support", func(c tb.Context) error {
		text := "🌟 *Підтримай розвиток «Інфо-Помічника»*\n\n" +
			"Цей бот створений *людьми для людей*. Ми віримо, що кожен має право знати правду про роботу влади.\n\n" +
			"Ми — повністю незалежний проект. У нас немає спонсорів, крім вас.\n\n" +
			"Твоя підтримка у вигляді *Telegram Stars* — це паливо для нашого розвитку."

		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{
				{Unique: "donate", Text: "🌟 1", Data: "1"},
				{Unique: "donate", Text: "🌟 10", Data: "10"},
				{Unique: "donate", Text: "🌟 50", Data: "50"},
			},
			{{Unique: "donate_custom", Text: "💎 Інша сума"}},
		}
		return c.Send(text, kb, tb.ModeMarkdown)
	})

	m.bot.Handle("/support", handler)
	m.bot.Handle("🌟 Підтримати проект", handler)

	donateBtn := tb.InlineButton{Unique: "donate"}
	m.bot.Handle(&donateBtn, safeHandler("donate", func(c tb.Context) error {
		_ = c.Respond()
		amount := atoi(c.Callback().Data)
		if amount > 0 {
			return m.sendStarsInvoice(c, amount)
		}
		return nil
	}))

	customBtn := tb.InlineButton{Unique: "donate_custom"}
	m.bot.Handle(&customBtn, safeHandler("donate_custom", func(c tb.Context) error {
		_ = c.Respond()
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "support:amount"
		saveSession(m.deps, c)
		return c.Send("✍️ Введіть кількість зірок (1-10000):")
	}))

	m.bot.Handle(tb.OnCheckout, func(c tb.Context) error {
		return c.Accept()
	})

	m.bot.Handle(tb.OnPayment, safeHandler("payment", func(c tb.Context) error {
		_ = c.Send("🎉 *Дякуємо за підтримку!*\n\nТвій внесок допоможе проекту стати сильнішим. 💪", tb.ModeMarkdown)

		payment := c.Message().Payment
		if payment != nil && m.deps.Cfg.AdminID != 0 {
			adminText := fmt.Sprintf("💰 *НОВИЙ ДОНАТ!*\n\n💎 Сума: *%d 🌟*\n👤 Від: %s (ID: %d)",
				payment.Total, c.Sender().FirstName, c.Sender().ID)
			_, _ = m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), adminText, tb.ModeMarkdown)
		}
		return nil
	}))
}

func (m *SupportModule) sendStarsInvoice(c tb.Context, amount int) error {
	invoice := &tb.Invoice{
		Title:       "Підтримка проекту",
		Description: fmt.Sprintf("Внесок на розвиток бота (%d 🌟)", amount),
		Payload:     fmt.Sprintf("support_%d", amount),
		Currency:    "XTR",
		Prices: []tb.Price{
			{Label: "Зірки", Amount: amount},
		},
	}
	return c.Send(invoice)
}

func (m *SupportModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	if step != "support:amount" {
		return false, nil
	}
	amount := atoi(text)
	if amount <= 0 || amount > 10000 {
		return true, c.Send("❌ Введіть ціле число від 1 до 10 000.")
	}
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "idle"
	saveSession(m.deps, c)
	log.Printf("[SUPPORT] amount=%d from user=%d", amount, c.Sender().ID)
	return true, m.sendStarsInvoice(c, amount)
}
