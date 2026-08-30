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
	signBtn *tb.InlineButton
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

	// Кнопка «Змінити підпис» — отдельный вопрос: подпись в письмах
	// (FullName) может отличаться от полных ФИО, и пользователь имеет
	// право в любой момент заменить её на короткую (только имя).
	signBtn := &tb.InlineButton{
		Unique: "psign",
		Text:   "✍️ Змінити підпис",
	}
	m.signBtn = signBtn
	m.bot.Handle(signBtn, safeHandler("profile_sign", m.handleSignBtn))
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
		if text != "" && !validEmail(text) {
			return true, c.Send("❌ Некоректний email (приклад: ivan@gmail.com). Введіть ще раз або натисніть «Пропустити»:")
		}
		sess.Profile.Email = text
		if text == "" {
			sess.Draft.UseSharedMailbox = true
		}
		if sess.Profile.FullName == "" {
			sess.Profile.FullName = session.ProfileDisplayName(sess.Profile)
		}
		saveSession(m.deps, c)
		return true, m.showProfile(c, sess)

	case "profile:signature":
		name := strings.Join(strings.Fields(text), " ")
		if utf8RuneCount(name) < 2 {
			return true, c.Send("❌ Підпис занадто короткий. Введіть підпис (наприклад: Віктор або Іван Петренко):")
		}
		if utf8RuneCount(name) > 80 {
			return true, c.Send("❌ Занадто довгий підпис (до 80 символів). Спробуйте ще раз:")
		}
		applySignature(sess, name)
		saveSession(m.deps, c)
		_ = c.Send(fmt.Sprintf("✅ Підпис у листах оновлено: <b>%s</b>\n\nНаступні запити до органів підписуватимуться так.", htmlEscape(name)), tb.ModeHTML)
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
	// Защита от nil: если модуль создан без Register() (например, через
	// AllModules вне bot.New), skipBtn может быть nil — раньше это падало
	// panic'ом и убивало весь процесс. Без кнопки клавиатура не добавляется.
	sendOpts := make([]interface{}, 0, 1)
	if m.skipBtn != nil {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{{*m.skipBtn}}
		sendOpts = append(sendOpts, kb)
	}

	switch {
	case sess.Profile.FirstName == "":
		sess.Step = "profile:firstName"
		saveSession(m.deps, c)
		return c.Send("1️⃣ Введіть ваше *ім'я*:", tb.ModeMarkdown)

	case sess.Profile.LastName == "":
		sess.Step = "profile:lastName"
		saveSession(m.deps, c)
		return c.Send("2️⃣ Введіть ваше прізвище (або пропустіть):", sendOpts...)

	case sess.Profile.MiddleName == "":
		sess.Step = "profile:middleName"
		saveSession(m.deps, c)
		return c.Send("3️⃣ По-батькові (не обов'язково):", sendOpts...)

	case sess.Profile.PostalAddress == "":
		sess.Step = "profile:postalAddress"
		saveSession(m.deps, c)
		return c.Send("4️⃣ Поштова адреса (не обов'язково):", sendOpts...)

	case sess.Profile.Email == "":
		sess.Step = "profile:email"
		saveSession(m.deps, c)
		return c.Send("5️⃣ Email (не обов'язково — відповідь і так буде на публічній сторінці запиту та в чаті):", sendOpts...)

	default:
		return m.showProfile(c, sess)
	}
}

func (m *ProfileModule) showProfile(c tb.Context, sess *session.SessionData) error {
	sess.Step = "idle"
	// ВАЖНО: не перезаписываем FullName из частей! FullName — это подпись
	// пользователя, которую он мог нарочно сделать короткой («Віктор») или
	// сокращённой («І. Петренко») через «Змінити подпис». Пересборка из
	// Прізвище+Ім'я молча ломала бы её при каждом просмотре профиля.
	if sess.Profile.FullName == "" {
		sess.Profile.FullName = session.ProfileDisplayName(sess.Profile)
	}
	saveSession(m.deps, c)

	name := session.ProfileDisplayName(sess.Profile)
	if name == "" {
		name = "не вказано"
	}
	sign := session.SignatureName(sess.Profile)
	if sign == "" {
		sign = "не вказано — бот запитає перед відправкою запиту"
	}
	email := sess.Profile.Email
	if email == "" {
		email = "— (не потрібно: відповідь на публічній сторінці запиту)"
	}
	addr := sess.Profile.PostalAddress
	if addr == "" {
		addr = "не вказано"
	}

	text := fmt.Sprintf("✅ *Профіль*\n\n👤 Ім'я: %s\n✍️ Підпис у листах: %s\n📧 Email: %s\n📍 Адреса: %s\n\n_Підпис можна змінити кнопкою нижче — наприклад, лише ім'я, без прізвища._", name, sign, email, addr)

	// Edit button
	editBtn := &tb.InlineButton{
		Unique: "pedit",
		Text:   "✏️ Редагувати дані",
	}
	m.bot.Handle(editBtn, safeHandler("profile_edit", m.handleEdit))

	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	if m.signBtn != nil {
		rows = append(rows, []tb.InlineButton{*m.signBtn})
	}
	rows = append(rows, []tb.InlineButton{*editBtn})
	kb.InlineKeyboard = rows
	return c.Send(text, kb, tb.ModeMarkdown)
}

func (m *ProfileModule) handleEdit(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	// Reset profile to allow re-entry. FullName тоже сбрасываем: после
	// полного переввода данных подпись пересоберётся из новых частей
	// (в конце showProfile заполнит её, только если пуста).
	sess.Profile.FirstName = ""
	sess.Profile.LastName = ""
	sess.Profile.MiddleName = ""
	sess.Profile.PostalAddress = ""
	sess.Profile.Email = ""
	sess.Profile.FullName = ""
	saveSession(m.deps, c)
	return m.askNextField(c, sess)
}

// handleSignBtn — кнопка «✍️ Змінити підпис»: спрашивает новую подпись
// для писем (можно только имя, можно сокращённо).
func (m *ProfileModule) handleSignBtn(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "profile:signature"
	saveSession(m.deps, c)
	current := session.SignatureName(sess.Profile)
	curNote := ""
	if current != "" {
		curNote = fmt.Sprintf("\n📌 Зараз: %s", htmlEscape(current))
	}
	return c.Send(fmt.Sprintf("✍️ *Як підписати листи до органів?*\n\nВведіть підпис — саме так буде підписано ваші запити. Це може бути:\n• повне ім'я — Іван Петренко;\n• лише ім'я — Віктор (прізвище не обов'язкове);\n• скорочено — І. Петренко.%s\n\n⚠️ Підпис має бути вашим *справжнім ім'ям*: вигадане ім'я дає органу право не відповідати (ст. 19 ЗУ «Про доступ до публічної інформації»).", curNote), tb.ModeMarkdown)
}
