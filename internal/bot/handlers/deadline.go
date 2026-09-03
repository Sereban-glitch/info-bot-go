package handlers

import (
        "fmt"
        "log"
        "strings"
        "sync"
        "time"

        tb "gopkg.in/telebot.v3"

        "info-bot-go/internal/safego"
        "info-bot-go/internal/sentlog"
)

// DeadlineModule tracks the 5-working-day deadline for government replies
// and sends reminder notifications to users.
//
// Просьба владельца (04.09.2026): напоминания о сроках не должны спамить.
// Раньше часовой проверки слала «последний день» и «просрочено» КАЖДЫЙ ЧАС
// (до 24 одинаковых сообщений в сутки на запит). Теперь: одна сгруппированная
// сводка на пользователя в сутки, днём (09:00–21:59 по Киеву), без ночи.
type DeadlineModule struct {
        deps *Deps
        bot  *tb.Bot

        // Дневной дайджест: время последней отправки по чату (in-memory;
        // «просрочено» дополнительно защищено персистентным статусом expired).
        mu         sync.Mutex
        lastDigest map[int64]time.Time
}

func NewDeadlineModule(deps *Deps) *DeadlineModule {
        return &DeadlineModule{deps: deps, bot: deps.Bot, lastDigest: make(map[int64]time.Time)}
}

func (m *DeadlineModule) Name() string { return "deadline" }

func (m *DeadlineModule) Register() {
        // /deadline command — show deadline status for your requests
        m.bot.Handle("/deadline", safeHandler("deadline", m.handleDeadline))
        m.bot.Handle("⏰ Терміни", safeHandler("deadline_btn", m.handleDeadline))

        // Background checker — runs every hour
        go m.runChecker()
}

func (m *DeadlineModule) handleDeadline(c tb.Context) error {
        entries := m.deps.SentLog.ListByUser(c.Sender().ID)
        if len(entries) == 0 {
                return c.Send("📭 У вас ще немає відправлених запитів.\n\nПочніть: /new")
        }

        text := "⏰ *Статус ваших запитів:*\n\n"
        hasActive := false

        for _, e := range entries {
                if e.Status == "replied" || e.ReplyReceivedAt != "" {
                        text += fmt.Sprintf("✅ %s — відповідь отримано\n", e.RecipientName)
                        continue
                }
                if e.Status == "bounced" {
                        text += fmt.Sprintf("⚠️ %s — не доставлено\n", e.RecipientName)
                        continue
                }

                hasActive = true
                deadline := calcWorkingDaysDeadline(e.Date)
                remaining := time.Until(deadline)
                daysLeft := int(remaining.Hours() / 24)

                if remaining <= 0 {
                        text += fmt.Sprintf("🔴 %s — термін порушено! (%s)\n", e.RecipientName, formatDate(deadline))
                } else if daysLeft <= 1 {
                        text += fmt.Sprintf("🟠 %s — останній день! (%s)\n", e.RecipientName, formatDate(deadline))
                } else if daysLeft <= 2 {
                        text += fmt.Sprintf("🟡 %s — залишилось %d дні (%s)\n", e.RecipientName, daysLeft, formatDate(deadline))
                } else {
                        text += fmt.Sprintf("🟢 %s — залишилось %d днів (%s)\n", e.RecipientName, daysLeft, formatDate(deadline))
                }
        }

        if !hasActive {
                text += "\n✅ Всі запити отримали відповідь!"
        }

        return c.Send(text, tb.ModeMarkdown)
}

// runChecker periodically checks for approaching/expired deadlines
// and notifies users proactively. Каждая проверка защищена safego:
// паника одной проверки не останавливает часовой тикер (ТЗ №4, D3).
func (m *DeadlineModule) runChecker() {
        ticker := time.NewTicker(1 * time.Hour)
        defer ticker.Stop()

        // Initial delay
        time.Sleep(30 * time.Second)

        for range ticker.C {
                safego.Run("deadline-check", m.checkDeadlines)
        }
}

// deadlineItem — один пункт дневной сводки сроков.
type deadlineItem struct {
        msgID   string
        line    string
        expired bool // просрочено → после отправки пометить, чтобы не повторять
}

// checkDeadlines — собирает сработавшие события (просрочка / последний
// день) ПО ПОЛЬЗОВАТЕЛЯМ и отправляет каждому одну сводку в сутки.
// Часовой тикер лишь накапливает: между отправками молчим.
func (m *DeadlineModule) checkDeadlines() {
        pending := collectDeadlineItems(m.deps.SentLog.ListAll(), time.Now())

        now := time.Now()
        loc := kyivLoc()
        for chatID, items := range pending {
                m.mu.Lock()
                last := m.lastDigest[chatID]
                m.mu.Unlock()

                if !deadlineDigestDue(last, now, loc) {
                        continue // уже отправляли сегодня или ночь — молчим
                }

                if err := m.sendDigest(chatID, items); err != nil {
                        log.Printf("[DEADLINE] failed to send digest to %d: %v (повтор через час)", chatID, err)
                        continue
                }

                m.mu.Lock()
                m.lastDigest[chatID] = now
                m.mu.Unlock()
                log.Printf("[DEADLINE] дайджест сроков: user=%d, пунктов=%d", chatID, len(items))

                // Помечаем ТОЛЬКО реально отправленное: сбой Telegram → повтор в следующий час.
                for _, it := range items {
                        if it.expired {
                                _ = m.deps.SentLog.MarkExpired(it.msgID)
                        }
                }
        }
}

