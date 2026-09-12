package handlers

// ClassifyModule — классификация ответов на портале прямо из Telegram
// (аналог кнопок «Оновити статус» на dostup.org.ua).
//
// Сценарий: пользователь получил уведомление об ответе органа (по
// существу) — под текстом бот прикрепляет ряд кнопок:
//
//	✅ Отримав усю інформацію   (successful)
//	🟡 Частково                 (partially_successful)
//	❌ Відмовлено               (rejected)
//	📭 Немає інформації         (not_held)
//	⏳ Ще чекаю                 (waiting_response)
//
// Нажатие кнопки отправляет на портал POST /request/<slug>/classifications
// с выбранным статусом — в точности как клик мышкой на сайте. Это
// поддерживает аккаунт в чистоте и НЕ даёт Alaveteli блокировать подачу
// новых запросов («First, did your other requests succeed?»).
//
// Только сам пользователь решает, чем классифицировать ответ своего
// запроса — бот никогда не делает это сам (см. autoclassify в Шаге 3).

import (
	"errors"
	"fmt"
	"log"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/dostup"
)

// ClassifyModule — модуль классификации ответов.
type ClassifyModule struct {
	deps *Deps
	bot  *tb.Bot
}

// NewClassifyModule создаёт модуль классификации.
func NewClassifyModule(deps *Deps) *ClassifyModule {
	return &ClassifyModule{deps: deps, bot: deps.Bot}
}

func (m *ClassifyModule) Name() string       { return "classify" }
func (m *ClassifyModule) StepPrefix() string { return "classify:" }

func (m *ClassifyModule) Register() {
	// Каждая кнопка уникальна по Unique, data = slug запроса.
	btns := []struct {
		unique, text, state string
	}{
		{"cls_ok", "✅ Отримав усю інформацію", "successful"},
		{"cls_part", "🟡 Частково", "partially_successful"},
		{"cls_no", "❌ Відмовлено", "rejected"},
		{"cls_noinf", "📭 Немає інформації", "not_held"},
		{"cls_wait", "⏳ Ще чекаю", "waiting_response"},
	}
	for _, b := range btns {
		btn := tb.InlineButton{Unique: b.unique}
		state := b.state
		m.bot.Handle(&btn, safeHandler("cls_"+b.unique, func(c tb.Context) error {
			return m.handleClassify(c, state)
		}))
	}
}

// handleClassify — нажатие кнопки: ставим статус на портале.
func (m *ClassifyModule) handleClassify(c tb.Context, state string) error {
	_ = c.Respond()
	slug := c.Callback().Data
	if slug == "" || len(slug) > 60 {
		return c.Respond(&tb.CallbackResponse{Text: "❌ Запит не знайдено.", ShowAlert: false})
	}
	if m.deps.Dostup == nil {
		return c.Send("⚠️ Канал «Доступ до правди» не налаштований.")
	}
	if err := m.deps.Dostup.ReportStatus(slug, state); err != nil {
		if errors.Is(err, dostup.ErrNeedsClassification) {
			return c.Send("⚠️ Портал просить спочатку класифікувати ще один відповідь на сайті — після цього бот знову зможе працювати.")
		}
		if errors.Is(err, dostup.ErrRateLimited) {
			return c.Send("⏳ Портал обмежив частоту запитів. Спробуйте за 3–5 хвилин.")
		}
		log.Printf("[CLASSIFY] slug=%s state=%s: %v", slug, state, err)
		return c.Send("❌ Не вдалося оновити статус. Спробуйте ще раз за хвилину або зайдіть на сайт: " + dostup.BaseURL + "/request/" + slug)
	}
	label := dostup.StatusLabel(state)
	return c.Send(fmt.Sprintf("✅ Статус запросу оновлено: %s\n\n🔗 %s/request/%s", label, dostup.BaseURL, slug))
}

// ClassificationButtons возвращает ряд кнопок классификации для
// уведомления об ответе (используется в dostupsync.notifyResponse).
// slug — слаґ запроса (data каждой кнопки).
func ClassificationButtons(slug string) [][]tb.InlineButton {
	if slug == "" || len(slug) > 60 {
		return nil // слишком длинный слаг — Telegram не пропустит callback data
	}
	return [][]tb.InlineButton{
		{
			{Unique: "cls_ok", Text: "✅ Уся інформація", Data: slug},
			{Unique: "cls_part", Text: "🟡 Частково", Data: slug},
		},
		{
			{Unique: "cls_no", Text: "❌ Відмовлено", Data: slug},
			{Unique: "cls_noinf", Text: "📭 Немає інформації", Data: slug},
			{Unique: "cls_wait", Text: "⏳ Ще чекаю", Data: slug},
		},
	}
}