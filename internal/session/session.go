package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Profile holds user personal data.
type Profile struct {
	FirstName     string `json:"firstName,omitempty"`
	LastName      string `json:"lastName,omitempty"`
	MiddleName    string `json:"middleName,omitempty"`
	PostalAddress string `json:"postalAddress,omitempty"`
	Email         string `json:"email,omitempty"`
	FullName      string `json:"fullName,omitempty"`
}

// Draft holds a request being composed.
type Draft struct {
	RecipientName      string `json:"recipientName,omitempty"`
	RecipientEmail     string `json:"recipientEmail,omitempty"`
	Subject            string `json:"subject,omitempty"`
	Body               string `json:"body,omitempty"`
	UseSharedMailbox   bool   `json:"useSharedMailbox,omitempty"`
	OSINTSuggestedName string `json:"osintSuggestedName,omitempty"`
	DostupSlug         string `json:"dostupSlug,omitempty"` // выбранный распорядитель на dostup.org.ua
}

// PRDraft holds copilot draft.
type PRDraft struct {
	Text        string `json:"text,omitempty"`
	PhotoID     string `json:"photoId,omitempty"`
	Tone        string `json:"tone,omitempty"`
	FinalText   string `json:"finalText,omitempty"`
	IsAnonymous bool   `json:"isAnonymous,omitempty"`
	AIVerdict   string `json:"aiVerdict,omitempty"`
}

// HistoryEntry tracks a sent request.
type HistoryEntry struct {
	Date            string `json:"date"`
	To              string `json:"to"`
	Subject         string `json:"subject"`
	MessageID       string `json:"messageId"`
	ChatID          int64  `json:"chatId,omitempty"`
	ReplyReceivedAt string `json:"replyReceivedAt,omitempty"`
}

// FollowUpDraft — черновик уточнения (follow-up) в гилку существующего запроса.
type FollowUpDraft struct {
	RequestSlug string `json:"requestSlug,omitempty"` // слаг запроса на портале
	Body        string `json:"body,omitempty"`        // текст уточнения
	Subject     string `json:"subject,omitempty"`     // тема исходного запроса (для карточки)
	Organ       string `json:"organ,omitempty"`       // орган
	URL         string `json:"url,omitempty"`         // публичная ссылка на гилку
	PickIdx     int    `json:"pickIdx,omitempty"`     // выбранный индекс в списке гилок
}

// AnalyzeDraft — черновик AI-розбора ответа органа (ТЗ №6 «Розбір відмови»).
// Хранит контекст (орган, тема, гилка портала) и ГОТОВЫЙ документ,
// сгенерированный моделью, — чтобы кнопка «Надіслати у гілку запиту»
// работала и после перезапуска бота.
type AnalyzeDraft struct {
	Organ        string `json:"organ,omitempty"`        // орган-распорядитель
	Subject      string `json:"subject,omitempty"`      // тема исходного запроса
	RequestSlug  string `json:"requestSlug,omitempty"`  // гилка на портале (если ответ оттуда)
	URL          string `json:"url,omitempty"`          // публичная ссылка на гилку
	ReplyText    string `json:"replyText,omitempty"`    // текст ответа органа
	NextStep     string `json:"nextStep,omitempty"`     // clarification|complaint|appeal|none
	DraftSubject string `json:"draftSubject,omitempty"` // тема готового документа
	DraftBody    string `json:"draftBody,omitempty"`    // текст готового документа
}

// SessionData is the per-user session.
type SessionData struct {
	Step     string         `json:"step"`
	Profile  Profile        `json:"profile"`
	Draft    Draft          `json:"draft"`
	PRDraft  *PRDraft       `json:"prDraft,omitempty"`
	History  []HistoryEntry `json:"history,omitempty"`
	FollowUp *FollowUpDraft `json:"followUp,omitempty"` // черновик уточнения в гилку
	Analyze  *AnalyzeDraft  `json:"analyze,omitempty"`  // черновик AI-розбора ответа (ТЗ №6)

	// DostupDisclosureShown — владелец один раз увидел дисклеймер
	// о публичности портала «Доступ до правди» (что публикуется открыто,
	// что маскирует портал). Флаг персистентный: повторно не показываем.
	DostupDisclosureShown bool `json:"dostupDisclosureShown,omitempty"`
}

