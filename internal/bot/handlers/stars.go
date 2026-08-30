package handlers

// Монетизация через Telegram Stars — модуль-каркас (ТЗ «Монетизация»),
// он же ЕДИНЫЙ РОУТЕР ПЛАТЕЖЕЙ для всего бота.
//
// Модуль регистрируется ПОСЛЕДНИМ, а telebot хранит обработчики в map —
// второй Handle на тот же endpoint перезаписывает первый. Поэтому все
// pre_checkout_query / successful_payment (и донаты /support, и покупка
// кредитов) обязаны обрабатываться здесь, в одном месте, с маршрутизацией
// по payload:
//   • support_<amount>    — добровольный донат (работает ВСЕГДА, независимо
//     от STARS_ENABLED — это не монетизация, а поддержка проекта);
//   • analyze:<uid>:<credits>:<ts> — покупка пакета розборов (гейтится
//     STARS_ENABLED).
//
// ПОКА ВЫКЛЮЧЕНО: cfg.StarsEnabled = false (по умолчанию). В этом режиме:
//   • /buy отвечает «оплата ещё не включена»;
//   • гейт в analyze не работает — розборы бесплатны, как раньше;
//   • донаты /support работают как раньше (это регрессия-фикс: из-за
//     перезаписи обработчиков они на время сломались);
//   • pre-checkout покупок отклоняется (Stars возвращаются пользователю).
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
	"strings"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/stars"
)

// SupportPayloadPrefix — префикс payload донатов /support.
const SupportPayloadPrefix = "support_"

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

// payloadKind — тип платежа по payload.
type payloadKind int

const (
	payloadUnknown payloadKind = iota // не наш формат — отклоняем
	payloadSupport                    // донат /support: support_<amount>
	payloadAnalyze                    // покупка кредитов: analyze:uid:credits:ts
)

// classifyPayload определяет тип платежа по полезной нагрузке инвойса.
func classifyPayload(p string) payloadKind {
	if strings.HasPrefix(p, SupportPayloadPrefix) {
		return payloadSupport
	}
	if _, _, ok := stars.ParsePayload(p); ok {
		return payloadAnalyze
	}
	return payloadUnknown
}

// preCheckoutDecision решает, принимать ли оплату на этапе pre-checkout.
// Чистая функция (без I/O) — чтобы покрыть тестами регрессию с донатами.
// Возвращает (принять, сообщение об ошибке для пользователя).
func preCheckoutDecision(starsEnabled bool, pack int, payload string, senderID int64) (bool, string) {
	switch classifyPayload(payload) {
	case payloadSupport:
		// Донаты — добровольная поддержка, работают всегда.
		return true, ""
	case payloadAnalyze:
		if !starsEnabled {
			return false, "Оплата ще не ввімкнена"
		}
		uid, credits, ok := stars.ParsePayload(payload)
		if !ok || uid != senderID || credits != pack {
			return false, "Рахунок застарів — надішліть /buy ще раз"
		}
		return true, ""
	default:
		return false, "Невідомий рахунок. Скористайтеся /support або /buy"
	}
}

// handlePreCheckout — подтверждение pre_checkout_query (единый роутер).
// Ответ должен уйти в течение 10 секунд, иначе Telegram отменит оплату.
func (m *StarsModule) handlePreCheckout(c tb.Context) error {
	q := c.PreCheckoutQuery()
	if q == nil {
		return nil
	}
	ok, errMsg := preCheckoutDecision(m.deps.Cfg.StarsEnabled, m.deps.Cfg.StarsAnalyzePack, q.Payload, q.Sender.ID)
	if !ok {
		log.Printf("[STARS] pre-checkout отклонён: payload=%q from=%d: %s", q.Payload, q.Sender.ID, errMsg)
		return c.Accept(errMsg)
	}
	return c.Accept()
}

// handlePaid — successful_payment (единый роутер): донаты благодарим
// и рапортуем админу, покупки кредитов зачисляем на баланс.
func (m *StarsModule) handlePaid(c tb.Context) error {
	msg := c.Message()
	if msg == nil || msg.Payment == nil {
		return nil
	}
	p := msg.Payment
	log.Printf("[STARS] successful_payment from=%d total=%d %s payload=%q charge=%s",
		msg.Sender.ID, p.Total, p.Currency, p.Payload, p.TelegramChargeID)

	switch classifyPayload(p.Payload) {
	case payloadSupport:
		return m.donationPaid(c, p)
	case payloadAnalyze:
		return m.creditsPaid(c, p)
	default:
		log.Printf("[STARS] неизвестный payload %q — оплата без зачисления", p.Payload)
		return c.Send("⚠️ Не вдалося визначити тип платежу. Напишіть адміністру — розберемося вручну.")
	}
}

// donationPaid — успешный донат /support: «спасибо» + уведомление админу.
// Дубликаты апдейта Telegram (повторная доставка) пропускаем молча.
func (m *StarsModule) donationPaid(c tb.Context, p *tb.Payment) error {
	if m.deps.Stars != nil && !m.deps.Stars.ChargeIfNew(p.TelegramChargeID) {
		log.Printf("[STARS] дубликат доната %s — пропуск", p.TelegramChargeID)
		return nil
	}
	_ = c.Send("🎉 *Дякуємо за підтримку!*\n\nТвій внесок допоможе проекту стати сильнішим. 💪", tb.ModeMarkdown)
	m.notifyAdmin(fmt.Sprintf("💰 *НОВИЙ ДОНАТ!*\n\n💎 Сума: *%d 🌟*\n👤 Від: %s (ID: %d)",
		p.Total, c.Sender().FirstName, c.Sender().ID))
	return nil
}

// creditsPaid — успешная покупка пакета розборов: зачисление кредитов.
func (m *StarsModule) creditsPaid(c tb.Context, p *tb.Payment) error {
	if !m.deps.Cfg.StarsEnabled || m.deps.Stars == nil {
		log.Printf("[STARS] оплата пришла при выключенной монетизации — кредиты не начислены")
		return c.Send("⚠️ Оплату отримано, але налаштування бота не містять монетизації. Напишіть адміністру — кредити нарахуємо вручну.")
	}

	uid, credits, ok := stars.ParsePayload(p.Payload)
	if !ok || uid != c.Sender().ID {
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
	m.notifyAdmin(fmt.Sprintf("🛒 *ПОКУПКА РОЗБОРІВ!*\n\n💎 %d ⭐ → %d кредитів\n👤 Від: %s (ID: %d)\n💰 Баланс: %d",
		m.deps.Cfg.StarsAnalyzePrice, credits, c.Sender().FirstName, uid, balance))

	return c.Send(fmt.Sprintf("✅ Оплату отримано! Нараховано %d розборів.\n\nВаш баланс: %d ⭐️-кредитів\n\nПодивіться відповідь органу — /analyze", credits, balance))
}

// notifyAdmin — уведомление владельцу о платёжном событии (донат/покупка).
func (m *StarsModule) notifyAdmin(text string) {
	if m.deps.Cfg.AdminID == 0 {
		return
	}
	if _, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), text, tb.ModeMarkdown); err != nil {
		log.Printf("[STARS] уведомление админу не ушло: %v", err)
	}
}
