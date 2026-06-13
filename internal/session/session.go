package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	RecipientName    string `json:"recipientName,omitempty"`
	RecipientEmail   string `json:"recipientEmail,omitempty"`
	Subject          string `json:"subject,omitempty"`
	Body             string `json:"body,omitempty"`
	UseSharedMailbox bool   `json:"useSharedMailbox,omitempty"`
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

// SessionData is the per-user session.
type SessionData struct {
	Step    string         `json:"step"`
	Profile Profile        `json:"profile"`
	Draft   Draft          `json:"draft"`
	PRDraft *PRDraft       `json:"prDraft,omitempty"`
	History []HistoryEntry `json:"history,omitempty"`
}

// NewSessionData returns a blank session.
func NewSessionData() *SessionData {
	return &SessionData{
		Step:    "idle",
		Profile: Profile{},
		Draft:   Draft{},
		PRDraft: nil,
		History: nil,
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

// FileStore implements file-based session storage.
type FileStore struct {
	dir   string
	mu    sync.RWMutex
	cache map[string]*SessionData
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
	return os.WriteFile(path, raw, 0644)
}

// SessionKey generates a session key from a Telegram user ID.
func SessionKey(userID int64) string {
	return fmt.Sprintf("user-%d", userID)
}
