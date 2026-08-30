package handlers

// Тижневий дайджест власнику (ТЗ №8, блок E3).
//
// Раз на неделю (понедельник 09:00 по Киеву) бот сам присылает владельцу:
//   • три цифры недели: +пользователи, +запросы, +розборы (ШАГ 5 плана
//     запуска — «три цифры раз в неделю», чтобы понимать, растёт продукт
//     или буксует, без ручных проверок);
//   • топ-5 самых открытых и топ-5 самых закрытых органов из рейтинга
//     (2145 органов, данные dostup.org.ua);
//   • готовый текст поста для канала — антирейтинг можно публиковать
//     как есть (ШАГ 2 плана запуска: рейтинг — витрина продукта).
//
// /digest — тот же отчёт по требованию (только владелец).
// Состояние (когда отправляли + снимок счётчиков для дельты недели)
// хранится в digest_state.json; дельта = текущие счётчики − снимок.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/dostup"
	"info-bot-go/internal/safego"
)

// digestSnapshot — снимок глобальных счётчиков на момент прошлого дайджеста.
type digestSnapshot struct {
	Users    int `json:"users"`
	Requests int `json:"requests"`
	Replies  int `json:"replies"`
	Analyze  int `json:"analyze"`
}

type digestState struct {
	LastSentAt time.Time      `json:"lastSentAt,omitempty"`
	Snapshot   digestSnapshot `json:"snapshot"`
}

// DigestModule — 25-й модуль: еженедельный дайджест владельцу.
type DigestModule struct {
	deps  *Deps
	bot   *tb.Bot
	path  string
	mu    sync.Mutex
	state digestState
}

func NewDigestModule(deps *Deps) *DigestModule {
	m := &DigestModule{deps: deps, bot: deps.Bot}
	if deps.Cfg != nil && deps.Cfg.SessionDir != "" {
		m.path = filepath.Join(deps.Cfg.SessionDir, "digest_state.json")
	}
	if m.path != "" {
		if b, err := os.ReadFile(m.path); err == nil {
			var st digestState
			if err := json.Unmarshal(b, &st); err == nil {
				m.state = st
			}
		}
	}
	return m
}

func (m *DigestModule) Name() string { return "digest" }

func (m *DigestModule) Register() {
	m.bot.Handle("/digest", safeHandler("digest", m.handleDigest))
}

// Start — фоновый цикл: проверка раз в 30 минут, отправка в понедельник
// после DIGEST_HOUR. Каждая итерация под safego — паника не убивает цикл.
func (m *DigestModule) Start() {
	if m.deps.Cfg == nil || !m.deps.Cfg.DigestEnabled || m.deps.Cfg.AdminID == 0 {
		return
	}
	check := func() {
		m.mu.Lock()
		last := m.state.LastSentAt
		m.mu.Unlock()
		if !digestDue(last, time.Now(), kyivLoc(), m.deps.Cfg.DigestHour) {
			return
		}
		m.sendAndPersist()
	}
	safego.Run("digest-boot", check) // рестарт в понедельник после часа отправки
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			safego.Run("digest", check)
		}
	}()
}

// handleDigest — /digest: отчёт по требованию (только владелец).
func (m *DigestModule) handleDigest(c tb.Context) error {
	if m.deps.Cfg.AdminID == 0 || c.Sender().ID != m.deps.Cfg.AdminID {
		return c.Send("ℹ️ Команда доступна лише власнику бота.")
	}
	text := m.buildReport()
	if _, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), text, tb.ModeHTML); err != nil {
		log.Printf("[DIGEST] send failed: %v", err)
		return c.Send("❌ Не вдалося надіслати дайджест (див. журнал).")
	}
	m.persistState()
	return nil
}

// sendAndPersist — авто-отправка и фиксация снимка/времени.
func (m *DigestModule) sendAndPersist() {
	text := m.buildReport()
	if _, err := m.bot.Send(tb.ChatID(m.deps.Cfg.AdminID), text, tb.ModeHTML); err != nil {
		log.Printf("[DIGEST] weekly send failed: %v", err)
		return // снимок не обновляем — попробуем на следующем тике
	}
	log.Printf("[DIGEST] тижневий дайджест надіслано")
	m.persistState()
}

// persistState — снимает текущие счётчики и сохраняет состояние.
func (m *DigestModule) persistState() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.LastSentAt = time.Now()
	m.state.Snapshot = m.currentSnapshot()
	if m.path == "" {
		return
	}
	b, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("[DIGEST] state write: %v", err)
		return
	}
	_ = os.Rename(tmp, m.path)
}

// currentSnapshot — счётчики прямо сейчас.
func (m *DigestModule) currentSnapshot() digestSnapshot {
	s := digestSnapshot{}
	if m.deps.Stats == nil {
		return s
	}
	g := m.deps.Stats.Get()
	s.Users = g.TotalUsers
	s.Requests = g.TotalRequestsSent
	s.Replies = g.TotalRepliesReceived
	s.Analyze = g.ModuleUsage["analyze"]
	return s
}

