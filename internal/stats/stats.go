package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// GlobalStats holds all counters for the bot dashboard.
type GlobalStats struct {
	TotalUsers           int            `json:"totalUsers"`
	TotalRequestsSent    int            `json:"totalRequestsSent"`
	TotalRepliesReceived int            `json:"totalRepliesReceived"`
	TotalBounced         int            `json:"totalBounced"`
	DailyRequestsSent    int            `json:"dailyRequestsSent"`
	DailyRequestsDate    string         `json:"dailyRequestsDate"`
	ModuleUsage          map[string]int `json:"moduleUsage"`
	UpdatedAt            string         `json:"updatedAt"`
}

// Stats manages the global stats file with thread-safe access.
type Stats struct {
	path string
	mu   sync.RWMutex
	data GlobalStats
}

// New creates a new Stats instance, loading existing data from file.
func New(dir string) (*Stats, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "_global_stats.json")
	s := &Stats{path: path}

	raw, err := os.ReadFile(path)
	if err != nil {
		s.data = GlobalStats{
			ModuleUsage:       make(map[string]int),
			DailyRequestsDate: time.Now().Format("2006-01-02"),
		}
		_ = s.flush()
		return s, nil
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		s.data = GlobalStats{
			ModuleUsage:       make(map[string]int),
			DailyRequestsDate: time.Now().Format("2006-01-02"),
		}
	}
	if s.data.ModuleUsage == nil {
		s.data.ModuleUsage = make(map[string]int)
	}
	return s, nil
}

// Get returns a copy of the current global stats.
func (s *Stats) Get() GlobalStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

// IncrementRequests adds +1 to total and daily request counters.
func (s *Stats) IncrementRequests() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetDailyIfNeeded()
	s.data.TotalRequestsSent++
	s.data.DailyRequestsSent++
	s.data.UpdatedAt = time.Now().Format(time.RFC3339)
	_ = s.flush()
}

// IncrementUsers adds +1 to total unique users.
func (s *Stats) IncrementUsers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.TotalUsers++
	s.data.UpdatedAt = time.Now().Format(time.RFC3339)
	_ = s.flush()
}

// IncrementReplies adds +1 to total replies received.
func (s *Stats) IncrementReplies() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.TotalRepliesReceived++
	s.data.UpdatedAt = time.Now().Format(time.RFC3339)
	_ = s.flush()
}

// IncrementBounced adds +1 to total bounced emails.
func (s *Stats) IncrementBounced() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.TotalBounced++
	s.data.UpdatedAt = time.Now().Format(time.RFC3339)
	_ = s.flush()
}

// IncrementModule adds +1 to the module usage counter.
func (s *Stats) IncrementModule(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.ModuleUsage[name]++
	s.data.UpdatedAt = time.Now().Format(time.RFC3339)
	_ = s.flush()
}

// DailyRemaining returns how many more emails can be sent today.
func (s *Stats) DailyRemaining(limit int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	today := time.Now().Format("2006-01-02")
	if s.data.DailyRequestsDate != today {
		return limit
	}
	return limit - s.data.DailyRequestsSent
}

// DailyLimitReached returns true if the daily send limit has been reached.
func (s *Stats) DailyLimitReached(limit int) bool {
	return s.DailyRemaining(limit) <= 0
}

// resetDailyIfNeeded resets the daily counter if the date has changed.
func (s *Stats) resetDailyIfNeeded() {
	today := time.Now().Format("2006-01-02")
	if s.data.DailyRequestsDate != today {
		s.data.DailyRequestsSent = 0
		s.data.DailyRequestsDate = today
	}
}

func (s *Stats) flush() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0644)
}
