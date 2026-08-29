package handlers

// Фоновая синхронизация с порталом «Доступ до правди» (dostup.org.ua).
//
// Портал — источник истины: запросы, поданные ЛЮБЫМ способом с аккаунта
// (через бота, через CLI-инструмент dostup-submit, вручную в браузере),
// попадают на страницу «Мої запити». Воркер:
//
//   1) тянет список запросов портала и добавляет неизвестные в sentlog
//      (счётчики и «Мої запити» бота всегда сходятся с порталом);
//   2) для каждого запроса проверяет публичную страницу статуса и
//      КЛАССИФИЦИРУЕТ входящие сообщения органа:
//        • ответ по существу → запрос закрывается, счётчик ответов +1,
//          пользователю приходит «📬 Відповідь по суті»;
//        • авто-подтверждение («ваш лист отримано») → запрос остаётся
//          в ожидании, счётчик НЕ растёт, приходит отдельное уведомление
//          «📄 Проміжна відповідь»;
//      уведомления дедуплицируются по id входящего (incoming-N);
//   3) согласует глобальные счётчики с фактическими данными (точно,
//      в т.ч. в меньшую сторону при откате ошибочно засчитанного автоответа).
//
// Период: DOSTUP_SYNC_MINUTES (по умолчанию 20) + джиттер.

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/dostup"
	"info-bot-go/internal/sentlog"
)

// DostupSync — фоновый воркер синхронизации бот ↔ портал.
type DostupSync struct {
	deps     *Deps
	bot      *tb.Bot
	stop     chan struct{}
	stopped  chan struct{}
	followUp *FollowUpModule // для напоминаний о просроченных гилках
}

