package moderation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// Статуси запиту в черзі модерації.
const (
	StatusPending  = "pending"  // чекає рішення власника
	StatusClaimed  = "claimed"  // власник натиснув кнопку, відправка в процесі
	StatusApproved = "approved" // надіслано (рішення ✅)
	StatusRejected = "rejected" // відхилено (рішення ❌)
)

// Канали відправки.
const (
	ChannelDostup = "dostup" // портал «Доступ до правди» (спільний акаунт)
	ChannelEmail  = "email"  // email-канал (спільна скринька)
)

// Item — запит, поставлений на перевірку власником.
// Містить УСЕ для відправки після ✅: слаг розпорядителя і готовий
// текст листа для порталу; адресу/тему/текст для email-каналу.
type Item struct {
	ID        string `json:"id"` // 8 hex-символів (влазить у data кнопки)
	UserID    int64  `json:"userId"`
	ChatID    int64  `json:"chatId"`
	TGName    string `json:"tgName,omitempty"`     // ім'я з Telegram на момент подачі
	TGUser    string `json:"tgUsername,omitempty"` // @username (без @)
	Signature string `json:"signature"`            // підпис у листі
	Channel   string `json:"channel"`              // dostup | email
	Slug      string `json:"slug,omitempty"`       // канал dostup: розпорядитель
	Organ     string `json:"organ"`
	Title     string `json:"title"`
	Body      string `json:"body"` // готовий текст листа (з підписом)

	// Канал email: адреса отримувача, тема (з префіксом), reply-to/cc.
	RecipientEmail string `json:"recipientEmail,omitempty"`
	MailSubject    string `json:"mailSubject,omitempty"`
	ReplyTo        string `json:"replyTo,omitempty"`
	CC             string `json:"cc,omitempty"`

	Reasons   []string  `json:"reasons"`
	CreatedAt time.Time `json:"createdAt"`
	Status    string    `json:"status"`
	DecidedAt time.Time `json:"decidedAt,omitempty"`
	ResultURL string    `json:"resultUrl,omitempty"` // публічне посилання (після ✅)
}

// Store — файлбекнеда черга модерації (moderation_queue.json у
// каталозі сесій). Порожній шлях — тільки пам'ять (тести).
// Кожна мутація write-through: рестарт не втрачає чергу.
type Store struct {
	mu   sync.Mutex
	path string
	list []Item
}

// NewStore створює чергу, завантажує файл і чистить старі рішення.
func NewStore(path string) *Store {
	s := &Store{path: path}
	s.load()
	s.pruneDecided(30 * 24 * time.Hour)
	return s
}

func (s *Store) load() {
	if s.path == "" {
		return
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var list []Item
	if err := json.Unmarshal(raw, &list); err != nil {
		log.Printf("[MODERATION] load: %v", err)
		return
	}
	s.list = list
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	raw, err := json.MarshalIndent(s.list, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(s.path, raw, 0600); err != nil {
		log.Printf("[MODERATION] save: %v", err)
	}
}

// newID — 8 випадкових hex-символів (15 байтів у data кнопки —
// обмеження Telegram 64 байти дотримано з запасом).
func newID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return time.Now().Format("0102150405") // фолбэк: час з точністю до секунди
	}
	return hex.EncodeToString(b)
}

// Enqueue ставить запит у чергу і повертає його з присвоєним ID.
func (s *Store) Enqueue(it Item) Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	it.ID = newID()
	it.Status = StatusPending
	it.CreatedAt = time.Now()
	s.list = append(s.list, it)
	s.saveLocked()
	return it
}

// Pending — запити, що чекають рішення (найновіші зверху).
func (s *Store) Pending() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Item
	for i := len(s.list) - 1; i >= 0; i-- {
		if s.list[i].Status == StatusPending {
			out = append(out, s.list[i])
		}
	}
	return out
}

// PendingCount — кількість запитів на очікуванні (для /moderation).
func (s *Store) PendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, it := range s.list {
		if it.Status == StatusPending {
			n++
		}
	}
	return n
}

// Claim атомарно забирає запит з очікування у «обробляється»:
// перший клік по кнопці перемагає, повторні кліки (і гонки двох
// вкладень) отримують false. Повертає копію.
func (s *Store) Claim(id string) (Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id && s.list[i].Status == StatusPending {
			s.list[i].Status = StatusClaimed
			it := s.list[i]
			s.saveLocked()
			return it, true
		}
	}
	return Item{}, false
}

// Release повертає запит у очікування (відправка не вдалася —
// власник зможе натиснути ✅ ще раз).
func (s *Store) Release(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id && s.list[i].Status == StatusClaimed {
			s.list[i].Status = StatusPending
			s.saveLocked()
			return
		}
	}
}

// SetStatus фіксує фінальне рішення (approved/rejected) з часом
// і, для approved, публічним посиланням на запит.
func (s *Store) SetStatus(id, status, resultURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i].Status = status
			s.list[i].DecidedAt = time.Now()
			s.list[i].ResultURL = resultURL
			s.saveLocked()
			return
		}
	}
}

// pruneDecided видаляє вирішені запити старші maxAge (аудит-слід
// тримаємо місяць); pending не чіпаємо ніколи — рішення чекають.
func (s *Store) pruneDecided(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := time.Now().Add(-maxAge)
	kept := s.list[:0]
	for _, it := range s.list {
		if it.Status == StatusPending || it.Status == StatusClaimed || it.DecidedAt.After(cut) {
			kept = append(kept, it)
		}
	}
	if len(kept) != len(s.list) {
		s.list = kept
		s.saveLocked()
	}
}
