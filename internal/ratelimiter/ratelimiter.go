package ratelimiter

import (
	"sync"
	"time"
)

// userBucket tracks request count per user within a time window.
type userBucket struct {
	count       int
	windowStart time.Time
}

// RateLimiter provides per-user fixed-window rate limiting.
type RateLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	buckets   map[int64]*userBucket
	stopClean chan struct{}
}

// New creates a RateLimiter allowing limit requests per window per user.
func New(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		limit:     limit,
		window:    window,
		buckets:   make(map[int64]*userBucket),
		stopClean: make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Allow returns true if the user is allowed to make a request.
// Automatically resets the counter if the window has expired.
func (rl *RateLimiter) Allow(userID int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[userID]
	if !ok || now.Sub(b.windowStart) >= rl.window {
		rl.buckets[userID] = &userBucket{count: 1, windowStart: now}
		return true
	}
	if b.count >= rl.limit {
		return false
	}
	b.count++
	return true
}

// Remaining returns how many requests the user can still make in the current window.
func (rl *RateLimiter) Remaining(userID int64) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[userID]
	if !ok || time.Since(b.windowStart) >= rl.window {
		return rl.limit
	}
	return rl.limit - b.count
}

// Stop terminates the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopClean)
}

// cleanup periodically removes stale entries to prevent memory leaks.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-2 * rl.window)
			for id, b := range rl.buckets {
				if b.windowStart.Before(cutoff) {
					delete(rl.buckets, id)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopClean:
			return
		}
	}
}
