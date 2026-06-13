package sentlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SentEntry records a sent information request.
type SentEntry struct {
	MessageID       string `json:"messageId"`
	ChatID          int64  `json:"chatId"`
	UserID          int64  `json:"userId"`
	RecipientName   string `json:"recipientName"`
	RecipientEmail  string `json:"recipientEmail"`
	Subject         string `json:"subject"`
	Date            string `json:"date"`
	Delivered       bool   `json:"delivered,omitempty"`
	Status          string `json:"status,omitempty"`
	ReplyReceivedAt string `json:"replyReceivedAt,omitempty"`
}

// SentLog manages the log of sent requests.
type SentLog struct {
	path     string
	mu       sync.RWMutex
	cache    []SentEntry
	lastLoad time.Time
}

// New creates a new SentLog.
func New(dir string) (*SentLog, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "_sent_log.json")
	sl := &SentLog{path: path}

	raw, err := os.ReadFile(path)
	if err != nil {
		sl.cache = []SentEntry{}
		sl.lastLoad = time.Now()
		return sl, nil
	}
	if err := json.Unmarshal(raw, &sl.cache); err != nil {
		sl.cache = []SentEntry{}
	}
	sl.lastLoad = time.Now()
	return sl, nil
}

// reloadIfNeeded checks if the file was modified since last load and reloads.
// Must be called with at least a read lock; upgrades to write lock if reload needed.
func (sl *SentLog) reloadIfNeeded() {
	stat, err := os.Stat(sl.path)
	if err != nil {
		return
	}
	// If file was modified after our last load, reload it
	if stat.ModTime().After(sl.lastLoad) {
		// Upgrade: we need to release read lock and acquire write lock
		// This is called from methods that already hold the lock,
		// so we do the reload directly (caller must handle locking properly)
		raw, readErr := os.ReadFile(sl.path)
		if readErr != nil {
			return
		}
		var entries []SentEntry
		if unmarshalErr := json.Unmarshal(raw, &entries); unmarshalErr != nil {
			return
		}
		// Only replace if file has more or different data
		if len(entries) >= len(sl.cache) {
			sl.cache = entries
			sl.lastLoad = time.Now()
		}
	}
}

// Append adds a new entry.
func (sl *SentLog) Append(entry SentEntry) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	sl.cache = append(sl.cache, entry)
	sl.lastLoad = time.Now()
	return sl.flush()
}

// FindByMessageID finds an entry by its Message-ID header.
func (sl *SentLog) FindByMessageID(messageID string) *SentEntry {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	sl.reloadIfNeeded()

	normalized := normalizeMessageID(messageID)
	for i := range sl.cache {
		if normalizeMessageID(sl.cache[i].MessageID) == normalized {
			return &sl.cache[i]
		}
	}
	return nil
}

// MarkDelivered marks an entry as delivered.
func (sl *SentLog) MarkDelivered(messageID string) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	normalized := normalizeMessageID(messageID)
	for i := range sl.cache {
		if normalizeMessageID(sl.cache[i].MessageID) == normalized {
			sl.cache[i].Delivered = true
			return sl.flush()
		}
	}
	return nil
}

// MarkReplied marks an entry as having received a reply.
func (sl *SentLog) MarkReplied(messageID string) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	normalized := normalizeMessageID(messageID)
	for i := range sl.cache {
		if normalizeMessageID(sl.cache[i].MessageID) == normalized {
			sl.cache[i].Status = "replied"
			sl.cache[i].ReplyReceivedAt = time.Now().Format(time.RFC3339)
			return sl.flush()
		}
	}
	return nil
}

// MarkExpired marks an entry's deadline as expired (so we don't re-notify).
func (sl *SentLog) MarkExpired(messageID string) error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	normalized := normalizeMessageID(messageID)
	for i := range sl.cache {
		if normalizeMessageID(sl.cache[i].MessageID) == normalized {
			sl.cache[i].Status = "expired"
			return sl.flush()
		}
	}
	return nil
}

// MarkBounced marks the most recent request to the given email as bounced.
func (sl *SentLog) MarkBounced(recipientEmail string) bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	for i := len(sl.cache) - 1; i >= 0; i-- {
		if sl.cache[i].RecipientEmail == recipientEmail && sl.cache[i].ReplyReceivedAt == "" {
			sl.cache[i].Status = "bounced"
			sl.cache[i].ReplyReceivedAt = nowISO()
			_ = sl.flush()
			return true
		}
	}
	return false
}

// ListByUser returns all entries for a given user.
func (sl *SentLog) ListByUser(userID int64) []SentEntry {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	sl.reloadIfNeeded()

	result := []SentEntry{}
	for _, e := range sl.cache {
		if e.UserID == userID {
			result = append(result, e)
		}
	}
	return result
}

// ListAll returns all entries.
func (sl *SentLog) ListAll() []SentEntry {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	sl.reloadIfNeeded()

	result := make([]SentEntry, len(sl.cache))
	copy(result, sl.cache)
	return result
}

// Close flushes data to disk.
func (sl *SentLog) Close() error {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// Reload before flushing to avoid overwriting newer data on disk
	sl.reloadIfNeeded()

	return sl.flush()
}

func (sl *SentLog) flush() error {
	raw, err := json.MarshalIndent(sl.cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sl.path, raw, 0644)
}

func normalizeMessageID(id string) string {
	result := id
	if len(result) > 0 && result[0] == '<' {
		result = result[1:]
	}
	if len(result) > 0 && result[len(result)-1] == '>' {
		result = result[:len(result)-1]
	}
	return result
}

func nowISO() string {
	return time.Now().Format(time.RFC3339)
}

