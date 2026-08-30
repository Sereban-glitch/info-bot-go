package handlers

// ТЗ №6 «Розбір відмови» — AI-анализ ответов органов.
//
// Сценарии входа:
//   1. /analyze или кнопка меню «🔍 Розбір відповіді» — бот ждёт текст
//      ответа (можно перешлястить сообщение), фото письма или голосом;
//   2. Кнопка «⚖️ Розібрати відповідь (AI)» на уведомлениях синхронизации
//      с порталом (ответ по существу / смена статуса) — контекст известен:
//      орган, тема и полный текст ответа подтягиваются из гилки портала;
//   3. Пересланный в покое длинный текст — бот предлагает разбор
//      (кнопки «Да/Нет»), поиск органа сохраняется для коротких пересылок.
//
// AI определяет: тип ответа (отказ / отписка / по существу), законность по
// ЗУ № 2939-VI, нарушенные статьи, срок — и готовит ГОТОВЫЙ документ:
// уточнение, жалобу или обращение. Документ можно отправить в ту же гилку
// запроса на портале (SubmitFollowUp) или скопировать отдельным сообщением.
//
// Лимит: 6 разборов в час на пользователя (защита AI-квоты; in-memory,
// без фоновой горутины — чистка ленивая).

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/ai"
	"info-bot-go/internal/dostup"
	"info-bot-go/internal/session"
)

// AnalyzeModule — модуль AI-розбора ответов органов (ТЗ №6).
type AnalyzeModule struct {
	deps   *Deps
	bot    *tb.Bot
	limits *analyzeLimiter

	starsModule *StarsModule // для кнопки «Купити розбори» (может быть nil)
}

// NewAnalyzeModule создаёт модуль розбора.
func NewAnalyzeModule(deps *Deps) *AnalyzeModule {
	return &AnalyzeModule{deps: deps, bot: deps.Bot, limits: newAnalyzeLimiter(6, time.Hour)}
}

// SetStarsModule связывает розбор с монетизацией (кнопка покупки
// появляется в подсказке о пустом балансе).
func (m *AnalyzeModule) SetStarsModule(sm *StarsModule) { m.starsModule = sm }

func (m *AnalyzeModule) Name() string       { return "analyze" }
func (m *AnalyzeModule) StepPrefix() string { return "analyze:" }

func (m *AnalyzeModule) Register() {
	m.bot.Handle("/analyze", safeHandler("analyze", m.handleStart))
	m.bot.Handle("🔍 Розбір відповіді", safeHandler("analyze_menu", m.handleStart))

	// Кнопка на уведомлениях синхронизации: data = slug запроса портала.
	anBtn := tb.InlineButton{Unique: "an_btn"}
	m.bot.Handle(&anBtn, safeHandler("an_btn", m.handleFromNotification))

	// «Да, разобрать» на пересланном тексте.
	fwdYes := tb.InlineButton{Unique: "an_fwd"}
	m.bot.Handle(&fwdYes, safeHandler("an_fwd", m.handleForwardYes))
	fwdNo := tb.InlineButton{Unique: "an_no"}
	m.bot.Handle(&fwdNo, safeHandler("an_no", m.handleForwardNo))

	// Действия с готовым документом.
	threadBtn := tb.InlineButton{Unique: "an_thread"}
	m.bot.Handle(&threadBtn, safeHandler("an_thread", m.handleSendToThread))
	copyBtn := tb.InlineButton{Unique: "an_copy"}
	m.bot.Handle(&copyBtn, safeHandler("an_copy", m.handleCopyDoc))
	cancelBtn := tb.InlineButton{Unique: "an_cancel"}
	m.bot.Handle(&cancelBtn, safeHandler("an_cancel", m.handleCancel))
}

