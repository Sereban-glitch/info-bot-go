package handlers

import (
	"fmt"
	"log"

	tb "gopkg.in/telebot.v3"
)

// AppLoginModule handle /login — одноразовий код входу в нативний
// застосунок «Смарт Запит» (Capacitor-обгортка міні-застосунку).
// Код діє 30 хвилин і обмінюється на підписаний initData через
// POST /api/app/auth (web-сервер).
type AppLoginModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewAppLoginModule(deps *Deps) *AppLoginModule {
	return &AppLoginModule{deps: deps, bot: deps.Bot}
}

func (m *AppLoginModule) Name() string { return "applogin" }

func (m *AppLoginModule) Register() {
	m.bot.Handle("/login", safeHandler("applogin", m.handleLogin))
	m.bot.Handle("🔑 Вхід у застосунок", safeHandler("applogin_btn", m.handleLogin))
}

func (m *AppLoginModule) handleLogin(c tb.Context) error {
	if m.deps.AppLogin == nil {
		return c.Send("Вхід у застосунок зараз недоступний. Спробуйте пізніше.")
	}
	code := m.deps.AppLogin.Generate(c.Sender().ID)
	log.Printf("[APPLOGIN] code для user=%d", c.Sender().ID)
	return c.Send(fmt.Sprintf(
		"🔑 Код входу в застосунок «Смарт Запит»:\n\n<b>%s</b>\n\n"+
			"1. Відкрийте застосунок на телефоні\n"+
			"2. Натисніть «Увійти з кодом»\n"+
			"3. Введіть цей код\n\n"+
			"Код діє <b>30 хвилин</b> і використовується один раз.\n"+
			"Отримати новий код — команда /login.",
		code), tb.ModeHTML)
}
