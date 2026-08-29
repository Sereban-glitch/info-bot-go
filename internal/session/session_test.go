package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// timeAfter — обёртка для тестов (не тянем time.After напрямую в проверках).
func timeAfter(d time.Duration) <-chan time.Time { return time.After(d) }

func TestSignatureName(t *testing.T) {
	cases := []struct {
		name string
		p    Profile
		want string
	}{
		{"FullName priority", Profile{FullName: "Іван Петренко", FirstName: "Олена", LastName: "Коваль"}, "Іван Петренко"},
		{"parts fallback", Profile{FirstName: "Іван", LastName: "Петренко", MiddleName: "Петрович"}, "Петренко Іван Петрович"},
		{"first name only", Profile{FirstName: "Іван"}, "Іван"},
		{"empty profile", Profile{}, ""},
		{"whitespace FullName", Profile{FullName: "   "}, ""},
	}
	for _, tc := range cases {
		if got := SignatureName(tc.p); got != tc.want {
			t.Errorf("%s: SignatureName=%q, want %q", tc.name, got, tc.want)
		}
	}
}

// ТЗ №5: блокировки сессий — пары Lock/Unlock сериализуют доступ,
// разные ключи не мешают друг другу (разные полосы).
func TestSessionLockStripes(t *testing.T) {
	fs, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	// Один ключ: повторная блокировка того же ключа должна блокировать
	// (проверяем через горутину с таймаутом).
	fs.LockSession("user-1")
	locked := make(chan struct{})
	go func() {
		fs.LockSession("user-1")
		close(locked)
	}()
	select {
	case <-locked:
		t.Fatal("LockSession не заблокировал повторный захват того же ключа")
	case <-timeAfter(100 * time.Millisecond):
		// ожидаемо: держим блокировку
	}
	fs.UnlockSession("user-1")
	select {
	case <-locked:
		// освободилась — вторая горутина взяла
	case <-timeAfter(time.Second):
		t.Fatal("после Unlock вторая горутина так и не получила блокировку")
	}
	fs.UnlockSession("user-1") // горутина взяла и держит — отдадим, чтобы не гонять мьютекс
}

// Delete — сессия исчезает из кэша и с диска.
func TestFileStoreDelete(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	key := SessionKey(12345)
	sess, _ := fs.Get(key)
	sess.Profile.FirstName = "Тест"
	if err := fs.Set(key, sess); err != nil {
		t.Fatal(err)
	}

	if !fs.Delete(key) {
		t.Fatal("Delete должен вернуть true для существующей сессии")
	}
	if _, err := os.Stat(dir + "/" + key + ".json"); !os.IsNotExist(err) {
		t.Fatal("файл сессии не удалён с диска")
	}
	fresh, _ := fs.Get(key) // после удаления — новая пустая
	if fresh.Profile.FirstName != "" {
		t.Fatalf("после удаления профиль не пуст: %q", fresh.Profile.FirstName)
	}
	if fs.Delete(key) {
		t.Fatal("повторный Delete должен вернуть false")
	}
}

// FollowUpThreads.DeleteByUser — гилки пользователя исчезают, чужие остаются.
func TestFollowUpDeleteByUser(t *testing.T) {
	th := NewFollowUpThreads(filepath.Join(t.TempDir(), "fu.json"))
	th.Upsert(1, FollowUpThread{Slug: "a", Subject: "s"})
	th.Upsert(2, FollowUpThread{Slug: "b", Subject: "s"})
	th.DeleteByUser(1)
	if got := th.List(1, 10); len(got) != 0 {
		t.Fatalf("гилки user 1 не удалены: %d", len(got))
	}
	if got := th.List(2, 10); len(got) != 1 {
		t.Fatalf("гилки user 2 пострадали: %d", len(got))
	}
}
