package sentlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ТЗ №5, фикс спама: две записи с одинаковым MessageID — обновиться
// должны ОБЕ (раньше обновлялась только первая, вторая спамила
// уведомлением каждый цикл синхронизации).
func TestMarkAcknowledgedUpdatesAllDuplicates(t *testing.T) {
	sl, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sl.Close()

	_ = sl.Append(SentEntry{MessageID: "dostup:same", UserID: 1, Subject: "Первая"})
	_ = sl.Append(SentEntry{MessageID: "dostup:same", UserID: 1, Subject: "Вторая"})

	if err := sl.MarkAcknowledged("dostup:same", "waiting_response", "лист отримано", "1172"); err != nil {
		t.Fatalf("MarkAcknowledged: %v", err)
	}

	for i, e := range sl.ListAll() {
		if e.LastIncomingID != "1172" {
			t.Fatalf("запись %d (%s): LastIncomingID=%q — дубль не обновлён, спам вернётся", i, e.Subject, e.LastIncomingID)
		}
		if e.AckAt == "" {
			t.Fatalf("запись %d (%s): AckAt пустой", i, e.Subject)
		}
	}
}

// UpdateDostupStatus — то же правило: все дубли получают статус.
func TestUpdateDostupStatusUpdatesAllDuplicates(t *testing.T) {
	sl, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sl.Close()

	_ = sl.Append(SentEntry{MessageID: "dostup:x", UserID: 1})
	_ = sl.Append(SentEntry{MessageID: "dostup:x", UserID: 1})
	_ = sl.Append(SentEntry{MessageID: "dostup:y", UserID: 2})

	if err := sl.UpdateDostupStatus("dostup:x", "waiting_response", "excerpt", "55", false); err != nil {
		t.Fatalf("UpdateDostupStatus: %v", err)
	}

	for _, e := range sl.ListAll() {
		if e.MessageID == "dostup:x" && e.LastIncomingID != "55" {
			t.Fatalf("дубль dostup:x не получил lastIncomingID")
		}
		if e.MessageID == "dostup:y" && e.LastIncomingID != "" {
			t.Fatalf("чужая запись изменилась")
		}
	}
}

// DeleteByUser — удаляются только записи пользователя, остальные живы.
func TestDeleteByUser(t *testing.T) {
	dir := t.TempDir()
	sl, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	_ = sl.Append(SentEntry{MessageID: "a", UserID: 7, Subject: "моя"})
	_ = sl.Append(SentEntry{MessageID: "b", UserID: 8, Subject: "чужая"})
	_ = sl.Append(SentEntry{MessageID: "c", UserID: 7, Subject: "моя2"})

	n, err := sl.DeleteByUser(7)
	if err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}
	if n != 2 {
		t.Fatalf("удалено %d, ожидаем 2", n)
	}
	rest := sl.ListAll()
	if len(rest) != 1 || rest[0].UserID != 8 {
		t.Fatalf("остались неверные записи: %+v", rest)
	}
	sl.Close()

	// Проверяем персистентность: перечитываем журнал с диска
	sl2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer sl2.Close()
	again := sl2.ListAll()
	if len(again) != 1 || again[0].MessageID != "b" {
		t.Fatalf("после перечитывания осталось %+v — удаление не сохранилось", again)
	}
}

// Удаление несуществующего пользователя — не ошибка, файл не трогаем.
func TestDeleteByUserNoop(t *testing.T) {
	dir := t.TempDir()
	sl, _ := New(dir)
	_ = sl.Append(SentEntry{MessageID: "a", UserID: 1})
	n, err := sl.DeleteByUser(999)
	if err != nil || n != 0 {
		t.Fatalf("no-op удаление: n=%d err=%v", n, err)
	}
	if len(sl.ListAll()) != 1 {
		t.Fatal("запись пропала без причины")
	}
}

// Атомарность flush: в файле нет мусора и JSON валиден построчно.
func TestFlushKeepsJSONLines(t *testing.T) {
	dir := t.TempDir()
	sl, _ := New(dir)
	_ = sl.Append(SentEntry{MessageID: "a", UserID: 1, Subject: "тест"})
	_ = sl.Append(SentEntry{MessageID: "b", UserID: 2, Subject: "апостроф 'здоров'я'"})
	sl.Close()

	raw, err := readFileLines(filepath.Join(dir, "_sent_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 {
		t.Fatalf("строк в журнале: %d, ожидаем 2", len(raw))
	}
	if !strings.Contains(raw[1], "здоров") {
		t.Fatal("апостроф потерялся")
	}
}

// readFileLines — построчное чтение журнала (для проверки формата).
func readFileLines(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out, nil
}
