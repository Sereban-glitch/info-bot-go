package alert

// Алерты владельцу в реальном времени: пользователь столкнулся с ошибкой.
//
// Раньше ошибки попадали только в bot.log — Арчи-монитор читал их 4 раза
// в сутки, владелец узнавал о сбое спустя часы. Теперь при ошибке у
// РЕАЛЬНОГО пользователя (не владельца) бот сразу пишет владельцу в чат:
// кто, где и что случилось — можно чинить, пока пользователь ещё в чате.
//
// Защита от флуда:
//   - одинаковая ошибка шлётся не чаще раза в 30 минут (дедуп по месту+тексту);
//   - не больше 10 алертов в час суммарно;
//   - действия владельца (его собственные тесты) не алертятся.
//
// Сообщение в стиле Арчи-монитора, по-русски — адресат один и тот же.

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/safego"
)

const (
	dedupWindow = 30 * time.Minute // одинаковая ошибка — не чаще
	hourlyCap   = 10               // максимум алертов в час
	maxErrText  = 200              // обрезка текста ошибки в алерте
)

var (
	mu      sync.Mutex
	bot     *tb.Bot
	adminID int64
	seen    = map[string]time.Time{} // ключ дедупа → время последнего алерта
	sentAt  []time.Time              // алерт-бюджет за последний час
)

// SetBot подключает бота для алертов (один раз при старте; nil — тихий режим).
func SetBot(b *tb.Bot, admin int64) {
	mu.Lock()
	defer mu.Unlock()
	bot, adminID = b, admin
}

// UserError сообщает, что пользователь userID столкнулся с ошибкой:
// who — «Іван @ivan», where — «handler analyze» или «AI розбір»,
// errText — текст ошибки. Отправка асинхронная: обработчик не ждёт.
func UserError(userID int64, who, where, errText string) {
	if userID == 0 || bot == nil {
		return
	}
	mu.Lock()
	admin := adminID
	mu.Unlock()
	if userID == admin { // владелец тестирует сам — алертов не нужно
		return
	}
	key := DedupKey(where, errText)
	if !takeSlot(key) {
		return
	}
	snapshotBot := bot
	text := BuildUserAlert(userID, who, where, errText)
	safego.Go("owner-alert", func() {
		if _, err := snapshotBot.Send(tb.ChatID(admin), text); err != nil {
			log.Printf("[ALERT] не удалось уведомить владельца (user=%d, %s): %v", userID, where, err)
		} else {
			log.Printf("[ALERT] владелец уведомлён об ошибке пользователя %d (%s)", userID, where)
		}
	})
}

// DedupKey — ключ группировки повторов: место + первые знаки текста.
// Без userID: если трое пользователей словили одну ошибку — узнаем
// одним алертом, а не тремя.
func DedupKey(where, errText string) string {
	e := strings.TrimSpace(errText)
	if len(e) > 80 {
		e = e[:80]
	}
	return strings.TrimSpace(where) + "|" + e
}

// takeSlot — отправка с bookkeeping: дедуп 30 минут + часовой лимит.
func takeSlot(key string) bool {
	mu.Lock()
	defer mu.Unlock()
	now := time.Now()
	if t, ok := seen[key]; ok && now.Sub(t) < dedupWindow {
		return false
	}
	// чистим хвосты часового бюджета
	cutoff := now.Add(-time.Hour)
	keep := sentAt[:0]
	for _, t := range sentAt {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	sentAt = keep
	if len(sentAt) >= hourlyCap {
		return false
	}
	seen[key] = now
	sentAt = append(sentAt, now)
	return true
}

// AllowSend — чистое решение по дедупу и часовому лимиту (для тестов):
// ключ уже был в окне дедупа? лимит исчерпан?
func AllowSend(key string, now time.Time, seen map[string]time.Time, window time.Duration, sent []time.Time, cap int) bool {
	if t, ok := seen[key]; ok && now.Sub(t) < window {
		return false
	}
	n := 0
	for _, t := range sent {
		if now.Sub(t) < time.Hour {
			n++
		}
	}
	return n < cap
}

// BuildUserAlert — текст алерта владельцу (чистая функция, покрыта тестами).
func BuildUserAlert(userID int64, who, where, errText string) string {
	if who == "" {
		who = "без імені"
	}
	e := strings.TrimSpace(errText)
	if e == "" {
		e = "(без тексту помилки)"
	}
	r := []rune(e)
	if len(r) > maxErrText {
		e = string(r[:maxErrText]) + "…"
	}
	return fmt.Sprintf("🚨 Пользователь столкнулся с ошибкой\n\n"+
		"👤 %s (id %d)\n"+
		"⚙️ %s\n"+
		"❌ %s\n\n"+
		"Подробности: /home/archi/info-bot/logs/bot.log — строка с этим временем. "+
		"Пользователь получил сообщение об ошибке; можно ответить ему через /support.",
		who, userID, where, e)
}
