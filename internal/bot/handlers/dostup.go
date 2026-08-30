package handlers

// Модуль отправки информационных запросов через портал «Доступ до правди»
// (dostup.org.ua). В отличие от email-канала, запрос публикуется публично
// на портале, доставляется органу напрямую и получает публичный трекинг-URL.
//
// Поток: кнопка «🌐 Доступ до правди» на экране подтверждения →
// → поиск распорядителя в каталоге по названию →
// → (если несколько совпадений) выбор из списка →
// → двухшаговая подача (preview + publish) → публичный URL запроса.

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/dostup"
	"info-bot-go/internal/moderation"
	"info-bot-go/internal/safego"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
)

// DostupModule обрабатывает канал отправки через dostup.org.ua.
type DostupModule struct {
	deps *Deps
	bot  *tb.Bot
	mod  *ModerationModule // ТЗ №10: антиспровокаційний скринінг (может быть nil)
}

// NewDostupModule создаёт модуль. deps.Dostup может быть nil —
// тогда модуль регистрируется, но сообщает о необходимости настроек.
func NewDostupModule(deps *Deps) *DostupModule {
	return &DostupModule{deps: deps, bot: deps.Bot}
}

// SetModerationModule — скринінг чутливих запитів (ТЗ №10).
func (m *DostupModule) SetModerationModule(mod *ModerationModule) { m.mod = mod }

func (m *DostupModule) Name() string       { return "dostup" }
func (m *DostupModule) StepPrefix() string { return "dostup:" }

func (m *DostupModule) Register() {
	pickBtn := tb.InlineButton{Unique: "dp_pick"}
	m.bot.Handle(&pickBtn, safeHandler("dp_pick", m.handleBodyPick))

	submitBtn := tb.InlineButton{Unique: "dp_submit"}
	m.bot.Handle(&submitBtn, safeHandler("dp_submit", m.handleSubmit))

	signBtn := tb.InlineButton{Unique: "dp_sign"}
	m.bot.Handle(&signBtn, safeHandler("dp_sign", m.askSignature))

	// Дисклеймер публичности портала (показ 1 раз перед первой отправкой)
	discOkBtn := tb.InlineButton{Unique: "dp_disc_ok"}
	m.bot.Handle(&discOkBtn, safeHandler("dp_disc_ok", m.handleDisclosureOk))

	discBackBtn := tb.InlineButton{Unique: "dp_disc_back"}
	m.bot.Handle(&discBackBtn, safeHandler("dp_disc_back", m.handleDisclosureBack))

	backBtn := tb.InlineButton{Unique: "dp_back"}
	m.bot.Handle(&backBtn, safeHandler("dp_back", func(c tb.Context) error {
		_ = c.Respond()
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "new:confirm"
		saveSession(m.deps, c)
		nrm := NewNewRequestModule(m.deps)
		return nrm.showConfirm(c, false)
	}))

	// Каталог розпорядників: разделы + пагинация
	catPickBtn := tb.InlineButton{Unique: "cat_pick"}
	m.bot.Handle(&catPickBtn, safeHandler("cat_pick", m.handleCatPick))

	catBrowseBtn := tb.InlineButton{Unique: "cat_browse"}
	m.bot.Handle(&catBrowseBtn, safeHandler("cat_browse", m.handleCatBrowse))

	catCancelBtn := tb.InlineButton{Unique: "cat_cancel"}
	m.bot.Handle(&catCancelBtn, safeHandler("cat_cancel", func(c tb.Context) error {
		_ = c.Respond()
		_ = c.Edit("❌ Каталог закрито.")
		return nil
	}))

	m.bot.Handle("/sync", safeHandler("sync", m.handleSync))
	m.bot.Handle("/status", safeHandler("status", m.handleStatus))
	m.bot.Handle("/catalog", safeHandler("catalog", func(c tb.Context) error {
		return m.ShowCatalog(c)
	}))
}

// StartDostupFlow вызывается кнопкой «🌐 Надіслати через «Доступ до правди»» из newrequest.go.
// Ищет распорядителя в каталоге портала по названию черновика.
func (m *DostupModule) StartDostupFlow(c tb.Context) error {
	_ = c.Respond()
	if m.deps.Dostup == nil {
		return c.Send("⚠️ Канал «Доступ до правди» не налаштований.\nДодайте DOSTUP_EMAIL та DOSTUP_PASSWORD у .env")
	}

	sess := c.Get("session").(*session.SessionData)
	if sess.Draft.RecipientName == "" || sess.Draft.Subject == "" || sess.Draft.Body == "" {
		return c.Send("❌ Чернетка неповна. Почніть заново: /new")
	}

	// Если распорядитель уже привязан — сразу к подтверждению
	if sess.Draft.DostupSlug != "" {
		sess.Step = "dostup:confirm"
		saveSession(m.deps, c)
		return m.showSubmitConfirm(c, sess.Draft.DostupSlug, sess.Draft.RecipientName)
	}

	_ = c.Edit("🔎 Шукаю розпорядника в каталозі «Доступ до правди»...")
	return m.searchAndOffer(c, sess.Draft.RecipientName)
}

// BindDostupBody ищет орган в каталоге портала и привязывает слаг к черновику.
// Вызывается после выбора получателя (dostup-first поток /new).
//
//	1 совпадение  — авто-привязка, сразу спрашиваем тему;
//	несколько    — клавиатура выбора;
//	0            — подсказка + ручной ввод названия.
func (m *DostupModule) BindDostupBody(c tb.Context, name string) error {
	if m.deps.Dostup == nil {
		// канал не настроен — работаем по-старому (email-поток)
		sess := c.Get("session").(*session.SessionData)
		sess.Step = "new:ask_subject"
		saveSession(m.deps, c)
		return c.Send("Коротка тема запиту (наприклад: «Витрати на ремонт доріг у 2025 році»):")
	}

	_ = c.Send("🔎 Шукаю розпорядника в каталозі «Доступ до правди» (~2150 органів)...")
	return m.searchAndOffer(c, name)
}

