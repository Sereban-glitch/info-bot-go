package handlers

import (
	"fmt"
	"log"
	"strings"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/session"
)

// ProfileModule handles /profile.
type ProfileModule struct {
	deps    *Deps
	bot     *tb.Bot
	skipBtn *tb.InlineButton
}

func NewProfileModule(deps *Deps) *ProfileModule {
	return &ProfileModule{
		deps: deps,
		bot:  deps.Bot,
	}
}

func (m *ProfileModule) Name() string       { return "profile" }
func (m *ProfileModule) StepPrefix() string { return "profile:" }

func (m *ProfileModule) Register() {
	m.bot.Handle("/profile", safeHandler("profile", m.handleProfile))
	m.bot.Handle("👤 Мій профіль", safeHandler("profile_btn", m.handleProfile))

	// Create and register the skip button inside Register() — this is the
	// telebot v3 pattern that guarantees the button pointer is stable and
	// the callback data (\fpskip) is correctly bound.
	skipBtn := &tb.InlineButton{
		Unique: "pskip",
		Text:   "⏭ Пропустити",
	}
	m.skipBtn = skipBtn // store for use in keyboards
	m.bot.Handle(skipBtn, safeHandler("profile_skip", m.handleSkip))
}

func (m *ProfileModule) handleProfile(c tb.Context) error {
	sess := c.Get("session").(*session.SessionData)
	if session.IsProfileReady(sess.Profile) {
		return m.showProfile(c, sess)
	}
	return m.askNextField(c, sess)
}

func (m *ProfileModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	sess := c.Get("session").(*session.SessionData)
	text = strings.TrimSpace(text)

	switch step {
	case "profile:firstName":
		if text == "" {
			return true, c.Send("❌ Ім'я не може бути порожнім. Введіть ваше ім'я:")
		}
		sess.Profile.FirstName = text
		saveSession(m.deps, c)
		return true, m.askNextField(c, sess)

	case "profile:lastName":
		sess.Profile.LastName = text
		saveSession(m.deps, c)
		return true, m.askNextField(c, sess)

	case "profile:middleName":
		sess.Profile.MiddleName = text
		saveSession(m.deps, c)
		return true, m.askNextField(c, sess)

	case "profile:postalAddress":
		sess.Profile.PostalAddress = text
		saveSession(m.deps, c)
		return true, m.askNextField(c, sess)

	case "profile:email":
		if text != "" && !strings.Contains(text, "@") {
			return true, c.Send("❌ Некоректний email. Введіть ще раз:")
		}
		sess.Profile.Email = text
		if text == "" {
			sess.Draft.UseSharedMailbox = true
		}
		sess.Profile.FullName = session.ProfileDisplayName(sess.Profile)
		saveSession(m.deps, c)
		return true, m.showProfile(c, sess)
	}
	return false, nil
}

func (m *ProfileModule) handleSkip(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	step := sess.Step
	log.Printf("[PROFILE] skip button pressed, step=%s, user=%d", step, c.Sender().ID)

	if !strings.HasPrefix(step, "profile:") {
		return nil
	}

	switch step {
	case "profile:lastName":
		sess.Profile.LastName = ""
	case "profile:middleName":
		sess.Profile.MiddleName = ""
	case "profile:postalAddress":
		sess.Profile.PostalAddress = ""
	case "profile:email":
		sess.Profile.Email = ""
		sess.Draft.UseSharedMailbox = true
	}

	saveSession(m.deps, c)
	return m.askNextField(c, sess)
}

func (m *ProfileModule) askNextField(c tb.Context, sess *session.SessionData) error {
	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{{*m.skipBtn}}

	switch {
	case sess.Profile.FirstName == "":
		sess.Step = "profile:firstName"
		saveSession(m.deps, c)
		return c.Send("1️⃣ Введіть ваше *ім'я*:", tb.ModeMarkdown)

	case sess.Profile.LastName == "":
		sess.Step = "profile:lastName"
		saveSession(m.deps, c)
		return c.Send("2️⃣ Введіть ваше прізвище (або пропустіть):", kb)

	case sess.Profile.MiddleName == "":
		sess.Step = "profile:middleName"
		saveSession(m.deps, c)
		return c.Send("3️⃣ По-батькові (не обов'язково):", kb)

	case sess.Profile.PostalAddress == "":
		sess.Step = "profile:postalAddress"
		saveSession(m.deps, c)
		return c.Send("4️⃣ Поштова адреса (не обов'язково):", kb)

	case sess.Profile.Email == "":
		sess.Step = "profile:email"
		saveSession(m.deps, c)
		return c.Send("5️⃣ Ваш email для відповідей (або пропустіть — використаємо спільну пошту):", kb)

	default:
		return m.showProfile(c, sess)
	}
}

func (m *ProfileModule) showProfile(c tb.Context, sess *session.SessionData) error {
	sess.Step = "idle"
	sess.Profile.FullName = session.ProfileDisplayName(sess.Profile)
	saveSession(m.deps, c)

	name := session.ProfileDisplayName(sess.Profile)
	if name == "" {
		name = "не вказано"
	}
	email := sess.Profile.Email
	if email == "" {
		email = m.deps.Cfg.SharedMailbox + " (спільна пошта)"
	}
	addr := sess.Profile.PostalAddress
	if addr == "" {
		addr = "не вказано"
	}

	text := fmt.Sprintf("✅ *Профіль збережено!*\n\n👤 Ім'я: %s\n📧 Email: %s\n📍 Адреса: %s\n\nТеперь можна: /new", name, email, addr)
	kb := MainMenuKeyboard(m.deps.Cfg, c.Sender().ID)
	return c.Send(text, kb, tb.ModeMarkdown)
}

