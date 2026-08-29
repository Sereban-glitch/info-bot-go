package ratelimiter

import (
	"sync"
	"time"
)

// KeyRateLimiter — ограничитель частоты по СТРОЧНОМУ ключу (IP-адрес,
// слаг органа и т.п.), в отличие от базового RateLimiter с int64-ключом
// пользователя Telegram. Тот же алгоритм: фиксированное окно + фоновая
// чистка устаревших корзин.
//
// Зачем: публичные HTTP-эндпоинты (например /api/rating) доступны кому
// угодно, и без ограничения один посетитель мог долбить портал-агрегат
// без остановки.
type KeyRateLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	buckets   map[string]*userBucket
	stopClean chan struct{}
	once      sync.Once
}

// NewKeyLimiter создаёт ограничитель: limit запросов за window на ключ.
// limit <= 0 означает «без ограничений» (используется в тестах и при
// явном отключении нормой в .env).
func NewKeyLimiter(limit int, window time.Duration) *KeyRateLimiter {
	rl := &KeyRateLimiter{
		limit:     limit,
		window:    window,
		buckets:   make(map[string]*userBucket),
		stopClean: make(chan struct{}),
	}
	if limit > 0 {
		go rl.cleanup()
	}
	return rl
}

// Allow пропускает запрос (true), если ключ укладывается в норму.
func (rl *KeyRateLimiter) Allow(key string) bool {
	if rl.limit <= 0 {
		return true
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok || now.Sub(b.windowStart) >= rl.window {
		rl.buckets[key] = &userBucket{count: 1, windowStart: now}
		return true
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

// RetryAfter возвращает, через сколько секунд ключ снова сможет пройти
// (для заголовка Retry-After ответа 429).
func (rl *KeyRateLimiter) RetryAfter(key string) int {
	if rl.limit <= 0 {
		return 0
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		return 0
	}
	elapsed := time.Since(b.windowStart)
	if elapsed >= rl.window {
		return 0
	}
	secs := int(rl.window.Seconds() - elapsed.Seconds())
	if secs < 1 {
		secs = 1
	}
	return secs
}

// Stop завершает фоновую чистку (идемпотентно).
func (rl *KeyRateLimiter) Stop() {
	rl.once.Do(func() {
		if rl.limit > 0 {
			close(rl.stopClean)
		}
	})
}

// cleanup периодически удаляет протухшие корзины, чтобы карта не росла
// бесконечно (защита от утечки памяти при сканировании множеством IP).
func (rl *KeyRateLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-2 * rl.window)
			for key, b := range rl.buckets {
				if b.windowStart.Before(cutoff) {
					delete(rl.buckets, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopClean:
			return
		}
	}
}