// searchAndOffer выполняет поиск с fallback-ами и показывает результаты.
func (m *DostupModule) searchAndOffer(c tb.Context, name string) error {
	sess := c.Get("session").(*session.SessionData)

	// Привязка из памяти каталога: орган уже выбирали — привязываем сразу
	if m.deps.DostupCatalog != nil {
		if b, ok := m.deps.DostupCatalog.LookupBinding(name); ok {
			sess.Draft.DostupSlug = b.Slug
			saveSession(m.deps, c)
			if sess.Draft.Subject != "" && sess.Draft.Body != "" {
				sess.Step = "dostup:confirm"
				saveSession(m.deps, c)
				return m.showSubmitConfirm(c, b.Slug, b.Name)
			}
		}
	}

	if err := m.deps.Dostup.EnsureSession(); err != nil {
		if errors.Is(err, dostup.ErrNotLoggedIn) {
			return c.Send("❌ Обліковий запис порталу не налаштований (DOSTUP_EMAIL/DOSTUP_PASSWORD).")
		}
		return c.Send(fmt.Sprintf("❌ Портал недоступний: %s", err))
	}

	bodies, err := m.deps.Dostup.SearchBodies(name)
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Помилка пошуку: %s", err))
	}
	if len(bodies) == 0 {
		// Пробуем по первым словам названия (без «м.» и прочего)
		words := strings.Fields(name)
		if len(words) > 2 {
			short := strings.Join(words[:3], " ")
			bodies, err = m.deps.Dostup.SearchBodies(short)
			if err == nil && len(bodies) == 0 && len(words) > 1 {
				bodies, err = m.deps.Dostup.SearchBodies(words[0])
			}
		}
	}
	if err != nil {
		return c.Send(fmt.Sprintf("❌ Помилка пошуку: %s", err))
	}

	if len(bodies) == 0 {
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "dp_back", Text: "⬅️ Назад до запиту"}},
		}
		sess.Step = "new:ask_recipient_name"
		saveSession(m.deps, c)
		return c.Send(fmt.Sprintf("ℹ️ В каталозі порталу не знайдено розпорядителя за назвою «%s».\n\nСпробуйте іншу назву (наприклад, скорочену або офіційну). Суди в каталозі порталу відсутні.", name), kb)
	}

	// Ровно одно точное совпадение — авто-привязка
	if len(bodies) == 1 {
		sess.Draft.DostupSlug = bodies[0].Slug
		sess.Draft.RecipientName = bodies[0].Name
		saveSession(m.deps, c)
		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "dp_back", Text: "🔄 Змінити розпорядителя"}},
		}
		_ = c.Send(fmt.Sprintf("✅ Знайдено на порталі: *%s*\n🌐 Запит буде опубліковано на dostup.org.ua", bodies[0].Name), kb, tb.ModeMarkdown)
		// Тема уже заполнена — сразу к подтверждению отправки
		if sess.Draft.Subject != "" && sess.Draft.Body != "" {
			sess.Step = "dostup:confirm"
			saveSession(m.deps, c)
			return m.showSubmitConfirm(c, bodies[0].Slug, bodies[0].Name)
		}
		sess.Step = "new:ask_subject"
		saveSession(m.deps, c)
		return c.Send("Коротка тема запиту (наприклад: «Витрати на ремонт доріг у 2025 році»):")
	}

	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	for _, b := range bodies {
		label := b.Name
		if len(label) > 60 {
			label = label[:57] + "..."
		}
		// Слаги бывают длиннее 64 байт — кнопки идут через реестр (r:N)
		rows = append(rows, []tb.InlineButton{{Unique: "dp_pick", Text: label, Data: registerPick(c.Sender().ID, b.Slug)}})
	}
	rows = append(rows, []tb.InlineButton{
		{Unique: "cat_pick", Text: "📚 Каталог", Data: "root"},
	})
	rows = append(rows, []tb.InlineButton{{Unique: "dp_back", Text: "⬅️ Назад до запиту"}})
	kb.InlineKeyboard = rows

	sess.Step = "dostup:pick_body"
	saveSession(m.deps, c)
	return c.Send(fmt.Sprintf("🌐 Знайдено %d розпорядників. Оберіть адресата запиту:", len(bodies)), kb)
}

// handleBodyPick — пользователь выбрал распорядителя из списка.
func (m *DostupModule) handleBodyPick(c tb.Context) error {
	_ = c.Respond()
	raw := c.Callback().Data
	slug := resolvePickData(c.Sender().ID, raw)
	if slug == "" {
		return c.Send("❌ Кнопку застаріло. Натисніть пошук ще раз.")
	}
	sess := c.Get("session").(*session.SessionData)
	sess.Draft.DostupSlug = slug
	saveSession(m.deps, c)

	// Уточняем название по странице портала — покажем в подтверждении
	bodyName := sess.Draft.RecipientName
	if nm := m.bodyNameBySlug(slug); nm != "" {
		bodyName = nm
		sess.Draft.RecipientName = nm
		saveSession(m.deps, c)
	} else if m.deps.DostupCatalog != nil {
		if cb, ok := m.deps.DostupCatalog.FindBySlug(slug); ok {
			bodyName = cb.Name
			sess.Draft.RecipientName = cb.Name
			saveSession(m.deps, c)
		}
	}

	// Запоминаем привязку — следующий запрос к этому органу привяжется сразу
	if m.deps.DostupCatalog != nil && bodyName != "" {
		m.deps.DostupCatalog.RememberBinding(bodyName, slug, "catalog-pick")
	}

	// Если тема уже есть — экран подтверждения, иначе спрашиваем тему
	if sess.Draft.Subject != "" && sess.Draft.Body != "" {
		sess.Step = "dostup:confirm"
		saveSession(m.deps, c)
		return m.showSubmitConfirm(c, slug, bodyName)
	}
	sess.Step = "new:ask_subject"
	saveSession(m.deps, c)
	return c.Send("Коротка тема запиту (наприклад: «Витрати на ремонт доріг у 2025 році»):")
}