// NewDostupSync создаёт воркер (deps.Dostup != nil — обязательное условие).
func NewDostupSync(deps *Deps) *DostupSync {
	return &DostupSync{
		deps:    deps,
		bot:     deps.Bot,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// SetFollowUpModule подключает модуль уточнений (напоминания о просрочках).
func (w *DostupSync) SetFollowUpModule(fu *FollowUpModule) {
	w.followUp = fu
}

// Start запускает цикл: первая синхронизация через 15 секунд после старта.
func (w *DostupSync) Start() {
	interval := 20 * time.Minute
	if m := w.deps.Cfg.DostupSyncMinutes; m > 0 {
		interval = time.Duration(m) * time.Minute
	}
	go func() {
		defer close(w.stopped)
		select {
		case <-time.After(15 * time.Second):
		case <-w.stop:
			return
		}
		w.refreshCatalogIfNeeded()
		w.SyncNow(false)
		for {
			jitter := time.Duration(rand.Int63n(int64(interval / 4)))
			select {
			case <-time.After(interval + jitter):
				w.refreshCatalogIfNeeded()
				w.SyncNow(false)
			case <-w.stop:
				return
			}
		}
	}()
	log.Printf("[DOSTUP-SYNC] воркер запущен (интервал %s)", interval)
}

// refreshCatalogIfNeeded обновляет локальный каталог портала (раз в сутки).
func (w *DostupSync) refreshCatalogIfNeeded() {
	if w.deps.DostupCatalog == nil || w.deps.Dostup == nil {
		return
	}
	syncedAt := w.deps.DostupCatalog.SyncedAt()
	if syncedAt != "" {
		if t, err := time.Parse(time.RFC3339, syncedAt); err == nil && time.Since(t) < 24*time.Hour {
			return
		}
	}
	log.Printf("[DOSTUP-SYNC] обновляю каталог портала...")
	cat, err := w.deps.Dostup.SyncCatalog()
	if err != nil {
		log.Printf("[DOSTUP-SYNC] каталог: %v", err)
		return
	}
	if w.deps.DostupCatalog.Replace(cat) {
		log.Printf("[DOSTUP-SYNC] каталог обновлён: %d органов", w.deps.DostupCatalog.Count())
	}
}

// Stop останавливает цикл.
func (w *DostupSync) Stop() {
	close(w.stop)
	select {
	case <-w.stopped:
	case <-time.After(3 * time.Second):
	}
}

// SyncNow выполняет один цикл синхронизации. verbose=true — команда /sync от админа.
func (w *DostupSync) SyncNow(verbose bool) string {
	if w.deps.Dostup == nil {
		return "⚠️ Канал «Доступ до правди» не налаштований."
	}
	var report strings.Builder

	// --- 1. Список запросов портала ---
	reqs, err := w.deps.Dostup.MyRequestsFull()
	if err != nil {
		log.Printf("[DOSTUP-SYNC] MyRequestsFull: %v", err)
		return fmt.Sprintf("❌ Портал недоступний: %v", err)
	}
	newCount := 0
	for _, pr := range reqs {
		msgID := "dostup:" + pr.Slug
		if w.deps.SentLog.FindByMessageID(msgID) != nil {
			continue
		}
		// Запрос, поданный вне бота: приписываем владельцу (AdminID)
		owner := w.deps.Cfg.AdminID
		name := pr.BodyName
		if name == "" {
			name = "dostup.org.ua"
		}
		date := pr.Date
		if date == "" {
			date = time.Now().Format(time.RFC3339)
		}
		_ = w.deps.SentLog.Append(sentlog.SentEntry{
			MessageID:      msgID,
			ChatID:         owner,
			UserID:         owner,
			RecipientName:  name,
			RecipientEmail: "dostup.org.ua",
			Subject:        pr.Title,
			Date:           date,
			Channel:        "dostup",
			URL:            pr.URL,
			DostupBody:     name,
			Delivered:      true,
		})
		newCount++
		log.Printf("[DOSTUP-SYNC] +запрос с портала: %s (%s)", pr.Title, pr.Slug)
	}
	if newCount > 0 {
		report.WriteString(fmt.Sprintf("📥 Події з порталу: %d нов(их) запит(ів) синхронізовано.\n", newCount))
	}

	// --- 1b. Гилки для уточнений: backfill из журнала ---
	// (существующие запросы сразу доступны для /followup)
	if w.deps.FollowUps != nil {
		for _, e := range w.deps.SentLog.ListAll() {
			if e.Channel != "dostup" || e.URL == "" {
				continue
			}
			slug := strings.TrimPrefix(e.MessageID, "dostup:")
			known := false
			for _, th := range w.deps.FollowUps.List(e.UserID, 30) {
				if th.Slug == slug {
					known = true
					break
				}
			}
			if !known {
				organ := e.DostupBody
				if organ == "" {
					organ = e.RecipientName
				}
				w.deps.FollowUps.Upsert(e.UserID, FollowUpThread{
					Slug:    slug,
					Subject: e.Subject,
					Organ:   organ,
					URL:     e.URL,
				})
			}
		}
	}

	// --- 2. Статусы и классификация ответов ---
	repliedCount, ackCount := w.syncStatuses(&report)
	if repliedCount > 0 {
		report.WriteString(fmt.Sprintf("📬 Відповідей по суті: %d.\n", repliedCount))
	}
	if ackCount > 0 {
		report.WriteString(fmt.Sprintf("📄 Проміжних відповідей (авто-підтверджень): %d.\n", ackCount))
	}
	log.Printf("[DOSTUP-SYNC] цикл завершён: на портале %d запит(ів), новых %d, ответов по существу %d, авто-подтверждений %d",
		len(reqs), newCount, repliedCount, ackCount)

	// --- 3. Согласование счётчиков (портал = источник истины) ---
	all := w.deps.SentLog.ListAll()
	total, replies := 0, 0
	for _, e := range all {
		total++
		if e.ReplyReceivedAt != "" || e.Status == "replied" {
			replies++
		}
	}
	if w.deps.Stats != nil {
		w.deps.Stats.SyncCounts(total, replies)
	}

	// --- 4. Напоминания о просроченных гилках (строк минул) ---
	w.remindOverdueThreads()

	// --- 5. Рейтинговый батч: постепенный сбор статистики органов ---
	w.collectRatingBatch()

	if verbose {
		if report.Len() == 0 {
			report.WriteString("✅ Синхронізація: змін немає. ")
		}
		report.WriteString(fmt.Sprintf("Портал: %d запит(ів), всього враховано: %d, відповідей по суті: %d.",
			len(reqs), total, replies))
		return report.String()
	}
	return report.String()
}

// ratingBatchSize — сколько органов рейтинга обновляется за один цикл
// синхронизации (20 минут + джиттер). Полный обход 2145 органов ≈ 18 часов.
const ratingBatchSize = 40

// collectRatingBatch — бережный фоновый сбор статистики органов для рейтинга.
//
// Портал хрупкий (май 2026 — потеря данных после сбоя), поэтому полный обход
// 2145 органов НЕ делается за один раз: за цикл обновляется не более
// ratingBatchSize органов, между запросами — вежливые паузы, при первом
// же rate-limit батч останавливается до следующего цикла. Приоритеты:
// биндинги пользователей → никогда не собранные → самые устаревшие.
func (w *DostupSync) collectRatingBatch() {
	if w.deps.DostupRatings == nil || w.deps.Dostup == nil || w.deps.DostupCatalog == nil {
		return
	}
	cat := w.deps.DostupCatalog.Get()
	if cat == nil || len(cat.Bodies) == 0 {
		return
	}
	prefer := map[string]bool{}
	for _, b := range w.deps.DostupCatalog.AllBindings() {
		if b.Slug != "" {
			prefer[b.Slug] = true
		}
	}
	batch, total := w.deps.DostupRatings.NextBatch(cat.Bodies, prefer, ratingBatchSize)
	if len(batch) == 0 {
		return
	}
	okCount, errCount := 0, 0
	for i, slug := range batch {
		if i > 0 {
			time.Sleep(time.Duration(1500+rand.Intn(500)) * time.Millisecond)
		}
		if _, err := w.deps.Dostup.RefreshBodyStats(slug); err != nil {
			if errors.Is(err, dostup.ErrRateLimited) {
				log.Printf("[RATING] rate-limit на %s — батч остановлен на %d из %d, продолжим в следующем цикле", slug, okCount, len(batch))
				break
			}
			errCount++
			if errCount <= 3 {
				log.Printf("[RATING] %s: %v", slug, err)
			}
			continue // ошибка одного органа не роняет батч
		}
		okCount++
	}
	if err := w.deps.DostupRatings.Save(); err != nil {
		log.Printf("[RATING] сохранение хранилища: %v", err)
	}
	covered := w.deps.DostupRatings.Count()
	log.Printf("[RATING] batch: +%d (ошибок %d), covered %d/%d", okCount, errCount, covered, total)
}

// syncStatuses обходит все dostup-запросы, классифицирует последние
// входящие сообщения и рассылает уведомления. Возвращает количество
// новых ответов по существу и новых авто-подтверждений.
func (w *DostupSync) syncStatuses(report *strings.Builder) (repliedCount, ackCount int) {
	for _, e := range w.deps.SentLog.ListAll() {
		if e.Channel != "dostup" {
			continue
		}
		// Финализированные запросы (ответ по существу зафиксирован
		// и id входящего известен) больше не опрашиваем.
		finalized := e.ReplyReceivedAt != "" && e.LastIncomingID != ""
		if finalized || e.Status == "bounced" || e.Status == "user_withdrawn" {
			continue
		}
		slug := strings.TrimPrefix(e.MessageID, "dostup:")
		st, err := w.deps.Dostup.GetRequestStatus(slug)
		if err != nil {
			log.Printf("[DOSTUP-SYNC] статус %s: %v", slug, err)
			continue
		}

		kind := dostup.ClassifyReply(st.ResponseExcerpt)
		wasFinal := e.ReplyReceivedAt != "" || e.Status == "replied"
		incomingChanged := st.HasResponse && st.LastIncomingID != "" && st.LastIncomingID != e.LastIncomingID

		switch {
		case st.HasResponse && dostup.ResponseArrived(st.Status) &&
			kind == dostup.ReplySubstantive:
			// --- ответ по существу ---
			if !wasFinal {
				_ = w.deps.SentLog.UpdateDostupStatus(e.MessageID, st.Status, st.ResponseExcerpt, st.LastIncomingID, true)
				repliedCount++
				if w.deps.Stats != nil {
					w.deps.Stats.IncrementReplies()
				}
				log.Printf("[DOSTUP-SYNC] %s: %s (incoming-%s)", slug, dostup.KindLabel(kind), st.LastIncomingID)
				w.notifyResponse(e, st)
			} else {
				// запись уже закрыта ранее — просто зафиксировать id
				_ = w.deps.SentLog.UpdateDostupStatus(e.MessageID, st.Status, st.ResponseExcerpt, st.LastIncomingID, true)
			}

		case st.HasResponse && kind == dostup.ReplyAcknowledgement:
			// --- промежуточное подтверждение ---
			if wasFinal {
				// миграция/откат: автоответ раньше засчитали как ответ
				_ = w.deps.SentLog.UnmarkReplied(e.MessageID, st.Status, st.ResponseExcerpt, st.LastIncomingID)
				log.Printf("[DOSTUP-SYNC] %s: откат — автоответ не является ответом по существу (incoming-%s)", slug, st.LastIncomingID)
				w.notifyCorrection(e, st)
			} else {
				_ = w.deps.SentLog.MarkAcknowledged(e.MessageID, st.Status, st.ResponseExcerpt, st.LastIncomingID)
				if incomingChanged {
					ackCount++
					log.Printf("[DOSTUP-SYNC] %s: %s (incoming-%s)", slug, dostup.KindLabel(kind), st.LastIncomingID)
					w.notifyAcknowledgement(e, st)
				}
			}

		default:
			// --- ответа по существу пока нет ---
			_ = w.deps.SentLog.UpdateDostupStatus(e.MessageID, st.Status, st.ResponseExcerpt, st.LastIncomingID, false)
			if !wasFinal && st.Status != e.LastStatus {
				switch st.Status {
				case "waiting_clarification", "rejected", "requires_admin", "error_message", "user_withdrawn":
					w.notifyStatusChange(e, st)
				}
			}
		}
		// пауза между запросами к порталу — вежливость к rate-limit
		time.Sleep(2 * time.Second)
	}
	return repliedCount, ackCount
}

// notifyResponse отправляет пользователю уведомление об ответе ПО СУЩЕСТВУ.
func (w *DostupSync) notifyResponse(e sentlog.SentEntry, st *dostup.RequestStatus) {
	organ := e.DostupBody
	if organ == "" {
		organ = e.RecipientName
	}
	text := fmt.Sprintf("📬 <b>Відповідь по суті на ваш запит!</b>\n\n"+
		"🏛 <b>%s</b>\n"+
		"📂 «%s»\n\n", htmlEscape(organ), htmlEscape(e.Subject))
	if st.ResponseExcerpt != "" {
		excerpt := st.ResponseExcerpt
		if len(excerpt) > 350 {
			excerpt = excerpt[:350] + "…"
		}
		text += fmt.Sprintf("💬 <i>%s</i>\n\n", htmlEscape(excerpt))
	}
	text += fmt.Sprintf("🔗 Повна переписка (без реєстрації):\n%s\n\n"+
		"ℹ️ Статус: %s", htmlLink(e.URL), dostup.StatusLabel(st.Status))

	// Гилка получила ответ по существу — фиксируем (напоминания отключаются)
	if w.deps.FollowUps != nil {
		w.deps.FollowUps.MarkReplied(e.UserID, strings.TrimPrefix(e.MessageID, "dostup:"), time.Now().Format(time.RFC3339))
	}

	w.sendNotify(e.UserID, text)
}

// notifyAcknowledgement сообщает о промежуточном авто-подтверждении:
// «ваш лист отримано» — хорошо, но ответом по существу не считается.
func (w *DostupSync) notifyAcknowledgement(e sentlog.SentEntry, st *dostup.RequestStatus) {
	organ := e.DostupBody
	if organ == "" {
		organ = e.RecipientName
	}
	text := fmt.Sprintf("📄 <b>Проміжна відповідь (авто-підтвердження)</b>\n\n"+
		"🏛 <b>%s</b>\n"+
		"📂 «%s»\n\n", htmlEscape(organ), htmlEscape(e.Subject))
	if st.ResponseExcerpt != "" {
		excerpt := st.ResponseExcerpt
		if len(excerpt) > 350 {
			excerpt = excerpt[:350] + "…"
		}
		text += fmt.Sprintf("💬 <i>%s</i>\n\n", htmlEscape(excerpt))
	}
	text += "ℹ️ Це підтвердження отримання запиту, а не відповідь по суті — " +
		"тому запит і надалі рахується як «очікує відповідь», лічильник відповідей не змінено.\n\n"
	text += fmt.Sprintf("🔗 Повна переписка (без реєстрації):\n%s\n\n"+
		"⏰ Відповідь по суті очікується до: <b>%s</b>",
		htmlLink(e.URL), formatReplyDeadline(e.Date))

	w.sendNotify(e.UserID, text)
}

// notifyCorrection объясняет пользователю откат статуса: сообщение органа
// оказалось автоответом, а не ответом по существу.
func (w *DostupSync) notifyCorrection(e sentlog.SentEntry, st *dostup.RequestStatus) {
	organ := e.DostupBody
	if organ == "" {
		organ = e.RecipientName
	}
	text := fmt.Sprintf("🔧 <b>Статус запиту скориговано</b>\n\n"+
		"🏛 <b>%s</b>\n"+
		"📂 «%s»\n\n", htmlEscape(organ), htmlEscape(e.Subject))
	text += "Орган надіслав лише авто-підтвердження («лист отримано») — це не відповідь по суті. " +
		"Запит повернуто в очікування відповіді, лічильник відповідей скасовано.\n\n"
	text += fmt.Sprintf("🔗 Повна переписка (без реєстрації):\n%s\n\n"+
		"⏰ Відповідь по суті очікується до: <b>%s</b>",
		htmlLink(e.URL), formatReplyDeadline(e.Date))

	w.sendNotify(e.UserID, text)
}

// notifyStatusChange уведомляет о важном изменении статуса без ответа.
func (w *DostupSync) notifyStatusChange(e sentlog.SentEntry, st *dostup.RequestStatus) {
	organ := e.DostupBody
	if organ == "" {
		organ = e.RecipientName
	}
	text := fmt.Sprintf("🔔 <b>Зміна статусу запиту</b>\n\n"+
		"🏛 <b>%s</b>\n"+
		"📂 «%s»\n\n"+
		"ℹ️ %s\n\n"+
		"🔗 Повна переписка (без реєстрації):\n%s",
		htmlEscape(organ), htmlEscape(e.Subject), dostup.StatusLabel(st.Status), htmlLink(e.URL))

	w.sendNotify(e.UserID, text)
}

// sendNotify доставляет сообщение в чат пользователя (HTML-режим).
func (w *DostupSync) sendNotify(userID int64, text string) {
	target := userID
	if target == 0 {
		target = w.deps.Cfg.AdminID
	}
	if target == 0 {
		return
	}
	_, err := w.bot.Send(tb.ChatID(target), text, tb.ModeHTML, tb.NoPreview)
	if err != nil {
		log.Printf("[DOSTUP-SYNC] уведомление user=%d: %v", target, err)
	}
}

// remindOverdueThreads — для гилок без ответа по существу, у которых
// строк (5 рабочих дней) минул: одно напоминание «допишите в гилку»
// (не чаще раза в сутки на гилку).
func (w *DostupSync) remindOverdueThreads() {
	if w.followUp == nil || w.deps.FollowUps == nil {
		return
	}
	for _, e := range w.deps.SentLog.ListAll() {
		if e.Channel != "dostup" || e.URL == "" {
			continue
		}
		slug := strings.TrimPrefix(e.MessageID, "dostup:")
		threads := w.deps.FollowUps.List(e.UserID, 30)
		for _, th := range threads {
			if th.Slug != slug {
				continue
			}
			// Ответ уже пришёл или недавно напоминали — пропускаем
			if th.RepliedAt != "" {
				continue
			}
			if th.LastRemindAt != "" {
				if t, err := time.Parse(time.RFC3339, th.LastRemindAt); err == nil && time.Since(t) < 24*time.Hour {
					continue
				}
			}
			deadline := replyDeadline(e.Date)
			if deadline.IsZero() || time.Now().Before(deadline) {
				continue
			}
			w.followUp.OfferOverdueReminder(e.UserID, th, deadline.Format("02.01.2006"))
			log.Printf("[DOSTUP-SYNC] напоминание о просрочке: %s (user %d)", slug, e.UserID)
		}
	}
}

// replyDeadline — дедлайн ответа (5 рабочих дней с даты отправки).
func replyDeadline(dateISO string) time.Time {
	t, err := time.Parse(time.RFC3339, dateISO)
	if err != nil {
		return time.Time{}
	}
	return addWorkingDays(t, 5)
}
