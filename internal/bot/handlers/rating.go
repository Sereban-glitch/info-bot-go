package handlers

import (
	"fmt"
	"strings"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/dostup"
)

// RatingModule — «🏆 Рейтинг органів»: публичный лидерборд открытости
// по данным портала «Доступ до правди».
//
// «Індекс відкритості» = round(100 × відповідей_по_суті / запитів);
// в рейтинг попадают только органы с ≥ 5 запитами. Среднее время ответа —
// наши собственные данные (портал таймингов не отдаёт).
//
// Команда /rating — топ-10 найвідкритіших + анти-топ-5; кнопки-страницы
// (callback rtg:b/w:<page> — короткие, ≤ 64 байт). Из каталога доступна
// кнопка «🏆 Рейтинг».

const (
	ratingTopPerPage   = 10 // найвідкритіші на страницу
	ratingWorstShown   = 5  // антирейтинг в общем выводе
	ratingWorstPerPage = 10
)

// RatingModule handles /rating and rating callbacks.
type RatingModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewRatingModule(deps *Deps) *RatingModule {
	return &RatingModule{deps: deps, bot: deps.Bot}
}

func (m *RatingModule) Name() string { return "rating" }

func (m *RatingModule) Register() {
	m.bot.Handle("/rating", safeHandler("rating", m.showRating))
	m.bot.Handle(&tb.Btn{Unique: "rtg_page"}, safeHandler("rating_page", m.handlePage))
}

// ratingView — общий вывод рейтинга (топ + антирейтинг + подпись честности).
func (m *RatingModule) showRating(c tb.Context) error {
	return m.sendRatingView(c, false)
}

// handlePage — пагинация: data = "o|0" | "b|<page>" | "w|<page>".
func (m *RatingModule) handlePage(c tb.Context) error {
	_ = c.Respond()
	parts := strings.SplitN(c.Callback().Data, "|", 2)
	if len(parts) != 2 {
		_ = c.Edit("❌ Сторінку закрито — надішліть /rating ще раз.")
		return nil
	}
	mode := parts[0]
	page := 0
	fmt.Sscanf(parts[1], "%d", &page)
	if page < 0 {
		page = 0
	}
	if mode == "o" {
		return m.sendRatingView(c, true)
	}
	if mode != "b" && mode != "w" {
		mode = "b"
	}
	return m.sendRatingPage(c, mode, page)
}

// leaderboardParams — каталог + хранилище рейтингов.
func (m *RatingModule) ratingSources() ([]dostup.CatalogBody, *dostup.RatingsStore, bool) {
	if m.deps.DostupRatings == nil || m.deps.DostupCatalog == nil {
		return nil, nil, false
	}
	cat := m.deps.DostupCatalog.Get()
	if cat == nil || len(cat.Bodies) == 0 {
		return nil, nil, false
	}
	return cat.Bodies, m.deps.DostupRatings, true
}

// sendRatingView — стартовый экран: топ-10 + анти-топ-5 + футер.
func (m *RatingModule) sendRatingView(c tb.Context, edit bool) error {
	bodies, store, ok := m.ratingSources()
	if !ok {
		text := "🏆 <b>Рейтинг органів</b>\n\n⚠️ Канал «Доступ до правди» не налаштований — рейтинг недоступний."
		if edit {
			return c.Edit(text, tb.ModeHTML)
		}
		return c.Send(text, tb.ModeHTML)
	}

	best, total := store.Leaderboard(bodies, dostup.LeaderOptions{Sort: "best", Limit: ratingTopPerPage})
	covered, catalogTotal := store.Count(), len(bodies)

	var b strings.Builder
	b.WriteString("🏆 <b>Рейтинг органів</b>\n\n")

	if total == 0 {
		// Empty state: сбор ещё идёт — честный прогресс вместо пустоты
		b.WriteString("⏳ Ще збираємо дані: обійдено ")
		b.WriteString(fmt.Sprintf("%d із %d", covered, catalogTotal))
		b.WriteString(" органів.\n\nРейтинг з'явиться, як тільки набереться достатньо органів із 5+ запитами. Дані збираємо акуратно, малими порціями — щоб не навантажувати портал.")
		text := b.String()
		if edit {
			return c.Edit(text, tb.ModeHTML)
		}
		return c.Send(text, tb.ModeHTML)
	}

	timings := m.bodyTimings()
	b.WriteString("🟢 <b>Найвідкритіші:</b>\n")
	for _, r := range best {
		b.WriteString(fmt.Sprintf("%d. %s — індекс %d %s\n", r.Rank, htmlEscape(truncateRunes(r.Name, 42)), r.Index, dostup.RatingBadge(r.Index)))
		if t, ok := timings[strings.ToLower(r.Name)]; ok {
			b.WriteString(fmt.Sprintf("    <i>⏱ сер. %.1f год за нашими даними (n=%d)</i>\n", t.Hours, t.Count))
		}
	}
	b.WriteString("\n")

	worst, _ := store.Leaderboard(bodies, dostup.LeaderOptions{Sort: "worst", Limit: ratingWorstShown})
	if len(worst) > 0 {
		b.WriteString("🔴 <b>Антирейтинг</b> <i>(ігнорують запити)</i>:\n")
		for _, r := range worst {
			b.WriteString(fmt.Sprintf("%d. %s — індекс %d 🔴 (прострочено %d%%)\n", r.Rank, htmlEscape(truncateRunes(r.Name, 42)), r.Index, r.OverduePct))
		}
		b.WriteString("\n")
	}

	// Подпись честности: источник + свежесть
	b.WriteString(fmt.Sprintf("📊 Дані: %d із %d органів порталу · рейтинг: %d із 5+ запитами\nДжерело: «Доступ до правди»", covered, catalogTotal, total))
	if latest := store.LatestFetch(); !latest.IsZero() {
		b.WriteString(" · станом на " + latest.Format("02.01.2006"))
	}

	kb := ratingOverviewKeyboard()
	if edit {
		return c.Edit(b.String(), kb, tb.ModeHTML)
	}
	return c.Send(b.String(), kb, tb.ModeHTML)
}