// bodyNameBySlug получает название распорядителя по слагу (страница /body/<slug>).
func (m *DostupModule) bodyNameBySlug(slug string) string {
	page, code, err := m.deps.Dostup.GetPage("/body/" + slug)
	if err != nil || code != 200 {
		return ""
	}
	re := regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	mm := re.FindStringSubmatch(page)
	if mm == nil {
		return ""
	}
	name := strings.Join(strings.Fields(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stripTagsSimple(mm[1])), "Запити до"))), " ")
	if len(name) > 120 {
		name = name[:120]
	}
	return name
}

func stripTagsSimple(s string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
}

// dostupDisclosureText — текст дисклеймера о публичности портала.
// Выделен в функцию для теста-контракта: юридически значимые обещания
// (публичность, маскировка e-mail, предупреждение о чутливих даних,
// переваги бота) не должны теряться при правках копирайта.
func dostupDisclosureText() string {
	return "🌍 <b>Перед першим запитом — важливо знати</b>\n\n" +
		"«Доступ до правди» — <b>публічний портал</b>, і це його сила: " +
		"ваш запит не загубиться, а відповідь органу побачать усі. " +
		"Прозорість — додатковий тиск на орган.\n\n" +
		"Але з цього випливає правило: портал <b>не для чутливих даних</b>.\n\n" +
		"📖 <b>Що буде відкрито для всіх:</b>\n" +
		"• текст вашого запиту та відповідь органу;\n" +
		"• ваш підпис — ім'я та прізвище;\n" +
		"• усе, що ви напишете в тексті: адреси, дати, номери.\n\n" +
		"🔒 <b>Що портал маскує автоматично:</b>\n" +
		"• e-mail — на сторінці запиту він замінений на [ email address ].\n\n" +
		"⚠️ Тому не вказуйте в запиті дату народження, домашню адресу, " +
		"номери документів чи медичні дані — вони стануть публічними. " +
		"Просіть інформацію, а не розповідайте про себе.\n\n" +
		"🤖 <b>Чим я відрізняюся від порталу:</b>\n" +
		"Я лише автоматизую офіційну процедуру (ст. 19–20 ЗУ № 2939-VI), " +
		"але поверх порталу даю те, чого в нього немає:\n" +
		"• 🔔 повідомлю в чат, коли орган відповість, — портал мовчить;\n" +
		"• ⏰ нагадаю, якщо строк (5 робочих днів) мине без відповіді;\n" +
		"• ✨ покращу запит штучним інтелектом;\n" +
		"• 🎙 прийму запит голосом і оформлю в документ."
}

// showDisclosure — экран дисклеймера публичности (шаг dostup:disclose).
// Показывается ровно один раз на пользователя — до первого подтверждения отправки.
func (m *DostupModule) showDisclosure(c tb.Context) error {
	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "dp_disc_ok", Text: "✅ Я зрозумів, продовжити"}},
		{{Unique: "dp_disc_back", Text: "✏️ Назад до запиту"}},
	}
	return c.Send(dostupDisclosureText(), kb, tb.ModeHTML)
}

// handleDisclosureOk — «Я зрозумів»: запоминаем флаг и возвращаем подтверждение.
func (m *DostupModule) handleDisclosureOk(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	sess.DostupDisclosureShown = true
	sess.Step = "dostup:confirm"
	saveSession(m.deps, c)
	if sess.Draft.DostupSlug == "" {
		return c.Send("❌ Чернетку втрачено. Почніть заново: /new")
	}
	return m.showSubmitConfirm(c, sess.Draft.DostupSlug, sess.Draft.RecipientName)
}

// handleDisclosureBack — «Назад до запиту»: вернуть к черновику без потери данных
// (пользователь может убрать чувствительные данные из текста перед отправкой).
func (m *DostupModule) handleDisclosureBack(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "new:confirm"
	saveSession(m.deps, c)
	nrm := NewNewRequestModule(m.deps)
	_ = c.Edit("✏️ Чернетка збережена. Перегляньте текст — приберіть чутливі дані, якщо вони там є.")
	return nrm.showConfirm(c, false)
}