// handleStart — /analyze: инструкция и переход в ожидание ответа органа.
func (m *AnalyzeModule) handleStart(c tb.Context) error {
	if m.deps.Gemini == nil || !m.deps.Gemini.Available() {
		return c.Send("⚠️ AI-розбір зараз недоступний (немає робочого ключа моделі). Спробуйте пізніше.")
	}
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "analyze:waiting_reply"
	sess.Analyze = &AnalyzeDraft{}
	saveSession(m.deps, c)

	text := "⚖️ <b>Розбір відповіді органу</b>\n\n" +
		"Надішліть відповідь органу на ваш запит — розберу її за Законом України «Про доступ до публічної інформації» № 2939-VI:\n" +
		"• ✍️ <b>текстом</b> — скопіюйте відповідь або перешліть повідомлення;\n" +
		"• 📷 <b>фото листа</b> — сфотографуйте документ (якесть фото перевірте очима);\n" +
		"• 🎤 <b>голосом</b> — прочитайте відповідь уголос.\n\n" +
		"Визначу: це відмова чи відповідь по суті, чи законна вона, які статті порушено — і підготую готовий документ (уточнення, скаргу чи звернення).\n\n" +
		"ℹ️ Це автоматична оцінка, а не юридична консультація.\n\n" +
		"Скасувати: /cancel"
	return c.Send(text, tb.ModeHTML)
}

// OfferForward — пересланный в покое длинный текст: предлагаем разбор.
func (m *AnalyzeModule) OfferForward(c tb.Context, text string) error {
	sess := c.Get("session").(*session.SessionData)
	sess.Analyze = &AnalyzeDraft{ReplyText: text}
	sess.Step = "analyze:confirm"
	saveSession(m.deps, c)

	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{{Unique: "an_fwd", Text: "⚖️ Так, розібрати"}, {Unique: "an_no", Text: "❌ Ні"}},
	}
	return c.Send("🔎 Це виглядає як переслане повідомлення.\n\n"+
		"Якщо це <b>відповідь органу</b> на ваш запит — розберу її за Законом № 2939-VI: визначу, відмова це чи відповідь по суті, чи законна вона, і підготую готовий документ (уточнення чи скаргу).\n\n"+
		"Розібрати?", kb, tb.ModeHTML)
}

// HandleText — текст ответа органа (включая пересланные сообщения).
func (m *AnalyzeModule) HandleText(c tb.Context, step string, text string) (bool, error) {
	if step != "analyze:waiting_reply" && step != "analyze:confirm" {
		return false, nil
	}
	sess := c.Get("session").(*session.SessionData)
	d := sess.Analyze
	if d == nil {
		d = &AnalyzeDraft{}
		sess.Analyze = d
	}
	d.ReplyText = strings.TrimSpace(text)
	if utf8RuneCount(d.ReplyText) < 10 {
		return true, c.Send("❌ Тексту замало. Надішліть повний текст відповіді органу (текстом, фото або голосом).")
	}
	saveSession(m.deps, c)
	return true, m.runAnalysis(c, sess, nil)
}

// HandleMedia — фото письма (делегируется из bugreport-обработчика).
func (m *AnalyzeModule) HandleMedia(c tb.Context) error {
	sess := c.Get("session").(*session.SessionData)
	if !strings.HasPrefix(sess.Step, "analyze:") {
		return nil
	}
	msg := c.Message()
	if msg == nil || msg.Photo == nil {
		return c.Send("📷 Надішліть фото листа або текст відповіді. Скасувати: /cancel")
	}
	if m.deps.Gemini == nil || !m.deps.Gemini.Available() {
		return c.Send("⚠️ AI-розбір зараз недоступний. Спробуйте пізніше.")
	}

	d := sess.Analyze
	if d == nil {
		d = &AnalyzeDraft{}
		sess.Analyze = d
	}
	if msg.Caption != "" {
		d.ReplyText = strings.TrimSpace(msg.Caption)
	}
	saveSession(m.deps, c)

	_ = c.Send("📷 Читаю фото листа… (10–30 секунд)")
	data, err := m.downloadPhoto(c)
	if err != nil {
		log.Printf("[ANALYZE] photo download: %v", err)
		return c.Send("❌ Не вдалося завантажити фото. Спробуйте ще раз або надішліть текстом.")
	}
	return m.runAnalysis(c, sess, data)
}

