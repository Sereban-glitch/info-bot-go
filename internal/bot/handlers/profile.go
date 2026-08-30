package handlers

import (
	"fmt"
	"log"
	"strings"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/session"
)

// profileFieldSteps — фіксована послідовність кроків заповнення профілю.
// Курсор рухається тільки вперед: після відповіді ЧИ пропуску бот питає
// НАСТУПНИЙ крок. Стара логіка «перше порожнє поле» (askNextField) робила
// кнопку «Пропустити» нескінченним циклом: пропущене поле лишалося
// порожнім, і бот питав його знову і знову (баг з продакшну, 31.08:
// «2️⃣ Введіть ваше прізвище» приїжджала на кожен клік кнопки).
var profileFieldSteps = []string{
	"profile:firstName",
	"profile:lastName",
	"profile:middleName",
	"profile:postalAddress",
	"profile:email",
}

// isProfileFieldStep — чи належить крок до послідовності полів профілю
// (відрізняємо від "profile:signature" — окреме питання, не поле).
func isProfileFieldStep(step string) bool {
	for _, s := range profileFieldSteps {
		if s == step {
			return true
		}
	}
	return false
}

// nextProfileStep — наступний крок послідовності; "" після останнього.
func nextProfileStep(step string) string {
	for i, s := range profileFieldSteps {
		if s == step {
			if i+1 < len(profileFieldSteps) {
				return profileFieldSteps[i+1]
			}
			return ""
		}
	}
	return ""
}

// profileFieldEmpty — чи порожнє поле, що відповідає кроку.
func profileFieldEmpty(p session.Profile, step string) bool {
	switch step {
	case "profile:firstName":
		return p.FirstName == ""
	case "profile:lastName":
		return p.LastName == ""
	case "profile:middleName":
		return p.MiddleName == ""
	case "profile:postalAddress":
		return p.PostalAddress == ""
	case "profile:email":
		return p.Email == ""
	}
	return false
}

// applySkip — користувач пропустив поле: лишаємо його порожнім
// (для email — спільна скринька, відповідь і так на публічній сторінці).
func applySkip(sess *session.SessionData, step string) {
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
}

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
	// Resume mid-flow first: Step points at the exact question the user
	// was on — including sessions left by the old buggy build, where
	// «Пропустити» looped forever on the same field.
	if isProfileFieldStep(sess.Step) {
		return m.askField(c, sess, sess.Step)
	}
	if session.IsProfileReady(sess.Profile) {
		return m.showProfile(c, sess)
	}
	return m.askFirstEmptyField(c, sess)
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
		return true, m.askField(c, sess, nextProfileStep(step))

	case "profile:lastName":
		sess.Profile.LastName = text
		saveSession(m.deps, c)
		return true, m.askField(c, sess, nextProfileStep(step))

	case "profile:middleName":
		sess.Profile.MiddleName = text
		saveSession(m.deps, c)
		return true, m.askField(c, sess, nextProfileStep(step))

	case "profile:postalAddress":
		sess.Profile.PostalAddress = text
		saveSession(m.deps, c)
		return true, m.askField(c, sess, nextProfileStep(step))

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
		return true, m.showProfile(c, sess) // останній крок → фінальний екран

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

	// Кнопка могла прилетіти зі старого повідомлення під час іншого
	// кроку (наприклад, вводу підпису) або зовсім іншого модуля —
	// нічого не пропускаємо.
	if !isProfileFieldStep(step) {
		return nil
	}

	// Головний фікс: рухаємось ЯВНО до наступного кроку, а не до
	// «першого порожнього поля» — інакше пропущене поле опинялось
	// порожнім і бот питав його знову (нескінченний цикл кнопки).
	applySkip(sess, step)
	return m.askField(c, sess, nextProfileStep(step))
}

// askField — запитує КОНКРЕТНИЙ крок профілю (єдине місце, де малюються
// питання флоу). Порожній step = послідовність завершена → фінальний екран.
func (m *ProfileModule) askField(c tb.Context, sess *session.SessionData, step string) error {
	// Ім'я — обов'язкове (ст. 19 ЗУ: запитувач має бути названий),
	// решта кроків отримують кнопку «Пропустити».
	sendOpts := make([]interface{}, 0, 2)
	if step != "profile:firstName" && m.skipBtn != nil {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{{*m.skipBtn}}
		sendOpts = append(sendOpts, kb)
	}

	switch step {
	case "profile:firstName":
		sess.Step = step
		saveSession(m.deps, c)
		return c.Send("1️⃣ Введіть ваше *ім'я*:", tb.ModeMarkdown)

	case "profile:lastName":
		sess.Step = step
		saveSession(m.deps, c)
		return c.Send("2️⃣ Введіть ваше прізвище (або пропустіть):", sendOpts...)

	case "profile:middleName":
		sess.Step = step
		saveSession(m.deps, c)
		return c.Send("3️⃣ По-батькові (не обов'язково):", sendOpts...)

	case "profile:postalAddress":
		sess.Step = step
		saveSession(m.deps, c)
		return c.Send("4️⃣ Поштова адреса (не обов'язково):", sendOpts...)

	case "profile:email":
		sess.Step = step
		saveSession(m.deps, c)
		return c.Send("5️⃣ Email (не обов'язково — відповідь і так буде на публічній сторінці запиту та в чаті):", sendOpts...)

	default:
		return m.showProfile(c, sess)
	}
}

// askFirstEmptyField — точка входу для нового/незавершеного профілю:
// продовжуємо з першого порожнього поля (курсор далі рухається тільки
// вперед). Використовується ТІЛЬКИ тут, не для просування всередині флоу.
func (m *ProfileModule) askFirstEmptyField(c tb.Context, sess *session.SessionData) error {
	for _, s := range profileFieldSteps {
		if profileFieldEmpty(sess.Profile, s) {
			return m.askField(c, sess, s)
		}
	}
	return m.showProfile(c, sess)
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
	// Після скидання всіх полів флоу починається з початку.
	return m.askField(c, sess, "profile:firstName")
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
	return c.Send(fmt.Sprintf("✍️ *Як підписати листи до органів?*\n\nВведіть підпис — саме так буде підписано ваші запити. Це може бути:\n• повне ім'я — Іван Петренко;\n• лише ім'я — Віктор (прізвище не обов'язкове);\n• скорочено — І. Петренко.%s\n\n💡 Закон не вимагає підтверджувати особу — псевдонім не заборонений. Але справжнє ім'я надійніше: орган бачить конкретного запитувача. А жартівливі підписи шкодять усім — за скаргою можуть заблокувати спільний акаунт порталу, через який працюють усі користувачі.", curNote), tb.ModeMarkdown)
}