// showSubmitConfirm — экран финального подтверждения подачи на портал.
// Показывает подпись письма: лист к органу подписывается именем пользователя,
// а «Громадський моніторинг» — лишь техническое название аккаунта на портале.
func (m *DostupModule) showSubmitConfirm(c tb.Context, slug, bodyName string) error {
	sess := c.Get("session").(*session.SessionData)

	// Гейт дисклеймера: до первого показа — только дисклеймер.
	// Все пути к подтверждению идут через эту функцию, поэтому
	// пропустить экран невозможно.
	if !sess.DostupDisclosureShown {
		sess.Step = "dostup:disclose"
		saveSession(m.deps, c)
		return m.showDisclosure(c)
	}

	sign := session.SignatureName(sess.Profile)
	signLine := fmt.Sprintf("✍️ Підпис у листі: <b>%s</b>", htmlEscape(sign))
	if sign == "" {
		signLine = "✍️ Підпис у листі: <b>не вказано — обов'язково вкажіть ім'я</b>"
	}

	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "dp_submit", Text: "✅ Надіслати та опублікувати"}},
		{{Unique: "dp_sign", Text: "✏️ Змінити підпис"}},
		{{Unique: "dp_back", Text: "⬅️ Змінити розпорядителя"}},
	}
	statsLine := m.bodyStatsLine(slug)
	warnLine := m.bodyStatsWarning(slug)
	return c.Send(fmt.Sprintf("🌐 <b>Надіслати через «Доступ до правди»:</b>\n\n🏛 Розпорядник: <b>%s</b>\n📩 Тема: %s\n%s%s\n%s\n\n🌍 <b>Нагадування:</b> запит і відповідь органу буде опубліковано відкрито на dostup.org.ua. E-mail портал маскує, чутливих даних у тексті бути не має.\n\nЗапит буде <b>опубліковано на порталі</b> та надіслано органу. Лист до органу підписується вашим ім'ям (ст. 19 ЗУ «Про доступ до публічної інформації»); «Громадський моніторинг» — лише технічна назва акаунта порталу.\n\nВи отримаєте публічне посилання для відстеження — відповідь видно без реєстрації. Я повідомлю в чат, коли орган надішле відповідь по суті (авто-підтвердження про отримання покажу окремо).", htmlEscape(bodyName), htmlEscape(sess.Draft.Subject), statsLine, signLine, warnLine), kb, tb.ModeHTML)
}

// askSignature — кнопка «✏️ Змінити підпис»: спрашивает, как подписать письмо.
func (m *DostupModule) askSignature(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	if sess.Draft.DostupSlug == "" {
		return c.Send("❌ Чернетку втрачено. Почніть заново: /new")
	}
	sess.Step = "dostup:ask_signature"
	saveSession(m.deps, c)
	current := session.SignatureName(sess.Profile)
	curNote := ""
	if current != "" {
		curNote = fmt.Sprintf("\n📌 Зараз: %s", htmlEscape(current))
	}
	return c.Send(fmt.Sprintf("✍️ <b>Як підписати ваш запит?</b>\n\nВведіть ім'я так, як має виглядати підпис у листі до органу. Це може бути:\n•\u00a0повне ім'я — <i>Іван Петренко</i>;\n•\u00a0лише ім'я — <i>Віктор</i> (прізвище не обов'язкове);\n•\u00a0скоро́чено — <i>І. Петренко</i>.%s\n\n💡 <b>Порада:</b> закон не вимагає підтверджувати особу, тож псевдонім не заборонений. Але справжнє ім'я надійніше — орган бачить конкретного запитувача і ретельніше відповідає, а оскаржити мовчанку простіше.\n⚠️ Жартівливі або явно вигадані підписи шкодять усім: за скаргою на такий запит можуть заблокувати спільний акаунт порталу, через який надсилаються запити всіх користувачів.\n🔒 Акаунт на порталі лишається технічним («Громадський моніторинг»), але лист підписано вашим ім'ям.\n📖 Текст запиту разом із підписом буде опубліковано на відкритій сторінці запиту.", curNote), tb.ModeHTML)
}

// HandleText — обработка текстовых шагов dostup-потока.
func (m *DostupModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	sess := c.Get("session").(*session.SessionData)
	text = strings.TrimSpace(text)

	switch step {
	case "dostup:disclose":
		// Пользователь пишет текст вместо нажатия кнопки — направляем к кнопкам.
		return true, c.Send("ℹ️ Натисніть кнопку нижче: ✅ Я зрозумів, продовжити — або ✏️ Назад до запиту.")

	case "dostup:ask_signature":
		name := strings.Join(strings.Fields(text), " ")
		if utf8RuneCount(name) < 2 {
			return true, c.Send("❌ Ім'я занадто коротке. Введіть підпис (наприклад: Віктор або Іван Петренко):")
		}
		if utf8RuneCount(name) > 80 {
			return true, c.Send("❌ Занадто довгий підпис (до 80 символів). Спробуйте ще раз:")
		}
		// FullName хранит подпись в точности как ввёл пользователь;
		// parts обновляем для совместимости (профиль, веб-дашборд).
		applySignature(sess, name)
		sess.Step = "dostup:confirm"
		saveSession(m.deps, c)
		_ = c.Send(fmt.Sprintf("✅ Підпис оновлено: <b>%s</b>", htmlEscape(name)), tb.ModeHTML)
		return true, m.showSubmitConfirm(c, sess.Draft.DostupSlug, sess.Draft.RecipientName)
	}
	return false, nil
}

// utf8RuneCount считает символы (не байты).
func utf8RuneCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// resolveByTitle ищет в «Моїх запитах» портала свежий запрос по теме —
// восстановление после ErrInvalidResponse (портал принял запрос, но
// подтверждение не распарсилось). Пауза даёт порталу время «увидеть»
// новый запрос в списке. Возвращает nil, если поиск не дал результата.
func (m *DostupModule) resolveByTitle(title string) *dostup.RequestInfo {
	if m.deps.Dostup == nil {
		return nil
	}
	time.Sleep(3 * time.Second) // не дёргаем портал сразу после неудачи
	reqs, err := m.deps.Dostup.MyRequestsFull()
	if err != nil {
		log.Printf("[DOSTUP] восстановление: «Мої запити» недоступны: %v", err)
		return nil
	}
	lp := strings.ToLower(title)
	if len(lp) > 30 {
		lp = lp[:30]
	}
	for _, pr := range reqs {
		if strings.Contains(strings.ToLower(pr.Title), lp) &&
			m.deps.SentLog.FindByMessageID("dostup:"+pr.Slug) == nil {
			info := pr.RequestInfo
			return &info
		}
	}
	return nil
}