// NewSessionData returns a blank session.
func NewSessionData() *SessionData {
	return &SessionData{
		Step:     "idle",
		Profile:  Profile{},
		Draft:    Draft{},
		PRDraft:  nil,
		History:  nil,
		FollowUp: nil,
		Analyze:  nil,
	}
}

// ProfileDisplayName returns a displayable name from the profile.
func ProfileDisplayName(p Profile) string {
	if p.LastName != "" || p.FirstName != "" {
		parts := []string{}
		if p.LastName != "" {
			parts = append(parts, p.LastName)
		}
		if p.FirstName != "" {
			parts = append(parts, p.FirstName)
		}
		if p.MiddleName != "" {
			parts = append(parts, p.MiddleName)
		}
		name := ""
		for i, s := range parts {
			if i > 0 {
				name += " "
			}
			name += s
		}
		return name
	}
	return p.FullName
}

// IsProfileReady returns true if the profile has at least a name.
func IsProfileReady(p Profile) bool {
	return (p.FirstName != "" && p.LastName != "") || p.FullName != ""
}

// SignatureName returns the name used to sign request letters.
// Priority: explicit FullName (user may format it as they like,
// e.g. «Іван Петренко» or «І. Петренко»), then parts-based
// «Прізвище Ім'я По-батькові». Returns "" when no name is known —
// callers must ask the user instead of falling back to an account
// or placeholder name: under ст. 19 ЗУ «Про доступ до публічної
// інформації» the requester must be named.
func SignatureName(p Profile) string {
	if fn := strings.TrimSpace(p.FullName); fn != "" {
		return fn
	}
	return strings.TrimSpace(ProfileDisplayName(p))
}

// FollowUpThread — гилка запроса, доступная для уточнений
// (следим за ответами и предлагаем дописать).
type FollowUpThread struct {
	Slug         string `json:"slug"`         // слаг запроса
	Subject      string `json:"subject"`      // тема
	Organ        string `json:"organ"`        // орган
	URL          string `json:"url"`          // публичная ссылка
	LastRemindAt string `json:"lastRemindAt"` // ISO-время последнего напоминания
	RepliedAt    string `json:"repliedAt"`    // ISO-время ответа по существу (если был)
	FollowUpAt   string `json:"followUpAt"`   // ISO-время последнего дописывания
}

// FollowUpThreads — персистентное хранилище гилок по пользователям
// (файл followup_threads.json в каталоге сессий).
type FollowUpThreads struct {
	mu   sync.Mutex
	path string
	data map[int64][]FollowUpThread
}

// NewFollowUpThreads создаёт хранилище (path может быть пустым — только память).
func NewFollowUpThreads(path string) *FollowUpThreads {
	t := &FollowUpThreads{path: path, data: map[int64][]FollowUpThread{}}
	t.load()
	return t
}

func (t *FollowUpThreads) load() {
	if t.path == "" {
		return
	}
	raw, err := os.ReadFile(t.path)
	if err != nil {
		return
	}
	var data map[int64][]FollowUpThread
	if err := json.Unmarshal(raw, &data); err == nil {
		t.data = data
	}
}

func (t *FollowUpThreads) save() {
	if t.path == "" {
		return
	}
	raw, err := json.MarshalIndent(t.data, "", " ")
	if err != nil {
		return
	}
	tmp := t.path + ".tmp"
	if os.WriteFile(tmp, raw, 0600) == nil {
		_ = os.Rename(tmp, t.path)
	}
}

// List — гилки пользователя (свежие сверху, максимум limit).
func (t *FollowUpThreads) List(userID int64, limit int) []FollowUpThread {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.data[userID]
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list
}

// DeleteByUser удаляет все гилки пользователя (ТЗ №5, /delete_my_data).
func (t *FollowUpThreads) DeleteByUser(userID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.data[userID]; !ok {
		return
	}
	delete(t.data, userID)
	t.save()
}

// Upsert добавляет/обновляет гилку (по слагу) наверху списка.
func (t *FollowUpThreads) Upsert(userID int64, th FollowUpThread) {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.data[userID]
	out := []FollowUpThread{th}
	for _, e := range list {
		if e.Slug != th.Slug {
			out = append(out, e)
		}
	}
	if len(out) > 30 {
		out = out[:30]
	}
	t.data[userID] = out
	t.save()
}

// MarkReplied фиксирует ответ по существу в гилке.
func (t *FollowUpThreads) MarkReplied(userID int64, slug, at string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.data[userID]
	for i := range list {
		if list[i].Slug == slug {
			list[i].RepliedAt = at
		}
	}
	t.data[userID] = list
	t.save()
}

