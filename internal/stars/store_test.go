package stars

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreWelcomeOnce(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "c.json"))

	// Новому пользователю — бонус один раз.
	if got := s.EnsureWelcome(42, 3); got != 3 {
		t.Fatalf("первый welcome = %d, want 3", got)
	}
	if got := s.EnsureWelcome(42, 3); got != 0 {
		t.Fatalf("повторный welcome = %d, want 0 (бонус только один)", got)
	}
	if s.Balance(42) != 3 {
		t.Fatalf("баланс = %d, want 3", s.Balance(42))
	}
}

func TestStoreSpend(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "c.json"))

	if s.Spend(1, 1) {
		t.Fatal("Spend без кредитов прошёл (хотел отказ)")
	}
	_ = s.Add(1, 5)
	if !s.Spend(1, 3) || s.Balance(1) != 2 {
		t.Fatalf("после траты 3 из 5 баланс = %d, want 2", s.Balance(1))
	}
	if s.Spend(1, 3) {
		t.Fatal("перерасход прошёл")
	}
	if !s.Spend(1, 2) || s.Balance(1) != 0 {
		t.Fatalf("баланс до нуля: %d, want 0", s.Balance(1))
	}
}

func TestStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	s1 := NewStore(path)
	s1.EnsureWelcome(7, 3)
	_ = s1.Add(7, 10)

	// «Рестарт» — новое хранилище того же файла.
	s2 := NewStore(path)
	if s2.Balance(7) != 13 {
		t.Fatalf("после рестарта баланс = %d, want 13", s2.Balance(7))
	}
	if got := s2.EnsureWelcome(7, 3); got != 0 {
		t.Fatalf("после рестарта welcome повторился (%d) — дубль бонуса", got)
	}
}

func TestStoreChargeDedup(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "c.json"))

	if !s.ChargeIfNew("chg-1") {
		t.Fatal("первый платёж посчитан дубликатом")
	}
	if s.ChargeIfNew("chg-1") {
		t.Fatal("повторная доставка платежа НЕ распознана как дубль — двойное зачисление!")
	}
	if !s.ChargeIfNew("chg-2") {
		t.Fatal("другой платёж посчитан дубликатом")
	}
	if s.WasCharged("chg-1") != true || s.WasCharged("nope") != false {
		t.Fatal("WasCharged врёт")
	}
}

func TestStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.json")
	// битый файл не должен ронять бота
	_ = writeFile(path, "not json{")
	s := NewStore(path)
	if s.Balance(1) != 0 {
		t.Fatal("битый файл дал кредиты")
	}
	if s.Spend(1, 1) {
		t.Fatal("Spend при нулевом балансе должен отказать")
	}
}

// writeFile — маленький хелпер для теста битого файла.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