// sendRatingPage — страница лидерборда (топ или антирейтинг).
func (m *RatingModule) sendRatingPage(c tb.Context, mode string, page int) error {
	bodies, store, ok := m.ratingSources()
	if !ok {
		_ = c.Edit("❌ Рейтинг недоступний.")
		return nil
	}
	perPage := ratingTopPerPage
	sortMode := "best"
	title := "🟢 <b>Найвідкритіші органи</b>"
	if mode == "w" {
		perPage = ratingWorstPerPage
		sortMode = "worst"
		title = "🔴 <b>Антирейтинг</b> — ігнорують запити"
	}
	offset := page * perPage
	rows, total := store.Leaderboard(bodies, dostup.LeaderOptions{Sort: sortMode, Offset: offset, Limit: perPage})
	if len(rows) == 0 {
		_ = c.Edit("❌ Сторінка порожня — надішліть /rating.")
		return nil
	}
	timings := m.bodyTimings()

	var b strings.Builder
	b.WriteString(title + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("%d. %s — індекс %d %s\n", r.Rank, htmlEscape(truncateRunes(r.Name, 42)), r.Index, dostup.RatingBadge(r.Index)))
		b.WriteString(fmt.Sprintf("    <i>по суті %d із %d · прострочено %d%%</i>\n", r.Successful, r.Requests, r.OverduePct))
		if t, ok := timings[strings.ToLower(r.Name)]; ok {
			b.WriteString(fmt.Sprintf("    <i>⏱ сер. %.1f год за нашими даними (n=%d)</i>\n", t.Hours, t.Count))
		}
	}

	covered, catalogTotal := store.Count(), len(bodies)
	b.WriteString(fmt.Sprintf("\n📊 Дані: %d із %d органів · рейтинг: %d", covered, catalogTotal, total))
	if latest := store.LatestFetch(); !latest.IsZero() {
		b.WriteString(" · станом на " + latest.Format("02.01.2006"))
	}

	_ = c.Edit(b.String(), ratingPageKeyboard(mode, page, total), tb.ModeHTML)
	return nil
}

// ratingOverviewKeyboard — клавиатура общего вида рейтинга.
func ratingOverviewKeyboard() *tb.ReplyMarkup {
	kb := &tb.ReplyMarkup{}
	kb.InlineKeyboard = [][]tb.InlineButton{
		{
			{Unique: "rtg_page", Text: "🏆 Ще відкритіші", Data: "b|1"},
			{Unique: "rtg_page", Text: "🔻 Ще гірші", Data: "w|1"},
		},
	}
	return kb
}

// ratingPageKeyboard — листалка страницы режима.
func ratingPageKeyboard(mode string, page, total int) *tb.ReplyMarkup {
	kb := &tb.ReplyMarkup{}
	perPage := ratingTopPerPage
	if mode == "w" {
		perPage = ratingWorstPerPage
	}
	var row []tb.InlineButton
	if page > 0 {
		row = append(row, tb.InlineButton{Unique: "rtg_page", Text: "⬅️", Data: fmt.Sprintf("%s|%d", mode, page-1)})
	}
	if (page+1)*perPage < total {
		row = append(row, tb.InlineButton{Unique: "rtg_page", Text: "➡️", Data: fmt.Sprintf("%s|%d", mode, page+1)})
	}
	var rows [][]tb.InlineButton
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, []tb.InlineButton{{Unique: "rtg_page", Text: "↩️ До рейтингу", Data: "o|0"}})
	kb.InlineKeyboard = rows
	return kb
}

// bodyTimings — среднее время ответа по нашим данным (≥ 2 ответов).
func (m *RatingModule) bodyTimings() map[string]bodyTimingView {
	out := map[string]bodyTimingView{}
	if m.deps.SentLog == nil {
		return out
	}
	for name, t := range m.deps.SentLog.AvgResponseHoursByBody() {
		if t.Count >= 2 {
			out[name] = bodyTimingView{Hours: t.Hours, Count: t.Count}
		}
	}
	return out
}

type bodyTimingView struct {
	Hours float64
	Count int
}

// truncateRunes — обрезка строки по рунам (названия органов бывают длинными).
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