// buildReport — полный текст дайджеста.
func (m *DigestModule) buildReport() string {
	m.mu.Lock()
	prev := m.state.Snapshot
	m.mu.Unlock()
	cur := m.currentSnapshot()

	best, worst, rated, asOf := m.ratingTops()
	return buildDigestText(cur, prev, best, worst, rated, asOf, time.Now())
}

// ratingTops — топ-5 открытых и закрытых органов рейтинга.
func (m *DigestModule) ratingTops() (best, worst []dostup.LeaderRow, rated int, asOf time.Time) {
	if m.deps.DostupRatings == nil || m.deps.DostupCatalog == nil {
		return nil, nil, 0, time.Time{}
	}
	catalog := m.deps.DostupCatalog.Get()
	if catalog == nil {
		return nil, nil, 0, time.Time{}
	}
	best, rated = m.deps.DostupRatings.Leaderboard(catalog.Bodies, dostup.LeaderOptions{Sort: "best", Limit: 5})
	worst, _ = m.deps.DostupRatings.Leaderboard(catalog.Bodies, dostup.LeaderOptions{Sort: "worst", Limit: 5})
	return best, worst, rated, m.deps.DostupRatings.LatestFetch()
}

// buildDigestText — чистая сборка текста дайджеста (для тестов).
func buildDigestText(cur, prev digestSnapshot, best, worst []dostup.LeaderRow, rated int, asOf, now time.Time) string {
	weekAgo := now.AddDate(0, 0, -7)
	head := fmt.Sprintf("📊 <b>Тижневий дайджест</b> · %s – %s\n\n",
		weekAgo.Format("02.01"), now.Format("02.01"))

	delta := func(name string, c, p int, total string) string {
		d := c - p
		if d < 0 {
			d = 0 // счётчики могли скорректироваться синхронизацией
		}
		return fmt.Sprintf("• %s: <b>+%d</b> (разом %s)\n", name, d, total)
	}
	body := "👥 <b>Люди за тиждень:</b>\n" +
		delta("Нових користувачів", cur.Users, prev.Users, fmt.Sprintf("%d", cur.Users)) +
		delta("Запитів надіслано", cur.Requests, prev.Requests, fmt.Sprintf("%d", cur.Requests)) +
		delta("Відповідей отримано", cur.Replies, prev.Replies, fmt.Sprintf("%d", cur.Replies)) +
		delta("AI-розборів", cur.Analyze, prev.Analyze, fmt.Sprintf("%d", cur.Analyze))

	rowLine := func(r dostup.LeaderRow) string {
		return fmt.Sprintf("%d. %s — індекс %d (запитів %d, прострочено %d%%)\n",
			r.Rank, r.Name, r.Index, r.Requests, r.OverduePct)
	}

	var tops string
	if len(best) > 0 {
		tops += "\n🏆 <b>Топ-5 найвідкритіших органів:</b>\n"
		for _, r := range best {
			tops += rowLine(r)
		}
	}
	if len(worst) > 0 {
		tops += "\n🧱 <b>Антирейтинг — топ-5 «закритих»:</b>\n"
		for _, r := range worst {
			tops += rowLine(r)
		}
	}
	if rated > 0 {
		tops += fmt.Sprintf("\n<i>Оцінено %d органів", rated)
		if !asOf.IsZero() {
			tops += ", дані станом на " + asOf.Format("02.01 15:04")
		}
		tops += ".</i>\n"
	}

	post := ""
	if len(worst) > 0 {
		post = "\n📝 <b>Готовий пост для каналу</b> (скопіюйте як є):\n\n" +
			"🧱 Топ-5 органів, що найгірше відповідають на запити громадян.\n" +
			fmt.Sprintf("Дані порталу dostup.org.ua, оцінено %d органів України.\n\n", rated)
		for _, r := range worst {
			post += fmt.Sprintf("%d. %s — проігноровано чи прострочено %d%% запитів.\n",
				r.Rank, r.Name, r.OverduePct)
		}
		post += "\nПеревірити будь-який орган і надіслати запит: @Infozaputbot"
	}

	return head + body + tops + post
}

// digestDue — пора ли отправлять: понедельник (Киев), час >= hour,
// и в этот понедельник ещё не отправляли.
func digestDue(lastSent, now time.Time, loc *time.Location, hour int) bool {
	kyiv := now.In(loc)
	if kyiv.Weekday() != time.Monday {
		return false
	}
	if kyiv.Hour() < hour {
		return false
	}
	if !lastSent.IsZero() {
		mondayStart := time.Date(kyiv.Year(), kyiv.Month(), kyiv.Day(), 0, 0, 0, 0, loc)
		if lastSent.In(loc).After(mondayStart) || lastSent.In(loc).Equal(mondayStart) {
			return false // уже отправляли в этот понедельник
		}
	}
	return true
}

// kyivLoc — часовой пояс Киева (без DST с 2023, но берём базу tz, если есть).
func kyivLoc() *time.Location {
	if loc, err := time.LoadLocation("Europe/Kyiv"); err == nil {
		return loc
	}
	return time.FixedZone("Kyiv", 3*3600)
}
