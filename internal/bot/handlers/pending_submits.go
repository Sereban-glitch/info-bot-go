package handlers

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// PendingSubmit — зафиксированная попытка отправки запроса на портал,
// результат которой не удалось подтвердить (SubmitRequest вернул
// ErrInvalidResponse: финальный POST ушёл, но подтверждение не распарсилось).
//
// Реальный случай (30.08.2026): запрос пользователя БЫЛ создан на портале,
// но бот показал «Помилку надсилання», а фоновая синхронизация, найдя
// «чужой» запрос, приписала его владельцу. Журнал заявок закрывает обе
// проблемы: синхронизация сначала ищет автора среди недавних попыток
// и приписывает запрос ему (плюс отправляет пользователю сообщение об успехе).
type PendingSubmit struct {
	UserID int64     `json:"userId"`
	ChatID int64     `json:"chatId"`
	Title  string    `json:"title"`
	Organ  string    `json:"organ"`
	At     time.Time `json:"at"`
}

// PendingSubmits — файлбэкнутое хранилище попыток (pending_submits.json
// в каталоге сессий). Пустой путь — только память (для тестов).
type PendingSubmits struct {
	mu   sync.Mutex
	path string
	list []PendingSubmit
}

// NewPendingSubmits создаёт хранилище и загружает файл.
func NewPendingSubmits(path string) *PendingSubmits {
	p := &PendingSubmits{path: path}
	p.load()
	p.pruneOlderThan(24 * time.Hour)
	return p
}

func (p *PendingSubmits) load() {
	if p.path == "" {
		return
	}
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	var list []PendingSubmit
	if err := json.Unmarshal(raw, &list); err != nil {
		return
	}
	p.list = list
}

func (p *PendingSubmits) saveLocked() {
	if p.path == "" {
		return
	}
	raw, err := json.MarshalIndent(p.list, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(p.path, raw, 0600); err != nil {
		log.Printf("[PENDING] save: %v", err)
	}
}

// Add фиксирует попытку отправки.
func (p *PendingSubmits) Add(ps PendingSubmit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list = append(p.list, ps)
	p.saveLocked()
}

// TakeMatching ищет попытку, чья тема совпадает с темой обнаруженного
// на портале запроса (по первым 30 байтам в нижнем регистре — тот же
// принцип, что и страховка в handleSubmit), свежее maxAge. Найденная
// попытка ИЗЫМАЕТСЯ из хранилища (запрос обрабатывается один раз).
// Возвращается копия; nil — совпадения нет.
func (p *PendingSubmits) TakeMatching(title string, maxAge time.Duration) *PendingSubmit {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for i := range p.list {
		if pendingMatchesByTitle(p.list[i].Title, title) && now.Sub(p.list[i].At) <= maxAge {
			ps := p.list[i]
			p.list = append(p.list[:i], p.list[i+1:]...)
			p.saveLocked()
			return &ps
		}
	}
	return nil
}

// pruneOlderThan удаляет протухшие попытки (их запрос уже либо найден,
// либо не создан вовсе).
func (p *PendingSubmits) pruneOlderThan(maxAge time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cut := time.Now().Add(-maxAge)
	kept := p.list[:0]
	for _, ps := range p.list {
		if ps.At.After(cut) {
			kept = append(kept, ps)
		}
	}
	if len(kept) != len(p.list) {
		p.list = kept
		p.saveLocked()
	}
}

// pendingMatchesByTitle — совпадает ли тема попытки с темой запроса портала.
// Портал обрезает длинные темы, поэтому сравниваем вхождение первых 30 байт
// темы попытки в тему запроса (оба в нижнем регистре) — как в существующей
// страховке от чужого адреса.
func pendingMatchesByTitle(pendingTitle, portalTitle string) bool {
	lp := strings.ToLower(strings.TrimSpace(pendingTitle))
	if lp == "" {
		return false
	}
	if len(lp) > 30 {
		lp = lp[:30]
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(portalTitle)), lp)
}
