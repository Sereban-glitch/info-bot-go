package handlers

import (
	tb "gopkg.in/telebot.v3"
)

// HelpModule handles /help and "ℹ️ Довідка".
type HelpModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewHelpModule(deps *Deps) *HelpModule {
	return &HelpModule{deps: deps, bot: deps.Bot}
}

func (m *HelpModule) Name() string { return "help" }

func (m *HelpModule) Register() {
	handler := safeHandler("help", func(c tb.Context) error {
		text := "Команди:\n" +
			"/new — створити новий запит (через «Доступ до правди»)\n" +
			"/templates — готові шаблони типових тем\n" +
			"/my — список ваших запитів з посиланнями\n" +
			"/status — живі статуси запитів на порталі\n" +
			"/rating — 🏆 рейтинг відкритості органів\n" +
			"/directory — повний список органів\n" +
			"/find <слово> — пошук у довіднику\n" +
			"/profile — заповнити/змінити ваші дані\n" +
			"/inbox — статус сканера відповідей\n" +
			"/hotlines — гарячі лінії\n" +
			"/support — підтримати розвиток проекту 🌟\n" +
			"/cancel — скасувати поточну дію\n\n" +
			"🔍 Або просто надішліть назву органу в чат — знайду на порталі.\n\n" +
			"У групових чатах команди працюють так само. Профіль у кожного користувача свій."

		if c.Sender().ID == m.deps.Cfg.AdminID {
			text += "\n\n*Для власника:*\n/sync — форс-синхронізація з порталом\n/backup — отримати архів коду проєкту"
		}

		kb := MainMenuKeyboard(m.deps.Cfg, c.Sender().ID)
		return c.Send(text, kb)
	})

	m.bot.Handle("/help", handler)
	m.bot.Handle("ℹ️ Довідка", handler)
}
