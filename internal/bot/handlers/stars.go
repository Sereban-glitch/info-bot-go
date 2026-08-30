package handlers

// Монетизация через Telegram Stars — модуль-каркас (ТЗ «Монетизация»).
//
// ПОКА ВЫКЛЮЧЕНО: cfg.StarsEnabled = false (по умолчанию). В этом режиме:
//   • /buy отвечает «оплата ещё не включена»;
//   • гейт в analyze не работает — розборы бесплатны, как раньше;
//   • pre-checkout на всякий случай отклоняется (инвойсов мы не создаём,
//     но если кто-то пришлёт старую ссылку — Stars не спишутся).
//
// ВКЛЮЧЕНИЕ (когда пользователей станет достаточно): STARS_ENABLED=true
// в .env + рестарт. Дальше всё автоматически:
//   • /buy → кнопка «Оплатить N ⭐» → инвойс-ссылка (createInvoiceLink, XTR);
//   • pre_checkout_query → подтверждение (проверка payload);
//   • successful_payment → зачисление кредитов (с защитой от дублей);
//   • каждый AI-розбор (и в боте, и в мини-приложении) списывает 1 кредит;
//   • новому пользователю один раз — STARS_FREE_CREDITS бесплатных.

import (
	"fmt"
	"log"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/stars"
)

// StarsModule — модуль монетизации (24-й).
type StarsModule struct {
	deps   *Deps
	bot    *tb.Bot
	client *stars.Client
}

// NewStarsModule создаёт модуль монетизации.
func NewStarsModule(deps *Deps) *StarsModule {
	m := &StarsModule{deps: deps, bot: deps.Bot}
	if deps.Cfg != nil && deps.Cfg.BotToken != "" {
		m.client = stars.NewClient(deps.Cfg.BotToken)
	}
	return m
}

func (m *StarsModule) Name() string { return "stars" }

func (m *StarsModule) Register() {
	m.bot.Handle("/buy", safeHandler("stars_buy", m.handleBuy))
	m.bot.Handle("💳 Купити розбори", safeHandler("stars_buy_btn", m.handleBuy))

	// Подсказка «кредиты исчерпаны» из модуля analyze — ведёт на /buy.
	buyHint := tb.InlineButton{Unique: "stars_buy_hint", Text: "💳 Купити розбори"}
	m.bot.Handle(&buyHint, safeHandler("stars_buy_hint", m.handleBuy))

	// Подтверждение оплаты (до списания Stars) — должно прийти и уйти
	// ответом в течение 10 секунд, иначе Telegram отменит оплату.
	m.bot.Handle(tb.OnCheckout, safeHandler("stars_checkout", m.handlePreCheckout))
	// Успешная оплата — зачисляем кредиты.
	m.bot.Handle(tb.OnPayment, safeHandler("stars_paid", m.handlePaid))
}

// handleBuy — /buy: показывает пакет и кнопку оплаты.
func (m *StarsModule) handleBuy(c tb.Context) error {
	cfg := m.deps.Cfg
	if !cfg.StarsEnabled || m.deps.Stars == nil || m.client == nil {
		return c.Send("💳 Оплата розборів ще не ввімкнена.\n\n" +
			"Зараз усі функції бота безкоштовні. Коли зʼявиться оплата — тут буде пакет AI-розборів за Telegram Stars.")
	}

	uid := c.Sender().ID
	m.deps.Stars.EnsureWelcome(uid, cfg.StarsFreeCredits)
	balance := m.deps.Stars.Balance(uid)

	link, err := m.client.CreateInvoiceLink(
		fmt.Sprintf("Пакет: %d AI-розборів", cfg.StarsAnalyzePack),
		"Розбір відповідей органів: тип, законність за ЗУ №2939-VI, порушені статті та готовий документ (скарга/уточнення).",
		stars.BuildPayload(uid, cfg.StarsAnalyzePack),
		cfg.StarsAnalyzePrice,
	)
	if err != nil {
		log.Printf("[STARS] createInvoiceLink user=%d: %v", uid, err)
		return c.Send("⚠️ Не вдалося створити рахунок. Спробуйте за хвилину.")
	}

	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{{
		{Text: fmt.Sprintf("💳 Оплатити %d ⭐", cfg.StarsAnalyzePrice), URL: link},
	}}
	text := fmt.Sprintf("💳 Пакет AI-розборів\n\n• %d розборів за %d Stars\n• 1 розбір = 1 відповідь органу під юридичним аналізом\n\nВаш баланс: %d ⭐️-кредитів",
		cfg.StarsAnalyzePack, cfg.StarsAnalyzePrice, balance)
	return c.Send(text, kb)
}

// handlePreCheckout — подтверждение pre_checkout_query.
func (m *StarsModule) handlePreCheckout(c tb.Context) error {
	q := c.PreCheckoutQuery()
	if q == nil {
		return nil
	}
	// Отключённая монетизация: отказ (Stars возвращаются пользователю).
	if !m.deps.Cfg.StarsEnabled || m.deps.Stars == nil {
		return c.Accept("Оплата ще не ввімкнена")
	}
	// Payload валиден и совпадает с покупателем.
	uid, credits, ok := stars.ParsePayload(q.Payload)
	if !ok || uid != q.Sender.ID || credits != m.deps.Cfg.StarsAnalyzePack {
		log.Printf("[STARS] pre-checkout отклонён: payload=%q from=%d", q.Payload, q.Sender.ID)
		return c.Accept("Рахунок застарів — надішліть /buy ще раз")
	}
	return c.Accept()
}

// handlePaid — successful_payment: зачисление кредитов.
func (m *StarsModule) handlePaid(c tb.Context) error {
	msg := c.Message()
	if msg == nil || msg.Payment == nil {
		return nil
	}
	p := msg.Payment
	log.Printf("[STARS] successful_payment from=%d total=%d %s payload=%q charge=%s",
		msg.Sender.ID, p.Total, p.Currency, p.Payload, p.TelegramChargeID)

	if !m.deps.Cfg.StarsEnabled || m.deps.Stars == nil {
		log.Printf("[STARS] оплата пришла при выключенной монетизации — кредиты не начислены")
		return c.Send("⚠️ Оплату отримано, але налаштування бота не містять монетизації. Напишіть адміністру — кредити нарахуємо вручну.")
	}

	uid, credits, ok := stars.ParsePayload(p.Payload)
	if !ok || uid != msg.Sender.ID {
		log.Printf("[STARS] payload не прошёл валидацию — оплата без зачисления")
		return c.Send("⚠️ Не вдалося зіставити платіж з акаунтом. Напишіть адміністру — повернемо кредити вручну.")
	}
	if !m.deps.Stars.ChargeIfNew(p.TelegramChargeID) {
		log.Printf("[STARS] дубликат платежа %s — пропуск", p.TelegramChargeID)
		return nil // повторная доставка апдейта: молча игнорируем
	}
	if err := m.deps.Stars.Add(uid, credits); err != nil {
		log.Printf("[STARS] зачисление упало: %v", err)
	}
	balance := m.deps.Stars.Balance(uid)
	log.Printf("[STARS] начислено %d кредитов user=%d, баланс=%d", credits, uid, balance)

	return c.Send(fmt.Sprintf("✅ Оплату отримано! Нараховано %d розборів.\n\nВаш баланс: %d ⭐️-кредитів\n\nПодивіться відповідь органу — /analyze", credits, balance))
}