// HandleVoice — ответ органа голосом (делегируется из voice-модуля).
func (m *AnalyzeModule) HandleVoice(c tb.Context) (bool, error) {
	sess := c.Get("session").(*session.SessionData)
	if sess == nil || !strings.HasPrefix(sess.Step, "analyze:") {
		return false, nil
	}
	if m.deps.Gemini == nil || !m.deps.Gemini.Available() {
		return true, c.Send("⚠️ Голосова обробка вимкнена. Надішліть текстом:")
	}

	_ = c.Send("🎧 Розшифровую вашу відповідь…")
	vm := NewVoiceModule(m.deps)
	audioData, err := vm.downloadVoice(c)
	if err != nil {
		return true, c.Send("⚠️ Не вдалося завантажити аудіо. Надішліть текстом:")
	}
	_, _, _, transcript, err := m.deps.Gemini.VoiceToRequest(audioData, "audio/ogg")
	if err != nil {
		return true, c.Send("⚠️ Не вдалося розпізнати. Надішліть текстом:")
	}
	if utf8RuneCount(transcript) < 10 {
		return true, c.Send("❌ Розшифровка занадто коротка. Надішліть текст відповіді:")
	}

	d := sess.Analyze
	if d == nil {
		d = &AnalyzeDraft{}
		sess.Analyze = d
	}
	d.ReplyText = transcript
	saveSession(m.deps, c)
	return true, m.runAnalysis(c, sess, nil)
}

// handleForwardYes — «Да, разобрать» пересланное сообщение.
func (m *AnalyzeModule) handleForwardYes(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	if sess.Analyze == nil || sess.Analyze.ReplyText == "" {
		_ = c.Edit("❌ Текст втрачено. Почніть заново: /analyze")
		return nil
	}
	return m.runAnalysis(c, sess, nil)
}

// handleForwardNo — «Нет» на предложение разбора пересланного.
func (m *AnalyzeModule) handleForwardNo(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "idle"
	sess.Analyze = nil
	saveSession(m.deps, c)
	_ = c.Edit("👌 Ок. Якщо захочете розібрати відповідь органу — команда /analyze.")
	return nil
}

// handleFromNotification — кнопка «Розібрати відповідь» на уведомлении
// синхронизации: подтягиваем контекст из журнала и полный текст ответа
// из гилки портала.
func (m *AnalyzeModule) handleFromNotification(c tb.Context) error {
	_ = c.Respond()
	slug := c.Callback().Data
	if m.deps.Gemini == nil || !m.deps.Gemini.Available() {
		return c.Send("⚠️ AI-розбір зараз недоступний (немає робочого ключа моделі). Спробуйте пізніше.")
	}
	if m.deps.Dostup == nil || m.deps.SentLog == nil {
		return c.Send("❌ Канал порталу не налаштований. Скористайтесь /analyze і надішліть текст відповіді вручну.")
	}
	e := m.deps.SentLog.FindByMessageID("dostup:" + slug)
	if e == nil {
		return c.Send("❌ Запит не знайдено у журналі. Скористайтесь /analyze і надішліть текст відповіді вручну.")
	}

	_ = c.Send("⏳ Завантажую повний текст відповіді з порталу…")
	replyText, err := m.deps.Dostup.GetRequestResponseText(slug, 8000)
	if err != nil {
		log.Printf("[ANALYZE] GetRequestResponseText %s: %v", slug, err)
	}
	if strings.TrimSpace(replyText) == "" {
		replyText = e.ResponseExcerpt // фолбэк: краткий текст из журнала
	}
	if strings.TrimSpace(replyText) == "" {
		return c.Send("❌ Не вдалося отримати текст відповіді. Скористайтесь /analyze і надішліть текст відповіді вручну.")
	}

	organ := e.DostupBody
	if organ == "" {
		organ = e.RecipientName
	}

	sess := c.Get("session").(*session.SessionData)
	sess.Analyze = &AnalyzeDraft{
		Organ:       organ,
		Subject:     e.Subject,
		RequestSlug: slug,
		URL:         e.URL,
		ReplyText:   replyText,
	}
	saveSession(m.deps, c)
	return m.runAnalysis(c, sess, nil)
}

