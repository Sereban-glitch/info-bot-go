package dostup

// Рейтинги органов: персистентное хранилище агрегированной статистики
// ответов распорядителей на портале «Доступ до правди».
//
// Источник — публичный фид органа /body/<slug>/feed.json (без авторизации):
// info { requests_count, requests_successful_count, requests_overdue_count,
// requests_not_held_count }. Портал отдаёт только счётчики, без таймингов —
// среднее время ответа считаем отдельно по собственному журналу (sentlog).
//
// «Індекс відкритості» = round(100 × відповідей_по_суті / запитів).
// В рейтинг попадают только органы с ≥ RatingMinRequests запитами —
// порог статистической значимости (меньше — «недостатньо даних»).
//
// Сбор постепенный (портал хрупкий, май 2026 — потеря данных после сбоя):
// фоновый воркер обновляет маленькие батчи за цикл (см. dostupsync.go),
// ленивые запросы пользователя пишут сюда же через BodyStatsCached.
// Хранилище — JSON-файл с атомарной записью (tmp+rename), права 0600.

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RatingMinRequests — минимальное число запитов для места в рейтинге.
const RatingMinRequests = 5

// ratingEntry — элемент хранилища рейтингов.
type ratingEntry struct {
	Stats     BodyStats `json:"stats"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// ratingsFile — формат dostup_ratings.json.
type ratingsFile struct {
	Version int                    `json:"version"`
	SavedAt string                 `json:"savedAt"`
	Entries map[string]ratingEntry `json:"entries"`
}

// LeaderRow — строка лидерборда (данные одного органа).
type LeaderRow struct {
	Rank       int       `json:"rank"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	Region     string    `json:"region,omitempty"`
	Index      int       `json:"index"` // 0..100; органы ниже порога не попадают в лист
	Requests   int       `json:"requests"`
	Successful int       `json:"successful"`
	Overdue    int       `json:"overdue"`
	OverduePct int       `json:"overduePct"`
	FetchedAt  time.Time `json:"fetchedAt"`
}

// LeaderOptions — параметры выборки лидерборда.
type LeaderOptions struct {
	Sort   string // "best" (по убыванию индекса) | "worst" (по возрастанию)
	Query  string // поиск по названию (без учёта регистра); пусто — все
	Offset int
	Limit  int
}

// RatingsStore — потокобезопасное персистентное хранилище рейтингов.
type RatingsStore struct {
	mu      sync.RWMutex
	path    string
	entries map[string]ratingEntry
}

// NewRatingsStore создаёт хранилище; path — файл dostup_ratings.json.
func NewRatingsStore(path string) *RatingsStore {
	return &RatingsStore{
		path:    path,
		entries: map[string]ratingEntry{},
	}
}

// Load загружает файл; отсутствие файла — не ошибка (первый запуск).
func (s *RatingsStore) Load() {
	if s == nil || s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return // нет файла или не читается — начинаем с пустого
	}
	var f ratingsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return // битый файл — не падаем, начинаем заново
	}
	if f.Entries != nil {
		s.mu.Lock()
		s.entries = f.Entries
		s.mu.Unlock()
	}
}

// Save записывает файл атомарно (tmp+rename), права 0600.
func (s *RatingsStore) Save() error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.RLock()
	f := ratingsFile{
		Version: 1,
		SavedAt: time.Now().UTC().Format(time.RFC3339),
		Entries: s.entries,
	}
	s.mu.RUnlock()
	b, err := json.MarshalIndent(&f, "", " ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".dostup_ratings-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}

// Set запоминает статистику органа (в памяти; персистентность — Save()).
func (s *RatingsStore) Set(slug string, st BodyStats) {
	if s == nil || slug == "" {
		return
	}
	s.mu.Lock()
	s.entries[slug] = ratingEntry{Stats: st, FetchedAt: time.Now()}
	s.mu.Unlock()
}

// SetAndSave — Set + атомарная запись (для ленивых одиночных обновлений).
func (s *RatingsStore) SetAndSave(slug string, st BodyStats) error {
	s.Set(slug, st)
	return s.Save()
}

// Get возвращает статистику и время выборки.
func (s *RatingsStore) Get(slug string) (BodyStats, time.Time, bool) {
	if s == nil {
		return BodyStats{}, time.Time{}, false
	}
	s.mu.RLock()
	e, ok := s.entries[slug]
	s.mu.RUnlock()
	return e.Stats, e.FetchedAt, ok
}

// Count — сколько органов уже в хранилище.
func (s *RatingsStore) Count() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	n := len(s.entries)
	s.mu.RUnlock()
	return n
}