// recordPendingSubmit фиксирует попытку, чей результат не подтверждён:
// фоновая синхронизация сверит «чужие» запросы портала с этими попытками
// и приписывает запрос настоящему автору.
func (m *DostupModule) recordPendingSubmit(c tb.Context, sess *session.SessionData, title string) {
	if m.deps.PendingSubmits == nil {
		return
	}
	m.deps.PendingSubmits.Add(PendingSubmit{
		UserID: c.Sender().ID,
		ChatID: c.Chat().ID,
		Title:  title,
		Organ:  sess.Draft.RecipientName,
		At:     time.Now(),
	})
	log.Printf("[DOSTUP] попытка зафиксирована как неподтверждённая: %q (user %d)", title, c.Sender().ID)
}

// applySignature обновляет подпись пользователя: FullName хранит точный
// ввод (как пользователь хочет видеть подпись: «Віктор», «Іван Петренко»,
// «І. Петренко»), parts синхронизируются для совместимости (профиль, веб).
func applySignature(sess *session.SessionData, name string) {
	sess.Profile.FullName = name
	words := strings.Fields(name)
	switch {
	case len(words) == 1:
		sess.Profile.FirstName = words[0]
		sess.Profile.LastName = ""
		sess.Profile.MiddleName = ""
	case len(words) == 2:
		sess.Profile.FirstName = words[0]
		sess.Profile.LastName = words[1]
		sess.Profile.MiddleName = ""
	default:
		sess.Profile.FirstName = words[0]
		sess.Profile.LastName = words[len(words)-1]
		sess.Profile.MiddleName = strings.Join(words[1:len(words)-1], " ")
	}
}

