package handlers

import (
        tb "gopkg.in/telebot.v3"

        "info-bot-go/internal/session"
)

// CancelModule handles /cancel command.
type CancelModule struct {
        deps *Deps
        bot  *tb.Bot
}

func NewCancelModule(deps *Deps) *CancelModule {
        return &CancelModule{deps: deps, bot: deps.Bot}
}

func (m *CancelModule) Name() string { return "cancel" }

func (m *CancelModule) Register() {
        m.bot.Handle("/cancel", safeHandler("cancel", func(c tb.Context) error {
                sess := c.Get("session").(*session.SessionData)
                sess.Step = "idle"
                sess.Draft = session.Draft{}
                sess.PRDraft = nil
                saveSession(m.deps, c)
                return c.Send("❌ Дію скасовано.")
        }))
}
