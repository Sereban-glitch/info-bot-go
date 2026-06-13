package directory

import (
	"strings"
	"sync"
)

type Recipient struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Category string `json:"category"`
}

type Directory struct {
	recipients     []Recipient
	byID           map[string]*Recipient
	categoryLabels map[string]string
	mu             sync.RWMutex
}

func New(recipients []Recipient) *Directory {
	d := &Directory{
		recipients: make([]Recipient, 0, len(recipients)),
		byID:       make(map[string]*Recipient, len(recipients)),
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
		r := &Recipient{
			ID:       recipients[i].ID,
			Name:     recipients[i].Name,
			Email:    recipients[i].Email,
			Category: recipients[i].Category,
		}
		d.recipients = append(d.recipients, *r)
		d.byID[recipients[i].ID] = r
	}
	return d
}

func All() *Directory {
	return New(DefaultRecipients)
}

func (d *Directory) AllRecipients() []Recipient {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]Recipient, len(d.recipients))
	copy(result, d.recipients)
	return result
}

func (d *Directory) FindByID(id string) *Recipient {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.byID[id]
}

func (d *Directory) Search(query string) []Recipient {
	d.mu.RLock()
	defer d.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	if id, ok := synonyms[q]; ok {
		if r := d.byID[id]; r != nil {
			return []Recipient{*r}
		}
	}

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

func (d *Directory) CategoryLabels() map[string]string {
	return d.categoryLabels
}

func (d *Directory) CategoryKeys() []string {
	return []string{"central", "law", "kyiv", "courts", "zaporizhia", "other"}
}

var synonyms = map[string]string{
	"кму":             "kmu",
	"уряд":            "kmu",
	"кабмін":          "kmu",
	"рада":            "rada",
	"верховна рада":   "rada",
	"парламент":       "rada",
	"вру":             "rada",
	"оп":              "op",
	"офіс президента": "op",
	"президент":       "op",
	"мін'юст":         "minjust",
	"мінюст":          "minjust",
	"юстиція":         "minjust",
	"мінфін":          "minfin",
	"фінанси":         "minfin",
	"мінекономіки":    "minekonomiky",
	"моз":             "moz",
	"мінохоронздоров": "moz",
	"мінцифра":        "mintsyfra",
	"дія":             "mintsyfra",
	"міноборони":      "minoborony",
	"зсу":             "minoborony",
	"омбудсмен":       "ombudsman",
	"права людини":    "ombudsman",
	"огп":             "gpu",
	"генпрокурор":     "gpu",
	"генпрокуратура":  "gpu",
	"прокуратура":     "gpu",
	"мвс":             "mvs",
	"поліція":         "npu",
	"нацполіція":      "npu",
	"набу":            "nabu",
	"назк":            "nazk",
	"сбу":             "sbu",
	"дбр":             "dbr",
	"кмда":            "kmda",
	"кмр":             "kmr",
	"київрада":        "kmr",
}
