package moderation

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreEnqueuePending(t *testing.T) {
	s := NewStore("")
	it := s.Enqueue(Item{UserID: 1, ChatID: 1, Title: "про розвідку", Body: "x", Status: StatusPending})
	if it.ID == "" || len(it.ID) != 8 {
		t.Fatalf("ID не згенеровано або не 8 символів: %q", it.ID)
	}
	if it.Status != StatusPending {
		t.Fatalf("статус після Enqueue = %q, хочемо pending", it.Status)
	}
	if it.CreatedAt.IsZero() {
		t.Error("CreatedAt не виставлено")
	}
	if got := s.PendingCount(); got != 1 {
		t.Fatalf("PendingCount=%d, хочемо 1", got)
	}
	pend := s.Pending()
	if len(pend) != 1 || pend[0].ID != it.ID {
		t.Fatalf("Pending повернув %v", pend)
	}
}

func TestStoreClaimOnce(t *testing.T) {
	s := NewStore("")
	it := s.Enqueue(Item{Title: "секретно", Status: StatusPending})
	if _, ok := s.Claim(it.ID); !ok {
		t.Fatal("перший Claim має перемогти")
	}
	if _, ok := s.Claim(it.ID); ok {
		t.Fatal("другий Claim (гонка/дубль-клік) має програти")
	}
	if s.PendingCount() != 0 {
		t.Error("claimed-запит не має бути у Pending")
	}
	// Release повертає в очікування — власник натисне ✅ ще раз
	s.Release(it.ID)
	if s.PendingCount() != 1 {
		t.Error("Release має повертати в pending")
	}
	if _, ok := s.Claim(it.ID); !ok {
		t.Error("після Release Claim має працювати")
	}
}

func TestStoreSetStatus(t *testing.T) {
	s := NewStore("")
	it := s.Enqueue(Item{Title: "x"})
	if _, ok := s.Claim(it.ID); !ok {
		t.Fatal("claim")
	}
	s.SetStatus(it.ID, StatusApproved, "https://dostup.org.ua/request/abc")
	pend := s.Pending()
	if len(pend) != 0 {
		t.Fatalf("після approved Pending=%v, хочемо порожньо", pend)
	}
	// Рішення зберігається (аудит-слід)
	found := false
	for _, x := range s.list {
		if x.ID == it.ID {
			found = true
			if x.Status != StatusApproved || x.ResultURL != "https://dostup.org.ua/request/abc" {
				t.Errorf("статус/URL не збережено: %+v", x)
			}
			if x.DecidedAt.IsZero() {
				t.Error("DecidedAt не виставлено")
			}
		}
	}
	if !found {
		t.Error("рішення має лишатись у журналі черги")
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moderation_queue.json")

	s1 := NewStore(path)
	it1 := s1.Enqueue(Item{UserID: 7, Title: "озброєння", Status: StatusPending})
	it2 := s1.Enqueue(Item{UserID: 8, Title: "спокійний", Status: StatusPending})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("файл черги не створено: %v", err)
	}

	// «Рестарт»: новий інстанс бачить ту саму чергу
	s2 := NewStore(path)
	if s2.PendingCount() != 2 {
		t.Fatalf("після рестарту PendingCount=%d, хочемо 2", s2.PendingCount())
	}
	if _, ok := s2.Claim(it1.ID); !ok {
		t.Error("claim після рестарту має працювати")
	}
	s2.SetStatus(it1.ID, StatusRejected, "")
	if s2.PendingCount() != 1 {
		t.Errorf("після рішення PendingCount=%d, хочемо 1 (залишився %s)", s2.PendingCount(), it2.ID)
	}
}

func TestStorePruneDecided(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moderation_queue.json")
	s := NewStore(path)
	old := s.Enqueue(Item{Title: "старе рішення"})
	s.SetStatus(old.ID, StatusRejected, "")
	// штучно постарімо рішення
	for i := range s.list {
		if s.list[i].ID == old.ID {
			s.list[i].DecidedAt = time.Now().Add(-40 * 24 * time.Hour)
		}
	}
	fresh := s.Enqueue(Item{Title: "свіже рішення"})
	s.SetStatus(fresh.ID, StatusApproved, "u")
	pending := s.Enqueue(Item{Title: "в очікуванні"})

	s2 := NewStore(path) // prune спрацьовує на завантаженні
	s2IDs := map[string]bool{}
	for _, it := range s2.list {
		s2IDs[it.ID] = true
	}
	if s2IDs[old.ID] {
		t.Error("старе рішення (40 днів) мало бути видалене")
	}
	if !s2IDs[fresh.ID] {
		t.Error("свіже рішення має лишитись (аудит-слід місяць)")
	}
	if !s2IDs[pending.ID] {
		t.Error("pending не видаляється НІКОЛИ, навіть старий")
	}
}

func TestStoreUniqueIDs(t *testing.T) {
	s := NewStore("")
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		it := s.Enqueue(Item{Title: "x"})
		if seen[it.ID] {
			t.Fatalf("колізія ID: %s", it.ID)
		}
		seen[it.ID] = true
	}
}