// precheck — доступность AI + лимит + индикатор «печатает».
// Возвращает false, если пользователю уже отправлено сообщение об ошибке.
func (m *AnalyzeModule) precheck(c tb.Context) bool {
	if m.deps.Gemini == nil || !m.deps.Gemini.Available() {
		_ = c.Send("⚠️ AI-розбір зараз недоступний (немає робочого ключа моделі). Спробуйте пізніше.")
		return false
	}
	uid := c.Sender().ID
	// Монетизация: включена — списываем 1 кредит (оплаченные розборы идут
	// без часового лимита). Выключена — бесплатный режим с прежним
	// лимитом 6/час (защита AI-квоты).
	if m.deps.Cfg.StarsEnabled && m.deps.Stars != nil {
		m.deps.Stars.EnsureWelcome(uid, m.deps.Cfg.StarsFreeCredits)
		if !m.deps.Stars.Spend(uid, 1) {
			kb := &tb.ReplyMarkup{}
			kb.InlineKeyboard = [][]tb.InlineButton{{
				{Unique: "stars_buy_hint", Text: "💳 Купити розбори"},
			}}
			_ = c.Send("💰 Безкоштовні розбори вичерпано. Купіть пакет — і продовжимо.", kb)
			return false
		}
	} else if !m.limits.allow(uid) {
		_ = c.Send("⏳ Ліміт розборів вичерпано (6 на годину) — це захист від перевитрати AI-квоти. Спробуйте за годину.")
		return false
	}
	_ = c.Bot().Notify(c.Sender(), tb.Typing)
	return true
}

// buyCmd — кнопка «Купити розбори» из подсказки о пустом балансе.
func (m *AnalyzeModule) buyCmd(c tb.Context) error {
	if sm := m.starsModule; sm != nil {
		return sm.handleBuy(c)
	}
	return c.Send("💳 Оплата розборів ще не ввімкнена.")
}

// runAnalysis — вызов модели, карточка вердикта и готовый документ.
func (m *AnalyzeModule) runAnalysis(c tb.Context, sess *session.SessionData, photo []byte) error {
	d := sess.Analyze
	if d == nil || (strings.TrimSpace(d.ReplyText) == "" && len(photo) == 0) {
		return c.Send("❌ Немає тексту відповіді. Почніть заново: /analyze")
	}
	if !m.precheck(c) {
		return nil
	}
	// Монетизация: если кредит был списан в precheck — при сбое модели
	// возвращаем его пользователю (честная оплата только за результат).
	spentCredit := m.deps.Cfg.StarsEnabled && m.deps.Stars != nil

	// Разбор идёт в два этапа: сперва БЫСТРЫЙ вердикт (пользователь видит
	// оценку за секунды), затем — готовый документ, который печатается
	// в прямом эфире (пилот стриминга).
	_ = c.Send("⏳ Аналізую відповідь… (спершу — швидкий вердикт)")

	analysis, err := m.deps.Gemini.AnalyzeRefusalVerdict(d.Organ, d.Subject, d.ReplyText, photo)
	if err != nil {
		if spentCredit {
			_ = m.deps.Stars.Add(c.Sender().ID, 1)
			_ = c.Send("↩️ Кредит розбору повернено на баланс.")
		}
		log.Printf("[ANALYZE] AI error user=%d: %v", c.Sender().ID, err)
		return c.Send("❌ Не вдалося виконати розбір (помилка моделі). Спробуйте ще раз за хвилину.")
	}

	d.NextStep = analysis.NextStep
	sess.Step = "idle"
	saveSession(m.deps, c)

	if m.deps.Stats != nil {
		m.deps.Stats.IncrementModule("analyze")
	}
	log.Printf("[ANALYZE] user=%d type=%s legal=%s next=%s", c.Sender().ID, analysis.Type, analysis.IsLegal, analysis.NextStep)

	// 1) Карточка вердикта — сразу, не ждём документа.
	if err := sendLongHTML(c, buildVerdictCard(d.Organ, d.Subject, analysis)); err != nil {
		log.Printf("[ANALYZE] verdict send: %v", err)
	}
	// 2) Готовый документ — печатается по мере генерации (стриминг).
	if analysis.NextStep != "none" {
		return m.streamDraft(c, sess, d, analysis)
	}
	return nil
}