// MarkFollowUpSent фиксирует время дописывания в гилку.
func (t *FollowUpThreads) MarkFollowUpSent(userID int64, slug, at string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.data[userID]
	for i := range list {
		if list[i].Slug == slug {
			list[i].FollowUpAt = at
			list[i].LastRemindAt = at // напоминание сбрасываем
		}
	}
	t.data[userID] = list
	t.save()
}

// MarkReminded фиксирует время напоминания «строк минул».
func (t *FollowUpThreads) MarkReminded(userID int64, slug, at string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	list := t.data[userID]
	for i := range list {
		if list[i].Slug == slug {
			list[i].LastRemindAt = at
		}
	}
	t.data[userID] = list
	t.save()
}

// FileStore is a file-based session storage.
//
// ТЗ №5 — целостность при одновременной работе: Get возвращает ОБЩИЙ
// указатель на данные сессии. Если один пользователь шлёт сообщения
// из двух устройств (или бот и мини-приложение работают одновременно),
// два обработчика могут менять одни и те же данные параллельно — вплоть
// до потери апдейтов и «fatal error: concurrent map write» (убивает весь
// процесс, recover не помогает).
//
// Решение — полосовые (striped) блокировки по ключу сессии: LockSession/
// UnlockSession сериализуют всю работу с сессией одного пользователя.
// Полосы (256 штук) распределяют разных пользователей по разным
// блокировкам — чужие друг другу люди не ждут.
type FileStore struct {
	dir   string
	mu    sync.RWMutex
	cache map[string]*SessionData

	stripes [256]sync.Mutex // полосовые блокировки: ключ → полоса по хешу
}

// NewFileStore creates a new file-based session store.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &FileStore{
		dir:   dir,
		cache: make(map[string]*SessionData),
	}, nil
}

// stripeIndex — номер полосы для ключа (FNV-1a, стабильно между запусками).
func (s *FileStore) stripeIndex(key string) int {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % 256)
}

// LockSession блокирует сессию пользователя: пока блокировка держится,
// никакой другой обработчик не может concurrently менять эту же сессию.
// Используется middleware бота и HTTP-обработчиками мини-приложения.
// ОБЯЗАТЕЛЬНО парный UnlockSession (лучше через defer).
func (s *FileStore) LockSession(key string) {
	s.stripes[s.stripeIndex(key)].Lock()
}

// UnlockSession освобождает сессионную блокировку.
func (s *FileStore) UnlockSession(key string) {
	s.stripes[s.stripeIndex(key)].Unlock()
}

// Get returns session data for the given key, loading from file if needed.
func (s *FileStore) Get(key string) (*SessionData, error) {
	s.mu.RLock()
	if data, ok := s.cache[key]; ok {
		s.mu.RUnlock()
		return data, nil
	}
	s.mu.RUnlock()

	// Load from file
	path := filepath.Join(s.dir, key+".json")
	data, err := s.loadData(path)
	if err != nil {
		// Return new empty session
		newData := NewSessionData()
		s.mu.Lock()
		s.cache[key] = newData
		s.mu.Unlock()
		return newData, nil
	}

	s.mu.Lock()
	s.cache[key] = data
	s.mu.Unlock()
	return data, nil
}

// Set saves session data for the given key.
func (s *FileStore) Set(key string, data *SessionData) error {
	s.mu.Lock()
	s.cache[key] = data
	s.mu.Unlock()

	path := filepath.Join(s.dir, key+".json")
	return s.saveData(path, data)
}

// Delete удаляет сессию пользователя полностью (ТЗ №5, /delete_my_data):
// из кэша и с диска. Возвращает true, если файл существовал.
func (s *FileStore) Delete(key string) bool {
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()

	err := os.Remove(filepath.Join(s.dir, key+".json"))
	return err == nil
}

// Close flushes any pending data.
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, data := range s.cache {
		path := filepath.Join(s.dir, key+".json")
		_ = s.saveData(path, data)
	}
	return nil
}

func (s *FileStore) loadData(path string) (*SessionData, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data SessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (s *FileStore) saveData(path string, data *SessionData) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	// 0600: session files contain PII (name, email, postal address).
	return os.WriteFile(path, raw, 0600)
}

// SessionKey generates a session key from a Telegram user ID.
func SessionKey(userID int64) string {
	return fmt.Sprintf("user-%d", userID)
}
