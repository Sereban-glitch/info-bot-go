package directory

import (
	"strings"
	"sync"
)

// Recipient represents a government body that can receive information requests.
type Recipient struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Category string `json:"category"`
}

// Directory holds the full list of recipients with search capabilities.
type Directory struct {
	recipients     []Recipient
	byID          map[string]*Recipient
	categoryLabels map[string]string
	mu            sync.RWMutex
}

// New creates a new Directory with the given recipients.
func New(recipients []Recipient) *Directory {
	d := &Directory{
		recipients:     recipients,
		byID:          make(map[string]*Recipient),
		categoryLabels: map[string]string{
			"central":    "🏛 Центральні органи",
			"law":        "⚖️ Правоохоронні / контрольні",
			"kyiv":       "🏙 Київ",
			"courts":     "⚖️ Суди",
			"zaporizhia": "🌻 Запоріжжя",
			"other":      "📋 Інше",
		},
	}
	for i := range recipients {
		d.byID[recipients[i].ID] = &d.recipients[i]
	}
	return d
}

// All returns all recipients from the built-in directory.
func All() *Directory {
	return New(DefaultRecipients)
}

// AllRecipients returns all recipients.
func (d *Directory) AllRecipients() []Recipient {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]Recipient, len(d.recipients))
	copy(result, d.recipients)
	return result
}

// FindByID finds a recipient by its ID.
func (d *Directory) FindByID(id string) *Recipient {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.byID[id]
}

// Search searches recipients by name or email.
func (d *Directory) Search(query string) []Recipient {
	d.mu.RLock()
	defer d.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	// 1. Check synonyms
	if id, ok := synonyms[q]; ok {
		if r := d.byID[id]; r != nil {
			return []Recipient{*r}
		}
	}

	// 2. Score-based search
	queryWords := strings.Fields(q)
	var filtered []string
	for _, w := range queryWords {
		if len(w) > 2 {
			filtered = append(filtered, w)
		}
	}

	if len(filtered) > 0 {
		type scored struct {
			r     Recipient
			score int
		}
		var results []scored
		for _, r := range d.recipients {
			name := strings.ToLower(r.Name)
			s := 0
			for _, w := range filtered {
				root := w
				if len(root) > 5 {
					root = root[:5]
				}
				if strings.Contains(name, root) {
					s++
				}
			}
			if s > 0 {
				results = append(results, scored{r, s})
			}
		}
		// Sort by score descending
		for i := 0; i < len(results); i++ {
			for j := i + 1; j < len(results); j++ {
				if results[j].score > results[i].score {
					results[i], results[j] = results[j], results[i]
				}
			}
		}
		var out []Recipient
		for _, r := range results {
			out = append(out, r.r)
		}
		return out
	}

	// 3. Fallback: contains match
	if len(q) > 4 {
		var out []Recipient
		for _, r := range d.recipients {
			if strings.Contains(strings.ToLower(r.Name), q) {
				out = append(out, r)
			}
		}
		return out
	}

	return nil
}

// ByCategory returns recipients in a given category.
func (d *Directory) ByCategory(cat string) []Recipient {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var result []Recipient
	for _, r := range d.recipients {
		if r.Category == cat {
			result = append(result, r)
		}
	}
	return result
}

// CategoryLabels returns the category label map.
func (d *Directory) CategoryLabels() map[string]string {
	return d.categoryLabels
}

// CategoryKeys returns ordered category keys.
func (d *Directory) CategoryKeys() []string {
	return []string{"central", "law", "kyiv", "courts", "zaporizhia", "other"}
}

// Synonym map for common abbreviations
var synonyms = map[string]string{
	"кму":                "kmu",
	"уряд":               "kmu",
	"кабмін":             "kmu",
	"рада":               "rada",
	"верховна рада":      "rada",
	"парламент":          "rada",
	"вру":                "rada",
	"оп":                 "op",
	"офіс президента":    "op",
	"президент":          "op",
	"мін'юст":            "minjust",
	"мінюст":             "minjust",
	"юстиція":            "minjust",
	"мінфін":             "minfin",
	"фінанси":            "minfin",
	"мінекономіки":       "minekonomiky",
	"моз":                "moz",
	"мінохоронздоров":    "moz",
	"мінцифра":           "mintsyfra",
	"дія":                "mintsyfra",
	"міноборони":         "minoborony",
	"зсу":                "minoborony",
	"омбудсмен":          "ombudsman",
	"права людини":       "ombudsman",
	"огп":                "gpu",
	"генпрокурор":        "gpu",
	"генпрокуратура":     "gpu",
	"прокуратура":        "gpu",
	"мвс":                "mvs",
	"поліція":            "npu",
	"нацполіція":         "npu",
	"набу":                "nabu",
	"назк":               "nazk",
	"сбу":                "sbu",
	"дбр":                "dbr",
	"кмда":               "kmda",
	"кмр":                "kmr",
	"київрада":           "kmr",
}