// streamDraft — готовит документ с живым отображением: бот присылает
// сообщение-заглушку и дополняет его по мере генерации (пилот стриминга).
// Финальная правка превращает сообщение в готовый документ с кнопками.
func (m *AnalyzeModule) streamDraft(c tb.Context, sess *session.SessionData, d *session.AnalyzeDraft, a *ai.RefusalAnalysis) error {
	// Если заглушку отправить не удалось — тихо уходим в блокирующий режим.
	holder, herr := c.Bot().Send(c.Recipient(), "⏳ <b>Готую документ…</b>\n\n<i>Текст з'являтиметься поступово.</i>", tb.ModeHTML)
	if herr != nil {
		log.Printf("[ANALYZE] stream holder send failed: %v", herr)
		subject, body, derr := m.deps.Gemini.AnalyzeRefusalDocument(a, d.Organ, d.Subject, d.ReplyText, nil)
		if derr != nil {
			log.Printf("[ANALYZE] document error user=%d: %v", c.Sender().ID, derr)
			return nil // вердикт уже доставлен
		}
		d.DraftSubject, d.DraftBody = subject, body
		saveSession(m.deps, c)
		return m.sendDraft(c, d)
	}

	var buf strings.Builder
	lastEdit := time.Now()

	subject, body, derr := m.deps.Gemini.AnalyzeRefusalDocument(a, d.Organ, d.Subject, d.ReplyText, func(delta string) {
		buf.WriteString(delta)
		// Telegram не любит частые правки одного сообщения: обновляем
		// не чаще раза в ~1.5 секунды, а финальный вид всё равно покажем
		// отдельной правкой после завершения генерации.
		if time.Since(lastEdit) >= 1500*time.Millisecond {
			lastEdit = time.Now()
			_, _ = c.Bot().Edit(holder, buildStreamingDocHTML(buf.String()), tb.ModeHTML)
		}
	})
	if derr != nil {
		log.Printf("[ANALYZE] document error user=%d: %v", c.Sender().ID, derr)
		_, _ = c.Bot().Edit(holder, "⚠️ Не вдалося підготувати документ (помилка моделі). Вердикт — вище. Спробуйте розбір ще раз за хвилину.", tb.ModeHTML)
		return nil
	}
	if strings.TrimSpace(body) == "" && strings.TrimSpace(subject) == "" {
		_, _ = c.Bot().Edit(holder, "ℹ️ Документ для цього випадку не потрібен — дивіться вердикт вище.", tb.ModeHTML)
		return nil
	}

	d.DraftSubject, d.DraftBody = subject, body
	saveSession(m.deps, c)

	// Финальный вид документа: если влезает в одно сообщение — правим
	// заглушку; иначе показываем документ отдельным сообщением.
	finalText := buildDraftHTML(d)
	if utf8.RuneCountInString(finalText) <= 4000 {
		kb := draftKeyboard(d, m.deps.Dostup != nil)
		if _, eerr := c.Bot().Edit(holder, finalText, tb.ModeHTML, kb); eerr != nil {
			log.Printf("[ANALYZE] final edit: %v", eerr)
			return m.sendDraft(c, d)
		}
		return nil
	}
	_, _ = c.Bot().Edit(holder, "✅ Документ готовий — нижче.", tb.ModeHTML)
	return m.sendDraft(c, d)
}

// buildStreamingDocHTML — промежуточный показ растущего документа
// (первые ~3400 знаков, чтобы гарантированно влезть в лимит сообщения).
func buildStreamingDocHTML(partial string) string {
	header := "⏳ <b>Готую документ…</b>\n\n"
	r := []rune(partial)
	tail := ""
	if len(r) > 3400 {
		r = r[:3400]
		tail = "\n\n…"
	}
	return header + htmlEscape(string(r)) + tail
}

// sendDraft — готовый документ с кнопками действий.
func (m *AnalyzeModule) sendDraft(c tb.Context, d *AnalyzeDraft) error {
	return sendLongHTMLWithKeyboard(c, buildDraftHTML(d), draftKeyboard(d, m.deps.Dostup != nil))
}