// collectDeadlineItems — чистая группировка сработавших сроков по чатам.
// Пропускает отвеченные/недоставленные/уже отправленную просрочку.
func collectDeadlineItems(entries []sentlog.SentEntry, now time.Time) map[int64][]deadlineItem {
        pending := make(map[int64][]deadlineItem)
        for _, e := range entries {
                // Skip if already replied, bounced, или просрочку уже отправляли
                if e.Status == "replied" || e.Status == "bounced" || e.Status == "expired" || e.ReplyReceivedAt != "" {
                        continue
                }

                deadline := calcWorkingDaysDeadline(e.Date)
                remaining := deadline.Sub(now) // >0 — дедлайн впереди (семантика time.Until)
                daysLeft := int(remaining.Hours() / 24)

                chatID := e.ChatID
                if chatID == 0 {
                        chatID = e.UserID
                }
                if chatID == 0 {
                        continue
                }

                switch {
                case remaining <= 0:
                        pending[chatID] = append(pending[chatID], deadlineItem{
                                msgID:   e.MessageID,
                                line:    fmt.Sprintf("🔴 %s — термін порушено (%s)", e.RecipientName, formatDate(deadline)),
                                expired: true,
                        })
                case daysLeft == 1:
                        pending[chatID] = append(pending[chatID], deadlineItem{
                                msgID: e.MessageID,
                                line:  fmt.Sprintf("🟠 %s — останній робочий день (%s)", e.RecipientName, formatDate(deadline)),
                        })
                }
        }
        return pending
}

// deadlineDigestDue — можно ли отправлять дневную сводку: днём
// (09:00–21:59 по Киеву) и не чаще одного раза в календарные сутки.
func deadlineDigestDue(lastSent time.Time, now time.Time, loc *time.Location) bool {
        kyiv := now.In(loc)
        h := kyiv.Hour()
        if h < 9 || h >= 22 {
                return false // ночь — не будим людей
        }
        if !lastSent.IsZero() {
                dayStart := time.Date(kyiv.Year(), kyiv.Month(), kyiv.Day(), 0, 0, 0, 0, loc)
                if !lastSent.In(loc).Before(dayStart) {
                        return false // уже отправляли сегодня
                }
        }
        return true
}

// sendDigest — одна групповая сводка о сроках вместо кучи сообщений.
func (m *DeadlineModule) sendDigest(chatID int64, items []deadlineItem) error {
        var b strings.Builder
        b.WriteString("⏰ *Строки відповідей — щоденна звірка*\n\n")
        expiredN := 0
        for _, it := range items {
                b.WriteString(it.line)
                b.WriteString("\n")
                if it.expired {
                        expiredN++
                }
        }
        if expiredN > 0 {
                b.WriteString("\nЗа ст. 22 ЗУ «Про доступ до публічної інформації» ви маєте право подати скаргу: керівнику органу, Уповноваженому з прав людини чи до суду. Строк розгляду скарги — 5 робочих днів.\n")
        }
        b.WriteString("\nДеталі: ⏰ Терміни або /my")
        _, err := m.bot.Send(tb.ChatID(chatID), b.String(), tb.ModeMarkdown)
        return err
}

// calcWorkingDaysDeadline adds 5 Ukrainian working days to the send date.
// Simple implementation: add 7 calendar days (covers at most 1 weekend).
// For full accuracy, use ukrainian holidays calendar.
func calcWorkingDaysDeadline(dateStr string) time.Time {
        t, err := time.Parse(time.RFC3339, dateStr)
        if err != nil {
                // Try alternative format
                t, err = time.Parse("2006-01-02", dateStr)
                if err != nil {
                        // Fallback: 7 days from now
                        return time.Now().Add(7 * 24 * time.Hour)
                }
        }

        // Add 5 working days (skip weekends)
        daysAdded := 0
        current := t
        for daysAdded < 5 {
                current = current.AddDate(0, 0, 1)
                weekday := current.Weekday()
                if weekday != time.Saturday && weekday != time.Sunday {
                        daysAdded++
                }
        }

        return current
}

func formatDate(t time.Time) string {
        return t.Format("02.01.2006")
}

// Ensure sentlog types are used
var _ = sentlog.SentEntry{}