// validEmail — минимальная проверка адреса (локальная@часть.домена),
// без регулярных выражений. Отсекает мусор вроде «@» или «a b@c.d»,
// который иначе попадает в письма органам («…поштою: @»).
func validEmail(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at != strings.LastIndexByte(s, '@') {
		return false
	}
	domain := s[at+1:]
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

// handleSubmit — финальная отправка через портал.
func (m *DostupModule) handleSubmit(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	if sess.Draft.DostupSlug == "" {
		return c.Send("❌ Розпорядника не обрано. Почніть заново: /new")
	}
	if m.deps.Dostup == nil {
		return c.Send("❌ Канал не налаштований.")
	}
	// Страховочный гейт дисклеймера: защита от старых кнопок
	// «Надіслати та опублікувати» в сообщениях до обновления.
	if !sess.DostupDisclosureShown {
		sess.Step = "dostup:disclose"
		saveSession(m.deps, c)
		return m.showDisclosure(c)
	}
	// Без имени не отправляем: по ст. 19 ЗУ запрос должен быть подписан
	// именем запрашивающего, иначе орган вправе отказать.
	if session.SignatureName(sess.Profile) == "" {
		return m.askSignature(c)
	}

	// Тема запроса: префикс + тема из черновика
	title := sess.Draft.Subject
	if len(title) > 150 {
		title = title[:147] + "..."
	}

	// Тело запроса: данные профиля + текст
	data := buildDostupBody(sess)

	// ТЗ №10 — антиспровокаційний скринінг: чутливі у воєнний час
	// запити (розвідка/посади/держтаємниця/приватність третіх осіб)
	// НЕ йдуть на спільний акаунт порталу одразу — власник отримує
	// картку з кнопками ✅/❌. Реальний тригер: 30.08.2026 запит про
	// військову посаду Буданова К.О. під підписом «Шварцнегер».
	if m.mod != nil && m.mod.Screening() {
		if v := moderation.Check(title, data, sess.Draft.RecipientName); v.Hold {
			return m.mod.HoldDostup(c, sess, title, data, v)
		}
	}

	_ = c.Edit("⏳ Подаю запит на порталі (двокрокова форма)...")

	info, err := m.deps.Dostup.SubmitRequest(sess.Draft.DostupSlug, title, data)
	if err != nil {
		if errors.Is(err, dostup.ErrRateLimited) {
			return c.Edit("⏳ Портал обмежив частоту запитів («Забагато запитів»).\nНатисніть кнопку повторно через 3–5 хвилин — чернетка збережена.")
		}
		// «Неожиданный ответ сервера»: финальный POST ушёл на портал, но
		// подтвердить создание запроса не удалось (подтверждение не
		// распарсилось, «Мої запити» был недоступен или ограничен по частоте).
		// Запрос при этом МОГ создаться — реальный случай 30.08.2026:
		// бот показал пользователю ошибку, а запрос опубликовался, и
		// синхронизация приписала его владельцу. Пробуем найти запрос
		// в «Моїх запитах» по теме: если он там — считаем отправку успешной.
		if errors.Is(err, dostup.ErrInvalidResponse) {
			if resolved := m.resolveByTitle(title); resolved != nil {
				log.Printf("[DOSTUP] отправка восстановлена через «Мої запити»: %s (ошибка была: %v)", resolved.Slug, err)
				info, err = resolved, nil
			}
		}
	}
	if err != nil {
		log.Printf("[DOSTUP] submit error: %v", err)
		// Даже при ошибке запрос мог создаться на портале: фиксируем попытку,
		// чтобы фоновая синхронизация приписала обнаруженный запрос настоящему
		// автору и сообщила ему об успехе, а не владельцу.
		m.recordPendingSubmit(c, sess, title)
		return c.Edit(fmt.Sprintf("❌ Помилка надсилання: %s\n\nЧернетка збережена, спробуйте ще раз. Якщо портал обмежив частоту — повторіть через 3–5 хвилин.", err))
	}

	// Страховка от чужого адреса (ТЗ №5, фикс спама): если портал вернул
	// адрес, который уже занят ДРУГИМ запросом в журнале, — сверяемся со
	// списком «Мої запити» и берём настоящий свежий запрос. Даже если эта
	// страховка не сработает, журнал обновляет ВСЕ записи с одинаковым
	// идентификатором — спама больше не будет в любом случае.
	if existing := m.deps.SentLog.FindByMessageID("dostup:" + info.Slug); existing != nil && existing.Subject != title {
		log.Printf("[DOSTUP] подозрение на чужой адрес: slug=%s уже записан с темой %q (новая: %q) — сверяю с «Мої запити»",
			info.Slug, existing.Subject, title)
		if reqs, err := m.deps.Dostup.MyRequestsFull(); err == nil {
			lp := strings.ToLower(title)
			if len(lp) > 30 {
				lp = lp[:30]
			}
			for _, pr := range reqs {
				if strings.Contains(strings.ToLower(pr.Title), lp) &&
					m.deps.SentLog.FindByMessageID("dostup:"+pr.Slug) == nil {
					log.Printf("[DOSTUP] сверка: беру свежий запрос %s вместо %s", pr.Slug, info.Slug)
					info = &pr.RequestInfo
					break
				}
			}
		}
	}

	// Лог отправки
	_ = m.deps.SentLog.Append(sentlog.SentEntry{
		MessageID:      "dostup:" + info.Slug,
		ChatID:         c.Chat().ID,
		UserID:         c.Sender().ID,
		RecipientName:  sess.Draft.RecipientName,
		RecipientEmail: "dostup.org.ua",
		Subject:        title,
		Date:           time.Now().Format(time.RFC3339),
		Channel:        "dostup",
		URL:            info.URL,
		DostupBody:     sess.Draft.RecipientName,
		Delivered:      true,
	})

	if m.deps.Stats != nil {
		m.deps.Stats.IncrementRequests()
		m.deps.Stats.IncrementModule("dostup")
	}

	sess.Step = "idle"
	// Гилка доступна для уточнений (followup) — читаем до сброса черновика
	if m.deps.FollowUps != nil {
		m.deps.FollowUps.Upsert(c.Sender().ID, FollowUpThread{
			Slug:    info.Slug,
			Subject: title,
			Organ:   sess.Draft.RecipientName,
			URL:     info.URL,
		})
	}
	// ТЗ №10: власник бачить КОЖНУ відправку через спільний акаунт
	// у реальному часі (крім власних тестів)
	if m.mod != nil {
		m.mod.NotifySent(c.Sender().ID, telegramName(c), telegramUsername(c),
			session.SignatureName(sess.Profile), sess.Draft.RecipientName, title, info.URL)
	}
	sess.Draft = Draft{}
	saveSession(m.deps, c)

	deadline := addWorkingDays(time.Now(), 5).Format("02.01.2006")
	kb := MainMenuKeyboard(m.deps.Cfg, c.Sender().ID)
	text := fmt.Sprintf("✅ <b>Запит опубліковано на «Доступ до правди»!</b>\n\n🔗 <b>Публічне посилання (без реєстрації):</b>\n%s\n\n⏰ Дедлайн відповіді органу: <b>%s</b> (до 5 робочих днів).\n\n📌 Що далі:\n• Я періодично перевіряю портал і <b>повідомлю тут</b>, коли орган надішле відповідь по суті.\n• Авто-підтвердження про отримання запиту покажу окремо — воно не рахується відповіддю.\n• Статус можна перевірити будь-коли: /my або /status\n• Публічність запиту — додатковий тиск на орган.\n⭐ Це перевага бота над самим порталом: портал мовчить — я повідомлю про відповідь і нагадаю про прострочення.", htmlLink(info.URL), deadline)
	return c.Edit(text, kb, tb.ModeHTML)
}

// handleSync — /sync: ручная форс-синхронизация с порталом (для владельца).
func (m *DostupModule) handleSync(c tb.Context) error {
	isAdmin := m.deps.Cfg.AdminID != 0 && c.Sender().ID == m.deps.Cfg.AdminID
	if !isAdmin {
		return c.Send("⏳ Синхронізація з порталом виконується автоматично.")
	}
	if m.deps.DostupSync == nil {
		return c.Send("⚠️ Синхронізація не активна (канал не налаштований).")
	}
	_ = c.Send("🔄 Синхронізуюся з порталом...")
	safego.Go("sync-now", func() {
		report := m.deps.DostupSync.SyncNow(true)
		_, _ = m.bot.Send(c.Chat(), report)
	})
	return nil
}

// handleStatus — /status: живые статусы всех запросов пользователя на портале.
func (m *DostupModule) handleStatus(c tb.Context) error {
	if m.deps.Dostup == nil {
		return c.Send("⚠️ Канал «Доступ до правди» не налаштований.")
	}
	requests := m.deps.SentLog.ListByUser(c.Sender().ID)
	dostupReq := 0
	for _, r := range requests {
		if r.Channel == "dostup" {
			dostupReq++
		}
	}
	if dostupReq == 0 {
		return c.Send("У вас ще немає запитів через «Доступ до правди».\nСтворіть перший: /new")
	}

	_ = c.Send("🔎 Перевіряю статуси на порталі...")
	var b strings.Builder
	b.WriteString("📊 <b>Статуси ваших запитів на порталі:</b>\n\n")
	for _, r := range requests {
		if r.Channel != "dostup" || r.URL == "" {
			continue
		}
		slug := strings.TrimPrefix(r.MessageID, "dostup:")
		st, err := m.deps.Dostup.GetRequestStatus(slug)
		if err != nil {
			b.WriteString(fmt.Sprintf("📂 «%s»\n⚠️ не вдалося перевірити\n\n", htmlEscape(r.Subject)))
			continue
		}
		kindNote := ""
		if st.HasResponse && dostup.ClassifyReply(st.ResponseExcerpt) == dostup.ReplyAcknowledgement {
			kindNote = "\n📄 орган прислав лише авто-підтвердження — чекаємо відповідь по суті"
		}
		b.WriteString(fmt.Sprintf("📂 «%s»\n%s%s\n🔗 Повна переписка: %s\n\n",
			htmlEscape(r.Subject), dostup.StatusLabel(st.Status), kindNote, htmlLink(r.URL)))
		time.Sleep(1 * time.Second)
	}
	if b.Len() > 4000 {
		b.WriteString("(список скорочено; повний перелік — /my)")
	}
	return c.Send(b.String()[:minInt(b.Len(), 4000)], tb.ModeHTML)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// buildDostupBody собирает текст запроса из профиля и черновика
// (по образцу официальных запросов по ЗУ «Про доступ до публічної інформації»).
// Письмо подписывается ИМЕНЕМ ПОЛЬЗОВАТЕЛЯ (SignatureName), а не названием
// аккаунта портала: «Громадський моніторинг» — только технический профиль,
// чиновник же должен видеть конкретного запитувача.
func buildDostupBody(sess *session.SessionData) string {
	var b strings.Builder
	b.WriteString("На підставі статей 1, 13, 19, 20 Закону України «Про доступ до публічної інформації» від 13 січня 2011 року № 2939-VI, які надають право звертатись із запитами до розпорядників інформації, прошу надати наступну інформацію.\n\n")
	b.WriteString(sess.Draft.Body)
	b.WriteString("\n\n")
	name := session.SignatureName(sess.Profile)
	if name == "" {
		name = "Громадянин України" // страховка; handleSubmit не пустит без имени
	}
	b.WriteString("З повагою,\n")
	b.WriteString(name)
	// Строка почты — только если адрес реально похож на адрес: в старых
	// профилях встречался мусор («@»), который попадал в письма органам.
	if sess.Profile.Email != "" && validEmail(sess.Profile.Email) {
		b.WriteString("\nВідповідь прошу надіслати електронною поштою: " + sess.Profile.Email)
	}
	if sess.Profile.PostalAddress != "" {
		b.WriteString("\nПоштова адреса: " + sess.Profile.PostalAddress)
	}
	b.WriteString("\n" + time.Now().Format("02.01.2006"))
	return b.String()
}

var _ = errors.New // страховка импортов

// ---------------------------------------------------------------------------
// Каталог розпорядників (локальный кэш портала)
// ---------------------------------------------------------------------------

// ShowCatalog — /catalog: разделы каталога портала.
func (m *DostupModule) ShowCatalog(c tb.Context) error {
	if m.deps.DostupCatalog == nil || m.deps.DostupCatalog.Count() == 0 {
		return c.Send("⚠️ Каталог порожній. Спробуйте пізніше — він оновлюється автоматично.")
	}
	updated := m.deps.DostupCatalog.SyncedAt()
	updatedStr := updated
	if t, err := time.Parse(time.RFC3339, updated); err == nil {
		updatedStr = t.Format("02.01.2006 15:04")
	}

	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	for _, cat := range dostup.Categories() {
		rows = append(rows, []tb.InlineButton{{Unique: "cat_pick", Text: cat.Title, Data: cat.ID}})
	}
	// Разделы по регионам — компактно в две колонки
	regions := dostup.RegionCodes()
	regionNames := dostup.Regions()
	var row []tb.InlineButton
	for _, code := range regions {
		label := "📍 " + shortRegionName(regionNames[code])
		row = append(row, tb.InlineButton{Unique: "cat_pick", Text: label, Data: "region:" + code})
		if len(row) == 2 {
			rows = append(rows, row)
			row = nil
		}
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []tb.InlineButton{{Unique: "rtg_page", Text: "🏆 Рейтинг органів", Data: "o|0"}})
	rows = append(rows, []tb.InlineButton{{Unique: "cat_cancel", Text: "❌ Закрити"}})
	kb.InlineKeyboard = rows

	return c.Send(fmt.Sprintf("📚 <b>Каталог розпорядників порталу «Доступ до правди»</b>\n\n🏛 Органів у каталозі: <b>%d</b>\n🕒 Оновлено: %s\n\nОберіть розділ — потім орган; запит буде подано через портал:",
		m.deps.DostupCatalog.Count(), updatedStr), kb, tb.ModeHTML)
}

// shortRegionName — короткое имя области для кнопки.
func shortRegionName(full string) string {
	s := strings.TrimSuffix(full, " область")
	s = strings.TrimPrefix(s, "м. ")
	if utf8RuneCount(s) > 16 {
		s = string([]rune(s)[:15]) + "…"
	}
	return s
}

// handleCatPick — выбран раздел каталога (или "root" — назад к разделам).
func (m *DostupModule) handleCatPick(c tb.Context) error {
	_ = c.Respond()
	section := c.Callback().Data
	if section == "root" {
		return m.ShowCatalog(c)
	}
	return m.showCatalogSection(c, section, 0)
}

// handleCatBrowse — пагинация раздела каталога; data = "<section>|<page>".
func (m *DostupModule) handleCatBrowse(c tb.Context) error {
	_ = c.Respond()
	parts := strings.SplitN(c.Callback().Data, "|", 2)
	if len(parts) != 2 {
		_ = c.Edit("❌ Каталог закрито.")
		return nil
	}
	section := parts[0]
	page := atoi(parts[1])
	return m.showCatalogSection(c, section, page)
}

// showCatalogSection — страница органов раздела.
func (m *DostupModule) showCatalogSection(c tb.Context, section string, page int) error {
	if m.deps.DostupCatalog == nil || m.deps.DostupCatalog.Count() == 0 {
		return c.Edit("⚠️ Каталог порожній. Спробуйте пізніше — він оновлюється автоматично.")
	}
	const perPage = 8
	bodies, total := m.deps.DostupCatalog.Browse(section, page, perPage)
	if len(bodies) == 0 {
		if page == 0 {
			return c.Edit("Немає запитів у цій категорії")
		}
		// пустая страница — откат к первой
		bodies, _ = m.deps.DostupCatalog.Browse(section, 0, perPage)
		page = 0
	}

	title := sectionTitle(section)
	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	for _, b := range bodies {
		label := b.Name
		if utf8RuneCount(label) > 55 {
			label = string([]rune(label)[:52]) + "..."
		}
		// Слаги длиннее 64 байт — через реестр (фикс BUTTON_DATA_INVALID)
		rows = append(rows, []tb.InlineButton{{Unique: "dp_pick", Text: label, Data: registerPick(c.Sender().ID, b.Slug)}})
	}

	// Пагинация: ‹ назад | счётчик | вперёд ›
	var nav []tb.InlineButton
	if page > 0 {
		nav = append(nav, tb.InlineButton{Unique: "cat_browse", Text: "⬅️", Data: fmt.Sprintf("%s|%d", section, page-1)})
	}
	nav = append(nav, tb.InlineButton{Unique: "cat_browse", Text: fmt.Sprintf("%d/%d", page+1, (total+perPage-1)/perPage), Data: fmt.Sprintf("%s|%d", section, page)})
	if (page+1)*perPage < total {
		nav = append(nav, tb.InlineButton{Unique: "cat_browse", Text: "➡️", Data: fmt.Sprintf("%s|%d", section, page+1)})
	}
	rows = append(rows, nav)
	rows = append(rows, []tb.InlineButton{
		{Unique: "cat_pick", Text: "📚 Розділи", Data: "root"},
		{Unique: "cat_cancel", Text: "❌ Закрити"},
	})
	kb.InlineKeyboard = rows

	text := fmt.Sprintf("📚 <b>%s</b>\n\nОрганів: %d. Оберіть розпорядника — запит буде подано через портал «Доступ до правди» з публічною сторінкою відстеження.",
		title, total)
	if c.Callback() != nil {
		return c.Edit(text, kb, tb.ModeHTML)
	}
	return c.Send(text, kb, tb.ModeHTML)
}

// sectionTitle — заголовок раздела каталога.
func sectionTitle(section string) string {
	if strings.HasPrefix(section, "region:") {
		code := strings.TrimPrefix(section, "region:")
		if name, ok := dostup.Regions()[code]; ok {
			return name
		}
		return "Область"
	}
	for _, cat := range dostup.Categories() {
		if cat.ID == section {
			return cat.Title
		}
	}
	return "Каталог розпорядників"
}

// bodyStatsWarning — предупреждение о рейтинге органа (просрочки портала).
// Возвращает непустую строку, если орган систематически просрочивает ответы.
func (m *DostupModule) bodyStatsWarning(slug string) string {
	if m.deps.Dostup == nil || slug == "" {
		return ""
	}
	st, err := m.deps.Dostup.BodyStatsCached(slug, true)
	if err != nil || st == nil || st.Requests < 3 {
		return ""
	}
	pct := st.OverduePct()
	if pct >= 30 || st.Successful == 0 && st.Requests >= 5 {
		return fmt.Sprintf("\n\n⚠️ <b>Увага:</b> у цього органу прострочено %d з %d запитів (%d%%) — відповідь, на жаль, може затриматись.",
			st.Overdue, st.Requests, pct)
	}
	return ""
}

// bodyStatsLine — строка рейтинга органа (для подтверждения отправки).
func (m *DostupModule) bodyStatsLine(slug string) string {
	if m.deps.Dostup == nil || slug == "" {
		return ""
	}
	st, err := m.deps.Dostup.BodyStatsCached(slug, true)
	if err != nil || st == nil || st.Requests == 0 {
		return ""
	}
	var b strings.Builder
	if idx, ok := dostup.OpennessIndex(st); ok {
		b.WriteString(fmt.Sprintf("\n📊 <b>Індекс відкритості: %d/100</b> %s\nПо суті %d із %d запитів · прострочено %d%%.",
			idx, dostup.RatingBadge(idx), st.Successful, st.Requests, st.OverduePct()))
		// Среднее время ответа — наши данные (портал таймингов не отдаёт)
		if m.deps.SentLog != nil {
			if name := m.bodyNameBySlug(slug); name != "" {
				if t, ok := m.deps.SentLog.AvgResponseHoursByBody()[strings.ToLower(name)]; ok && t.Count >= 2 {
					b.WriteString(fmt.Sprintf("\n⏱ Сер. час відповіді за нашими даними: %.1f год (n=%d).", t.Hours, t.Count))
				}
			}
		}
	} else {
		b.WriteString(fmt.Sprintf("\n📊 Запитів до органу на порталі: %d, по суті відповідей: %d, прострочено: %d.",
			st.Requests, st.Successful, st.Overdue))
	}
	return b.String()
}