// buildDraftHTML — финальный вид готового документа (HTML).
func buildDraftHTML(d *AnalyzeDraft) string {
	header := fmt.Sprintf("✉️ <b>ГОТОВИЙ ДОКУМЕНТ: %s</b>\n\n", nextStepLabel(d.NextStep))
	body := ""
	if strings.TrimSpace(d.DraftSubject) != "" {
		body += "<b>Тема:</b> " + htmlEscape(d.DraftSubject) + "\n\n"
	}
	body += htmlEscape(d.DraftBody)
	return header + body
}

// draftKeyboard — кнопки действий под готовым документом.
func draftKeyboard(d *AnalyzeDraft, dostupEnabled bool) *tb.ReplyMarkup {
	kb := &tb.ReplyMarkup{}
	var rows [][]tb.InlineButton
	if d.RequestSlug != "" && dostupEnabled {
		rows = append(rows, []tb.InlineButton{{Unique: "an_thread", Text: "✉️ Надіслати у гілку запиту"}})
	}
	rows = append(rows, []tb.InlineButton{{Unique: "an_copy", Text: "📋 Окремим повідомленням (щоб скопіювати)"}})
	rows = append(rows, []tb.InlineButton{{Unique: "an_cancel", Text: "❌ Закрити"}})
	kb.InlineKeyboard = rows
	return kb
}

// handleSendToThread — отправка готового документа в ту же гилку запроса.
func (m *AnalyzeModule) handleSendToThread(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	d := sess.Analyze
	if d == nil || d.RequestSlug == "" || d.DraftBody == "" {
		return c.Send("❌ Чернетку втрачено. Почніть заново: /analyze")
	}
	if m.deps.Dostup == nil {
		return c.Send("❌ Канал «Доступ до правди» не налаштований.")
	}

	_ = c.Edit("⏳ Публікую документ у гілку запиту…")

	full := d.DraftBody
	if strings.TrimSpace(d.DraftSubject) != "" {
		full = strings.TrimSpace(d.DraftSubject) + "\n\n" + d.DraftBody
	}
	sign := session.SignatureName(sess.Profile)
	if sign == "" {
		sign = "Громадянин України"
	}
	full += "\n\nЗ повагою,\n" + sign + "\n" + time.Now().Format("02.01.2006")

	url, err := m.deps.Dostup.SubmitFollowUp(d.RequestSlug, full)
	if err != nil {
		if errors.Is(err, dostup.ErrRateLimited) {
			return c.Edit("⏳ Портал обмежив частоту запитів. Натисніть кнопку повторно через 3–5 хвилин — чернетка збережена.")
		}
		if errors.Is(err, dostup.ErrNotFollowupOwner) {
			return c.Edit("❌ Це запит подано з іншого акаунту порталу — дописати може лише власник. Скопіюйте документ кнопкою «📋» і надішліть його самостійно.")
		}
		log.Printf("[ANALYZE] submit %s: %v", d.RequestSlug, err)
		return c.Edit(fmt.Sprintf("❌ Помилка надсилання: %s\n\nЧернетка збережена, спробуйте ще раз.", err))
	}

	sess.Step = "idle"
	sess.Analyze = nil
	saveSession(m.deps, c)

	text := "✅ <b>Документ надіслано у гілку запиту</b>\n\n" +
		"🔗 Повна переписка (без реєстрації):\n" + htmlLink(url) + "\n\n" +
		"Я продовжую слідкувати за цією гілкою — повідомлю, коли орган відповість."
	return c.Edit(text, tb.ModeHTML)
}

// handleCopyDoc — документ отдельным сообщением (удобно копировать на телефоне).
func (m *AnalyzeModule) handleCopyDoc(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	d := sess.Analyze
	if d == nil || d.DraftBody == "" {
		return c.Send("❌ Чернетку втрачено. Почніть заново: /analyze")
	}
	text := d.DraftSubject + "\n\n" + d.DraftBody
	// Без HTML-режима: чистый текст, который удобно выделить и скопировать.
	if len([]rune(text)) <= 4000 {
		return c.Send(text)
	}
	r := []rune(text)
	for i := 0; i < len(r); i += 4000 {
		end := i + 4000
		if end > len(r) {
			end = len(r)
		}
		_ = c.Send(string(r[i:end]))
	}
	return nil
}

