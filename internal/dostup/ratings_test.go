package dostup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpennessIndex(t *testing.T) {
	cases := []struct {
		name      string
		stats     BodyStats
		wantIndex int
		wantRated bool
	}{
		{"92 из 61", BodyStats{Requests: 61, Successful: 56, Overdue: 3}, 92, true},
		{"50 из 34", BodyStats{Requests: 34, Successful: 17, Overdue: 12}, 50, true},
		{"порог: 4 запроса — мало", BodyStats{Requests: 4, Successful: 4}, 0, false},
		{"порог: ровно 5 — в рейтинге", BodyStats{Requests: 5, Successful: 0}, 0, true},
		{"округление вверх", BodyStats{Requests: 7, Successful: 5}, 71, true},
		{"ноль запросов", BodyStats{}, 0, false},
		{"nil-статистика", BodyStats{}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var st *BodyStats
			if tc.name != "nil-статистика" {
				st = &tc.stats
			}
			idx, ok := OpennessIndex(st)
			if ok != tc.wantRated || idx != tc.wantIndex {
				t.Fatalf("OpennessIndex = (%d, %v), хотим (%d, %v)", idx, ok, tc.wantIndex, tc.wantRated)
			}
		})
	}
}

func TestRatingBadge(t *testing.T) {
	if RatingBadge(92) != "🟢" || RatingBadge(70) != "🟢" {
		t.Fatal("индекс ≥70 должен быть 🟢")
	}
	if RatingBadge(69) != "🟡" || RatingBadge(40) != "🟡" {
		t.Fatal("индекс 40–69 должен быть 🟡")
	}
	if RatingBadge(39) != "🔴" || RatingBadge(0) != "🔴" {
		t.Fatal("индекс <40 должен быть 🔴")
	}
}

func TestRatingsStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dostup_ratings.json")
	s1 := NewRatingsStore(path)
	s1.Set("organ_a", BodyStats{Requests: 61, Successful: 56, Overdue: 3})
	s1.Set("organ_b", BodyStats{Requests: 34, Successful: 17, Overdue: 12})
	if err := s1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatalf("файл не создан: %v", err)
	} else if fi.Mode().Perm() != 0600 {
		t.Fatalf("права файла = %v, хотим 0600", fi.Mode().Perm())
	}

	s2 := NewRatingsStore(path)
	s2.Load()
	if s2.Count() != 2 {
		t.Fatalf("после Load Count = %d, хотим 2", s2.Count())
	}
	st, _, ok := s2.Get("organ_a")
	if !ok || st.Requests != 61 || st.Successful != 56 {
		t.Fatalf("organ_a после round-trip: %+v ok=%v", st, ok)
	}
	// «Із 2145»: количество должно пережить рестарт
	if s2.LatestFetch().IsZero() {
		t.Fatal("LatestFetch не должен быть нулевым после Load")
	}
}

func TestRatingsLeaderboard(t *testing.T) {
	s := NewRatingsStore(filepath.Join(t.TempDir(), "r.json"))
	s.Set("good", BodyStats{Requests: 61, Successful: 56, Overdue: 3})
	s.Set("mid", BodyStats{Requests: 34, Successful: 17, Overdue: 12})
	s.Set("bad", BodyStats{Requests: 20, Successful: 1, Overdue: 15})
	s.Set("tiny", BodyStats{Requests: 3, Successful: 0}) // ниже порога
	bodies := []CatalogBody{
		{Slug: "good", Name: "Мін'юст", Region: ""},
		{Slug: "mid", Name: "Обласна адміністрація", Region: "region:63"},
		{Slug: "bad", Name: "Якась установа", Region: ""},
		{Slug: "tiny", Name: "Дрібний орган", Region: ""},
		{Slug: "unknown", Name: "Немає даних", Region: ""},
	}

	// Топ: по убыванию индекса, без tiny/unknown
	best, total := s.Leaderboard(bodies, LeaderOptions{Sort: "best"})
	if total != 3 {
		t.Fatalf("total = %d, хотим 3 (порог ≥5 отрезает tiny)", total)
	}
	want := []string{"good", "mid", "bad"}
	for i, r := range best {
		if r.Slug != want[i] {
			t.Fatalf("best[%d] = %s, хотим %s", i, r.Slug, want[i])
		}
		if r.Rank != i+1 {
			t.Fatalf("rank = %d, хотим %d", r.Rank, i+1)
		}
	}

	// Антирейтинг: по возрастанию
	worst, _ := s.Leaderboard(bodies, LeaderOptions{Sort: "worst"})
	if len(worst) != 3 || worst[0].Slug != "bad" || worst[2].Slug != "good" {
		t.Fatalf("worst порядок неверен: %+v", worst)
	}

	// Поиск по названию (без учёта регистра)
	found, total := s.Leaderboard(bodies, LeaderOptions{Sort: "best", Query: "ЮСТ"})
	if total != 1 || len(found) != 1 || found[0].Slug != "good" {
		t.Fatalf("поиск «ЮСТ»: total=%d found=%+v", total, found)
	}

	// Пагинация
	page2, total := s.Leaderboard(bodies, LeaderOptions{Sort: "best", Offset: 2, Limit: 5})
	if total != 3 || len(page2) != 1 || page2[0].Slug != "bad" {
		t.Fatalf("offset=2: %+v (total %d)", page2, total)
	}
}

func TestRatingsNextBatch(t *testing.T) {
	s := NewRatingsStore(filepath.Join(t.TempDir(), "r.json"))
	now := time.Now()
	s.Set("stale", BodyStats{Requests: 10, Successful: 5})
	// подменяем fetchedAt на «давно» напрямую через entries
	s.mu.Lock()
	e := s.entries["stale"]
	e.FetchedAt = now.Add(-48 * time.Hour)
	s.entries["stale"] = e
	s.mu.Unlock()

	bodies := []CatalogBody{
		{Slug: "stale", Name: "Старый"},
		{Slug: "never1", Name: "Новый 1"},
		{Slug: "never2", Name: "Новый 2"},
		{Slug: "bound", Name: "Биндинг"},
	}
	prefer := map[string]bool{"bound": true}

	batch, total := s.NextBatch(bodies, prefer, 2)
	if total != 4 {
		t.Fatalf("total = %d, хотим 4", total)
	}
	if len(batch) != 2 {
		t.Fatalf("batch len = %d, хотим 2", len(batch))
	}
	// Приоритет 1: биндинг; приоритет 2: никогда не собранные
	if batch[0] != "bound" {
		t.Fatalf("batch[0] = %s, хотим bound (биндинги вперёд)", batch[0])
	}
	if batch[1] != "never1" && batch[1] != "never2" {
		t.Fatalf("batch[1] = %s, хотим never*", batch[1])
	}

	// Биндинг свежий (только что) → в батч не попадает
	s.Set("bound", BodyStats{Requests: 9, Successful: 9})
	batch2, _ := s.NextBatch(bodies, prefer, 2)
	for _, slug := range batch2 {
		if slug == "bound" {
			t.Fatal("свежий биндинг не должен обновляться каждый цикл (раз в сутки)")
		}
	}

	// Всё собрано и свежо — остаются только устаревшие
	s.Set("never1", BodyStats{Requests: 6, Successful: 6})
	s.Set("never2", BodyStats{Requests: 7, Successful: 7})
	batch3, _ := s.NextBatch(bodies, prefer, 1)
	if len(batch3) != 1 || batch3[0] != "stale" {
		t.Fatalf("самый устаревший должен быть первым: %+v", batch3)
	}
}
