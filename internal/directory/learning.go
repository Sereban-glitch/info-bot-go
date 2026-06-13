package directory

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const learnedFileName = "learned_agencies.json"

type learnedEntry struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type learnedData struct {
	Agencies map[string]learnedEntry `json:"agencies"`
}

var fileMu sync.Mutex

func (d *Directory) LoadLearned(sessionDir string) {
	if sessionDir == "" {
		return
	}
	p := filepath.Join(sessionDir, learnedFileName)
	data, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[DIR] Error reading learned agencies: %v", err)
		}
		return
	}

	var ld learnedData
	if err := json.Unmarshal(data, &ld); err != nil {
		log.Printf("[DIR] Error parsing learned agencies: %v", err)
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for id, entry := range ld.Agencies {
		if _, exists := d.byID[id]; exists {
			continue
		}
		r := &Recipient{
			ID:       id,
			Name:     entry.Name,
			Email:    entry.Email,
			Category: "learned",
		}
		d.recipients = append(d.recipients, *r)
		d.byID[id] = r
	}

	log.Printf("[DIR] Loaded %d learned agencies from %s", len(ld.Agencies), p)
}

func (d *Directory) saveLearned(sessionDir string) {
	if sessionDir == "" {
		return
	}

	d.mu.RLock()
	ld := learnedData{Agencies: make(map[string]learnedEntry)}
	for id, r := range d.byID {
		if r.Category == "learned" {
			ld.Agencies[id] = learnedEntry{Name: r.Name, Email: r.Email}
		}
	}
	d.mu.RUnlock()

	if len(ld.Agencies) == 0 {
		return
	}

	data, err := json.MarshalIndent(ld, "", "  ")
	if err != nil {
		log.Printf("[DIR] Error marshaling learned agencies: %v", err)
		return
	}

	p := filepath.Join(sessionDir, learnedFileName)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		log.Printf("[DIR] Error creating session dir: %v", err)
		return
	}

	fileMu.Lock()
	defer fileMu.Unlock()

	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Printf("[DIR] Error writing temp learned agencies: %v", err)
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		log.Printf("[DIR] Error replacing learned agencies: %v", err)
		os.Remove(tmp)
		return
	}

	log.Printf("[DIR] Saved %d learned agencies to %s", len(ld.Agencies), p)
}

func (d *Directory) AddLearned(sessionDir, name, email string) string {
	if name == "" || email == "" {
		return ""
	}

	id := generateLearnedID(name)

	d.mu.Lock()
	if _, exists := d.byID[id]; exists {
		existing := d.byID[id]
		if existing.Email != email {
			existing.Email = email
		}
		d.mu.Unlock()
		d.saveLearned(sessionDir)
		return id
	}

	r := &Recipient{
		ID:       id,
		Name:     name,
		Email:    email,
		Category: "learned",
	}
	d.recipients = append(d.recipients, *r)
	d.byID[id] = r
	d.mu.Unlock()

	d.saveLearned(sessionDir)
	return id
}

func (d *Directory) CheckLearnedEmail(name string) string {
	if name == "" {
		return ""
	}
	id := generateLearnedID(name)
	d.mu.RLock()
	defer d.mu.RUnlock()
	if r, ok := d.byID[id]; ok {
		return r.Email
	}
	return ""
}

func (d *Directory) SearchLearned(query string) []Recipient {
	d.mu.RLock()
	defer d.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	var result []Recipient
	for _, r := range d.recipients {
		if r.Category != "learned" {
			continue
		}
		if strings.Contains(strings.ToLower(r.Name), q) {
			result = append(result, r)
		}
	}
	return result
}

func (d *Directory) LearnedCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	count := 0
	for _, r := range d.recipients {
		if r.Category == "learned" {
			count++
		}
	}
	return count
}

func generateLearnedID(name string) string {
	h := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(name))))
	return fmt.Sprintf("lrn_%x", h[:4])
}
