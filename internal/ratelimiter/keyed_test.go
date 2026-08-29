package ratelimiter

import (
	"testing"
	"time"
)

// ТЗ №4, D1: строчный ограничитель — базовые нормы.
func TestKeyLimiterAllowsWithinLimit(t *testing.T) {
	rl := NewKeyLimiter(3, time.Minute)
	defer rl.Stop()

	for i := 1; i <= 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("запрос %d из 3 должен проходить", i)
		}
	}
	if rl.Allow("1.2.3.4") {
		t.Fatal("4-й запрос за минуту должен блокироваться")
	}
	// другой ключ — своя корзина
	if !rl.Allow("5.6.7.8") {
		t.Fatal("другой IP не должен страдать от лимита соседа")
	}
}

// ТЗ №4, D1: RetryAfter — разумное число секунд до разблокировки.
func TestKeyLimiterRetryAfter(t *testing.T) {
	rl := NewKeyLimiter(1, time.Minute)
	defer rl.Stop()

	if ra := rl.RetryAfter("ip"); ra != 0 {
		t.Fatalf("до исчерпания лимита RetryAfter=0, получили %d", ra)
	}
	rl.Allow("ip")
	rl.Allow("ip") // блок
	ra := rl.RetryAfter("ip")
	if ra <= 0 || ra > 60 {
		t.Fatalf("RetryAfter должен быть в (0, 60], получили %d", ra)
	}
}

// ТЗ №4, D1: limit <= 0 — режим «лимит отключён» (отладка).
func TestKeyLimiterDisabled(t *testing.T) {
	rl := NewKeyLimiter(0, time.Minute)
	for i := 0; i < 1000; i++ {
		if !rl.Allow("ip") {
			t.Fatal("при limit<=0 всё должно проходить")
		}
	}
}

// Окно должно сбрасываться со временем.
func TestKeyLimiterWindowReset(t *testing.T) {
	rl := NewKeyLimiter(1, 30*time.Millisecond)
	defer rl.Stop()

	if !rl.Allow("ip") {
		t.Fatal("первый запрос должен проходить")
	}
	if rl.Allow("ip") {
		t.Fatal("второй запрос в том же окне — блок")
	}
	time.Sleep(40 * time.Millisecond)
	if !rl.Allow("ip") {
		t.Fatal("после истечения окна счётчик должен обнулиться")
	}
}