// handleCancel — «Закрыть».
func (m *AnalyzeModule) handleCancel(c tb.Context) error {
	_ = c.Respond()
	sess := c.Get("session").(*session.SessionData)
	sess.Step = "idle"
	sess.Analyze = nil
	saveSession(m.deps, c)
	_ = c.Edit("❌ Закрито.")
	return nil
}

// downloadPhoto скачивает фото письма (как voice.go — через file API бота).
func (m *AnalyzeModule) downloadPhoto(c tb.Context) ([]byte, error) {
	msg := c.Message()
	if msg == nil || msg.Photo == nil {
		return nil, fmt.Errorf("no photo in message")
	}
	file, err := c.Bot().FileByID(msg.Photo.FileID)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", m.deps.Cfg.BotToken, file.FilePath)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("photo download: HTTP %d", resp.StatusCode)
	}
	const maxPhotoBytes = 8 << 20 // 8 МБ — с запасом для фото Telegram
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Карточка вердикта и подписи (чистые функции — тестируются напрямую).
// ---------------------------------------------------------------------------

// buildVerdictCard формирует HTML-карточку вердикта по ответу органа.
func buildVerdictCard(organ, subject string, a *ai.RefusalAnalysis) string {
	var b strings.Builder
	b.WriteString("⚖️ <b>РОЗБІР ВІДПОВІДІ ОРГАНУ</b>\n\n")
	b.WriteString("📋 Тип: <b>" + analysisTypeLabel(a.Type) + "</b>\n")
	if strings.TrimSpace(organ) != "" {
		b.WriteString("🏛 Орган: <b>" + htmlEscape(organ) + "</b>\n")
	}
	if strings.TrimSpace(subject) != "" {
		b.WriteString("📂 Запит: «" + htmlEscape(subject) + "»\n")
	}
	if a.Summary != "" {
		b.WriteString("💬 Стисло: " + htmlEscape(a.Summary) + "\n")
	}
	b.WriteString("\n⚖️ Законність: <b>" + legalityLabel(a.IsLegal) + "</b>\n")
	if a.LegalNotes != "" {
		b.WriteString(htmlEscape(a.LegalNotes) + "\n")
	}
	if len(a.Violations) > 0 {
		b.WriteString("🚩 Порушення:\n")
		for i, v := range a.Violations {
			if i >= 5 {
				b.WriteString("• …\n")
				break
			}
			line := htmlEscape(v.Article)
			if v.Reason != "" {
				line += " — " + htmlEscape(v.Reason)
			}
			b.WriteString("• " + line + "\n")
		}
	}
	b.WriteString("⏱ Строк відповіді: " + deadlineLabel(a.DeadlineOk) + "\n\n")
	if a.Recommendation != "" {
		b.WriteString("💡 <b>Порада:</b> " + htmlEscape(a.Recommendation) + "\n\n")
	}
	if a.NextStep != "none" && a.NextStep != "" {
		b.WriteString("➡️ Наступний крок: <b>" + nextStepLabel(a.NextStep) + "</b>\n\n")
	}
	b.WriteString("ℹ️ Автоматична правова оцінка на основі ЗУ № 2939-VI. Не є юридичною консультацією.")
	return b.String()
}

// analysisTypeLabel — человекочитаемый тип ответа.
func analysisTypeLabel(t string) string {
	switch t {
	case "refusal":
		return "❌ Повна відмова"
	case "partial":
		return "🟡 Часткова відмова / надано не все"
	case "brushoff":
		return "🤷 Відписка (відповідь без суті)"
	case "substantive":
		return "✅ Відповідь по суті"
	case "ack":
		return "📨 Авто-підтвердження отримання"
	}
	return "❔ Незрозуміло"
}

// legalityLabel — подпись оценки законности.
func legalityLabel(s string) string {
	switch s {
	case "legal":
		return "✅ Законна"
	case "illegal":
		return "⚠️ НЕЗАКОННА"
	case "partially":
		return "🟡 Частково законна"
	}
	return "❔ Оцінити неможливо"
}

