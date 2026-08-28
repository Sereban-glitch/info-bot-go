package handlers

import (
	"fmt"
	"strings"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/dostup"
)

// MyRequestsModule handles /my.
type MyRequestsModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewMyRequestsModule(deps *Deps) *MyRequestsModule {
	return &MyRequestsModule{deps: deps, bot: deps.Bot}
}

func (m *MyRequestsModule) Name() string { return "my-requests" }

func (m *MyRequestsModule) Register() {
	handler := safeHandler("my-requests", func(c tb.Context) error {
		requests := m.deps.SentLog.ListByUser(c.Sender().ID)
		if len(requests) == 0 {
			return c.Send("У вас ще немає надісланих запитів через цього бота.\n\nСтворіть перший запит: /new")
		}

		// Последний (самый свежий) запрос канала dostup проверим живьём
		liveStatus := map[string]*dostup.RequestStatus{}
		if m.deps.Dostup != nil {
			checked := 0
			for _, r := range requests {
				if r.Channel != "dostup" || checked >= 3 {
					continue
				}
				slug := strings.TrimPrefix(r.MessageID, "dostup:")
				if st, err := m.deps.Dostup.GetRequestStatus(slug); err == nil {
					liveStatus[r.MessageID] = st
					_ = m.deps.SentLog.UpdateDostupStatus(r.MessageID, st.Status, st.ResponseExcerpt, st.LastIncomingID,
						st.HasResponse && dostup.ResponseArrived(st.Status) &&
							dostup.ClassifyReply(st.ResponseExcerpt) == dostup.ReplySubstantive &&
							r.ReplyReceivedAt == "")
					checked++
					time.Sleep(1 * time.Second)
				}
			}
		}

		text := fmt.Sprintf("📨 <b>Ваші запити (%d):</b>\n\n", len(requests))
		for i, r := range requests {
			sentDate, _ := time.Parse(time.RFC3339, r.Date)
			deadline := addWorkingDays(sentDate, 5).Format("02.01.2006")
			formatted := sentDate.Format("02.01.2006")

			// Статус: живой из портала > сохранённый > базовый
			status := "⏳"
			statusText := ""
			onlyAck := false
			if st, ok := liveStatus[r.MessageID]; ok {
				statusText = dostup.StatusLabel(st.Status)
				status = "⏳"
				if st.HasResponse && dostup.ClassifyReply(st.ResponseExcerpt) == dostup.ReplyAcknowledgement {
					status = "📄" // проміжна відповідь — відповіді по суті ще немає
					onlyAck = true
				} else if st.Status == "successful" || st.Status == "partially_successful" {
					status = "✅"
				} else if dostup.ResponseArrived(st.Status) {
					status = "📬"
				}
			} else if r.Channel == "dostup" && r.LastStatus != "" {
				statusText = dostup.StatusLabel(r.LastStatus)
				status = "⏳"
				if r.AckAt != "" && r.ReplyReceivedAt == "" {
					status = "📄"
					onlyAck = true
				} else if dostup.ResponseArrived(r.LastStatus) {
					status = "📬"
				}
			} else if r.ReplyReceivedAt != "" || r.Status == "replied" {
				status = "✅"
			} else if r.Status == "bounced" {
				status = "❌"
			} else if r.Delivered {
				status = "📨"
			}

			chanIcon := "📧"
			if r.Channel == "dostup" {
				chanIcon = "🌐"
			}

			text += fmt.Sprintf("%d. %s %s 🗓 <b>%s</b>\n🏛 <b>Кому:</b> %s\n📂 <b>Тема:</b> %s\n",
				i+1, status, formatted, chanIcon, htmlEscape(r.RecipientName), htmlEscape(r.Subject))
			if statusText != "" {
				text += fmt.Sprintf("ℹ️ %s\n", statusText)
			}
			if onlyAck {
				text += "📄 Орган прислав лише авто-підтвердження — чекаємо відповідь по суті.\n"
			}
			if r.Channel == "dostup" && r.ReplyReceivedAt == "" {
				text += fmt.Sprintf("⏰ <b>Дедлайн:</b> %s\n", deadline)
			}
			if r.URL != "" {
				text += fmt.Sprintf("🔗 Відстежувати на порталі: %s\n", htmlLink(r.URL))
			}
			if r.ResponseExcerpt != "" && (r.ReplyReceivedAt != "" || r.Status == "replied" || (r.Channel == "dostup" && r.AckAt != "")) {
				excerpt := r.ResponseExcerpt
				if len(excerpt) > 200 {
					excerpt = excerpt[:200] + "…"
				}
				text += fmt.Sprintf("💬 <i>%s</i>\n", htmlEscape(excerpt))
			}
			text += "\n"
		}

		if len(text) > 4000 {
			text = text[:4000] + "..."
		}
		return c.Send(text, tb.ModeHTML)
	})

	m.bot.Handle("/my", handler)
	m.bot.Handle("📨 Мої запити", handler)
}