// FreshStats — статистика из хранилища, если она свежее ttl.
func (s *RatingsStore) FreshStats(slug string, ttl time.Duration) (BodyStats, bool) {
	st, fetched, ok := s.Get(slug)
	if !ok || time.Since(fetched) >= ttl {
		return BodyStats{}, false
	}
	return st, true
}

// NextBatch — что собирать в этом цикле (приоритеты бережного обхода):
//  1. prefer — органы, выбранные реальными пользователями (биндинги);
//  2. никогда не собранные (наращиваем покрытие);
//  3. самые устаревшие (освежаем).
//
// Возвращает не более limit слагов; total — размер каталога для контроля.
func (s *RatingsStore) NextBatch(bodies []CatalogBody, prefer map[string]bool, limit int) (batch []string, total int) {
	if limit <= 0 {
		return nil, len(bodies)
	}
	type cand struct {
		slug  string
		fetch time.Time
		prio  int // 0 — prefer, 1 — никогда, 2 — устаревший
	}
	var cands []cand
	s.mu.RLock()
	for _, b := range bodies {
		if b.Slug == "" {
			continue
		}
		e, ok := s.entries[b.Slug]
		switch {
		case prefer[b.Slug]:
			// биндинги обновляем раз в сутки максимум, чтобы не долбить
			if !ok || time.Since(e.FetchedAt) > 24*time.Hour {
				cands = append(cands, cand{b.Slug, e.FetchedAt, 0})
			}
		case !ok:
			cands = append(cands, cand{b.Slug, time.Time{}, 1})
		default:
			cands = append(cands, cand{b.Slug, e.FetchedAt, 2})
		}
	}
	s.mu.RUnlock()
	if len(cands) == 0 {
		return nil, len(bodies)
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].prio != cands[j].prio {
			return cands[i].prio < cands[j].prio
		}
		return cands[i].fetch.Before(cands[j].fetch) // самые старые вперёд
	})
	if len(cands) > limit {
		cands = cands[:limit]
	}
	for _, c := range cands {
		batch = append(batch, c.slug)
	}
	return batch, len(bodies)
}

// OpennessIndex — «індекс відкритості» органа (0..100).
// ok=false — данных недостаточно (меньше RatingMinRequests запитів).
func OpennessIndex(st *BodyStats) (int, bool) {
	if st == nil || st.Requests < RatingMinRequests {
		return 0, false
	}
	return int(math.Round(100 * float64(st.Successful) / float64(st.Requests))), true
}

// RatingBadge — эмодзи-бейдж по индексу.
func RatingBadge(index int) string {
	switch {
	case index >= 70:
		return "🟢"
	case index >= 40:
		return "🟡"
	default:
		return "🔴"
	}
}

// Leaderboard строит рейтинг по каталогу (названия берутся из каталога).
// Возвращает страницу строк и общее число органов в рейтинге (до offset/limit).
func (s *RatingsStore) Leaderboard(bodies []CatalogBody, opts LeaderOptions) ([]LeaderRow, int) {
	if s == nil {
		return nil, 0
	}
	q := strings.ToLower(strings.TrimSpace(opts.Query))
	s.mu.RLock()
	var rows []LeaderRow
	for _, b := range bodies {
		e, ok := s.entries[b.Slug]
		if !ok {
			continue
		}
		idx, rated := OpennessIndex(&e.Stats)
		if !rated {
			continue // ниже порога — не оцениваем
		}
		if q != "" && !strings.Contains(strings.ToLower(b.Name), q) {
			continue
		}
		rows = append(rows, LeaderRow{
			Slug:       b.Slug,
			Name:       b.Name,
			Region:     b.Region,
			Index:      idx,
			Requests:   e.Stats.Requests,
			Successful: e.Stats.Successful,
			Overdue:    e.Stats.Overdue,
			OverduePct: e.Stats.OverduePct(),
			FetchedAt:  e.FetchedAt,
		})
	}
	s.mu.RUnlock()

	worst := opts.Sort == "worst"
	sort.SliceStable(rows, func(i, j int) bool {
		if worst {
			return rows[i].Index < rows[j].Index
		}
		return rows[i].Index > rows[j].Index
	})
	total := len(rows)
	for i := range rows {
		rows[i].Rank = i + 1
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	if opts.Offset > total {
		opts.Offset = total
	}
	rows = rows[opts.Offset:]
	if opts.Limit > 0 && len(rows) > opts.Limit {
		rows = rows[:opts.Limit]
	}
	return rows, total
}

// LatestFetch — время самой свежей выборки в хранилище (для «станом на»).
func (s *RatingsStore) LatestFetch() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.RLock()
	var latest time.Time
	for _, e := range s.entries {
		if e.FetchedAt.After(latest) {
			latest = e.FetchedAt
		}
	}
	s.mu.RUnlock()
	return latest
}