// deadlineLabel — подпись оценки срока.
func deadlineLabel(s string) string {
	switch s {
	case "ok":
		return "дотримано (5 робочих днів)"
	case "missed":
		return "❗ прострочено"
	}
	return "невідомо"
}

// nextStepLabel — подпись следующего шага.
func nextStepLabel(s string) string {
	switch s {
	case "clarification":
		return "УТОЧНЕННЯ ЗАПИТУ"
	case "complaint":
		return "СКАРГА на розпорядника"
	case "appeal":
		return "ОСКАРЖЕННЯ (вищий орган / уповноважений / суд)"
	}
	return "—"
}

// ---------------------------------------------------------------------------
// Лимит разборов: 6 в час на пользователя, без фоновой горутины.
// ---------------------------------------------------------------------------

type analyzeBucket struct {
	count       int
	windowStart time.Time
}

type analyzeLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[int64]*analyzeBucket
}

func newAnalyzeLimiter(limit int, window time.Duration) *analyzeLimiter {
	return &analyzeLimiter{limit: limit, window: window, buckets: map[int64]*analyzeBucket{}}
}

func (l *analyzeLimiter) allow(userID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[userID]
	if !ok || now.Sub(b.windowStart) >= l.window {
		// Ленивая чистка: не даём карте расти бесконечно.
		if len(l.buckets) > 4096 {
			for id, bb := range l.buckets {
				if now.Sub(bb.windowStart) >= l.window {
					delete(l.buckets, id)
				}
			}
		}
		l.buckets[userID] = &analyzeBucket{count: 1, windowStart: now}
		return true
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

// ---------------------------------------------------------------------------
// Отправка длинных HTML-сообщений (лимит Telegram — 4096 символов).
// ---------------------------------------------------------------------------

const analyzeChunkLimit = 3500

// sendLongHTML отправляет текст частями по абзацам; каждый кусок ≤ лимита.
func sendLongHTML(c tb.Context, text string) error {
	return sendLongHTMLWithKeyboard(c, text, nil)
}

func sendLongHTMLWithKeyboard(c tb.Context, text string, kb *tb.ReplyMarkup) error {
	chunks := splitHTMLMessage(text, analyzeChunkLimit)
	for i, ch := range chunks {
		var opts []interface{}
		opts = append(opts, tb.ModeHTML)
		if kb != nil && i == len(chunks)-1 {
			opts = append(opts, kb)
		}
		if err := c.Send(ch, opts...); err != nil {
			return err
		}
	}
	return nil
}

// splitHTMLMessage режет текст по границам абзацев (\n\n), не ломая теги —
// теги <b> не пересекают границы абзацев в наших сообщениях.
func splitHTMLMessage(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	if len(text) <= limit {
		return []string{text}
	}
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
	}
	for _, p := range paragraphs {
		// Абзац сам по себе слишком длинный — режем по строкам, затем тупо по рунам.
		if len(p) > limit {
			if cur.Len() > 0 {
				flush()
			}
			chunks = append(chunks, splitHard(p, limit)...)
			continue
		}
		if cur.Len() > 0 && cur.Len()+2+len(p) > limit {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
	}
	flush()
	return chunks
}

// splitHard — жёсткая нарезка слишком длинного абзаца (по строкам,
// затем по рунам с байтовым бюджетом — кириллица занимает 2 байта).
func splitHard(p string, limit int) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(p, "\n") {
		if len(line) > limit {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			out = append(out, splitLineByBytes(line, limit)...)
			continue
		}
		if cur.Len() > 0 && cur.Len()+1+len(line) > limit {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n")
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// splitLineByBytes режет длинную строку по границам рун, укладываясь
// в байтовый лимит (лимит Telegram — байты, а не символы).
func splitLineByBytes(line string, limit int) []string {
	var out []string
	var cur strings.Builder
	for _, r := range line {
		rl := utf8.RuneLen(r)
		if cur.Len() > 0 && cur.Len()+rl > limit {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
