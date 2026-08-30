package handlers

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPendingSubmitsMatchAndTake(t *testing.T) {
	p := NewPendingSubmits("")
	p.Add(PendingSubmit{
		UserID: 6919677903, ChatID: 6919677903,
		Title: "Запит про надання інформації щодо військової посади Буданова К.О.",
		Organ: "Офіс Президента України",
		At:    time.Now().Add(-10 * time.Minute),
	})
	p.Add(PendingSubmit{
		UserID: 42, ChatID: 42,
		Title: "Стан доріг", Organ: "ОДА",
		At: time.Now().Add(-2 * time.Hour),
	})

	// Точное совпадение темы (портал мог обрезать): первые 30 байт входят
	got := p.TakeMatching("Запит про надання інформації щодо військової посади Буданова К.О.", 24*time.Hour)
	if got == nil {
		t.Fatal("совпадение не найдено")
	}
	if got.UserID != 6919677903 {
		t.Errorf("UserID=%d, want 6919677903", got.UserID)
	}
	// Изъято: повторный поиск той же темы — уже ничего
	if again := p.TakeMatching("Запит про надання інформації щодо військової посади Буданова К.О.", 24*time.Hour); again != nil {
		t.Errorf("попытка не изъята: %+v", again)
	}
	// Вторая попытка на месте
	if got2 := p.TakeMatching("Стан доріг", 24*time.Hour); got2 == nil || got2.UserID != 42 {
		t.Errorf("вторая попытка потерялась: %+v", got2)
	}
}

func TestPendingSubmitsAge(t *testing.T) {
	p := NewPendingSubmits("")
	p.Add(PendingSubmit{UserID: 7, Title: "Давня спроба", At: time.Now().Add(-30 * time.Hour)})
	if got := p.TakeMatching("Давня спроба", 24*time.Hour); got != nil {
		t.Errorf("протухшая попытка сматчилась: %+v", got)
	}
}

func TestPendingSubmitsNoMatch(t *testing.T) {
	p := NewPendingSubmits("")
	p.Add(PendingSubmit{UserID: 7, Title: "Одна тема", At: time.Now()})
	if got := p.TakeMatching("Совсем другая тема", 24*time.Hour); got != nil {
		t.Errorf("ложное совпадение: %+v", got)
	}
	if got := p.TakeMatching("", 24*time.Hour); got != nil {
		t.Errorf("пустая тема сматчилась: %+v", got)
	}
}

// Персистентность: запись сохраняется на диск и подхватывается новым экземпляром.
func TestPendingSubmitsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending_submits.json")
	p1 := NewPendingSubmits(path)
	p1.Add(PendingSubmit{
		UserID: 55, ChatID: 55, Title: "Персистентна спроба",
		At: time.Now().Add(-5 * time.Minute),
	})

	p2 := NewPendingSubmits(path)
	got := p2.TakeMatching("Персистентна спроба", 24*time.Hour)
	if got == nil || got.UserID != 55 {
		t.Fatalf("после перезапуска попытка не найдена: %+v", got)
	}
}

// Протухшие записи чистятся при загрузке.
func TestPendingSubmitsPruneOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending_submits.json")
	p1 := NewPendingSubmits(path)
	p1.Add(PendingSubmit{UserID: 1, Title: "стара", At: time.Now().Add(-48 * time.Hour)})
	p1.Add(PendingSubmit{UserID: 2, Title: "свіжа", At: time.Now().Add(-1 * time.Hour)})

	p2 := NewPendingSubmits(path)
	if got := p2.TakeMatching("стара", 72*time.Hour); got != nil {
		t.Errorf("старая попытка пережила загрузку: %+v", got)
	}
	if got := p2.TakeMatching("свіжа", 24*time.Hour); got == nil {
		t.Error("свежая попытка потерялась при загрузке")
	}
}

func TestPendingMatchesByTitle(t *testing.T) {
	cases := []struct {
		pending, portal string
		want            bool
	}{
		{"Тема запиту", "Тема запиту", true},
		{"тема запиту", "ТЕМА ЗАПИТУ", true}, // регистр не важен
		{"Довга тема запиту, яку портал обрізає", "Довга тема запиту, яку портал обрізає і додає", true},
		{"Тема А", "Тема Б", false},
		{"", "Що завгодно", false},
	}
	for _, tc := range cases {
		if got := pendingMatchesByTitle(tc.pending, tc.portal); got != tc.want {
			t.Errorf("pendingMatchesByTitle(%q, %q)=%v, want %v", tc.pending, tc.portal, got, tc.want)
		}
	}
}
