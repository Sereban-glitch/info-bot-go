package dostup

// Локальный каталог распорядителей порталу «Доступ до правди».
//
// Портал отдаёт каталог постранично (https://dostup.org.ua/body/list/<region>),
// но для мгновенного поиска и кнопок-разделов бот держит локальную копию
// (dostup_catalog.json), которую фоново обновляет DostupSync
// (refreshCatalogIfNeeded). Выбор органа запоминается в dostup_bindings.json —
// в следующий раз запрос к знакомому органу привязывается автоматически.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CatalogBody — запись каталога (совместима с dostup_catalog.json).
type CatalogBody struct {
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Region string `json:"region,omitempty"`
}

// Catalog — файл dostup_catalog.json.
type Catalog struct {
	Version  int           `json:"version"`
	SyncedAt string        `json:"syncedAt"`
	Bodies   []CatalogBody `json:"bodies"`
}

// Binding — привязка «название → слаг» (dostup_bindings.json).
type Binding struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Source  string `json:"source,omitempty"`
	BoundAt string `json:"boundAt,omitempty"`
}

// CatalogStore — потокобезопасное хранилище каталога и привязок.
type CatalogStore struct {
	mu       sync.RWMutex
	path     string
	bindPath string
	catalog  *Catalog
	bindings map[string]Binding // key: strings.ToLower(name)
}

// NewCatalogStore создаёт хранилище; path — файл каталога.
// Привязки лежат рядом в dostup_bindings.json (имя файла совместимо
// с прод-версией: существующие привязки подхватываются автоматически).
func NewCatalogStore(path string) *CatalogStore {
	return &CatalogStore{
		path:     path,
		bindPath: filepath.Join(filepath.Dir(path), "dostup_bindings.json"),
		bindings: make(map[string]Binding),
	}
}

// Load читает каталог и привязки с диска (если файлов нет — тихо пропускает).
func (s *CatalogStore) Load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if raw, err := os.ReadFile(s.path); err == nil {
		var c Catalog
		if err := json.Unmarshal(raw, &c); err == nil && len(c.Bodies) > 0 {
			s.catalog = &c
			log.Printf("[CATALOG] загружен из %s: %d органов, синхронизирован %s", s.path, len(c.Bodies), c.SyncedAt)
		}
	}
	if raw, err := os.ReadFile(s.bindPath); err == nil {
		var b map[string]Binding
		if err := json.Unmarshal(raw, &b); err == nil {
			s.bindings = b
		}
	}
}

// Get возвращает текущий каталог (может быть nil).
func (s *CatalogStore) Get() *Catalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.catalog
}

// Count — количество органов в каталоге.
func (s *CatalogStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return 0
	}
	return len(s.catalog.Bodies)
}

// SyncedAt — время последней синхронизации каталога.
func (s *CatalogStore) SyncedAt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return ""
	}
	return s.catalog.SyncedAt
}

// Replace обновляет каталог, если новый не подозрительно мал, и сохраняет на диск.
// Возвращает true, если каталог был заменён.
func (s *CatalogStore) Replace(newC *Catalog) bool {
	if newC == nil || len(newC.Bodies) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Защита от битой выгрузки: не заменяем полный каталог куцым.
	if s.catalog != nil && len(newC.Bodies) < len(s.catalog.Bodies)/3 {
		log.Printf("dostup: каталог подозрительно мал (%d органов), не заменяю", len(newC.Bodies))
		return false
	}

	newC.Version = 1
	if newC.SyncedAt == "" {
		newC.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.catalog = newC

	raw, err := json.MarshalIndent(newC, "", " ")
	if err == nil {
		_ = atomicWrite(s.path, raw)
	}
	return true
}

// RememberBinding сохраняет привязку «название → слаг» (source: catalog-pick и т.п.).
func (s *CatalogStore) RememberBinding(name, slug, source string) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" || slug == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.bindings[key]; ok && b.Slug == slug {
		return
	}
	s.bindings[key] = Binding{
		Slug:    slug,
		Name:    name,
		Source:  source,
		BoundAt: time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(s.bindings, "", " ")
	if err == nil {
		_ = atomicWrite(s.bindPath, raw)
	}
}

// LookupBinding ищет слаг по названию (точное совпадение, без учёта регистра).
func (s *CatalogStore) LookupBinding(name string) (Binding, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bindings[key]
	return b, ok
}

// FindBySlug возвращает орган каталога по слагу.
func (s *CatalogStore) FindBySlug(slug string) (CatalogBody, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return CatalogBody{}, false
	}
	for _, b := range s.catalog.Bodies {
		if b.Slug == slug {
			return b, true
		}
	}
	return CatalogBody{}, false
}

// ---------------------------------------------------------------------------
// Разделы каталога (кнопки «Каталог»)
// ---------------------------------------------------------------------------

// CatalogCategory — раздел каталога для кнопок.
type CatalogCategory struct {
	ID    string
	Title string
}

// Categories — фиксированные разделы каталога.
//
//	central — органы без региона (region == "");
//	region:XXX — областные органы по коду региона;
//	all — весь каталог по алфавиту.
func Categories() []CatalogCategory {
	return []CatalogCategory{
		{ID: "central", Title: "🏛 Центральні органи"},
		{ID: "courts", Title: "⚖️ Суди"},
		{ID: "law", Title: "👮 Правоохоронні / контрольні"},
		{ID: "all", Title: "📚 Всі органи (за алфавітом)"},
		{ID: "manual", Title: "🆕 Інший орган (ввести вручну)"},
	}
}

// Regions — коды регионов портала с человеческими названиями.
func Regions() map[string]string {
	return map[string]string{
		"kyiv":      "м. Київ",
		"odesa":     "Одеська область",
		"lviv":      "Львівська область",
		"dnipro":    "Дніпропетровська область",
		"tern":      "Тернопільська область",
		"lug":       "Луганська область",
		"ifr":       "Івано-Франківська область",
		"khmel":     "Хмельницька область",
		"rivne":     "Рівненська область",
		"khers":     "Херсонська область",
		"zakarp":    "Закарпатська область",
		"vinnytsya": "Вінницька область",
		"don":       "Донецька область",
		"zhyt":      "Житомирська область",
		"volyn":     "Волинська область",
		"chern":     "Чернівецька область",
		"khark":     "Харківська область",
		"cherk":     "Черкаська область",
		"chernihiv": "Чернігівська область",
		"zap":       "Запорізька область",
		"sumy":      "Сумська область",
		"poltava":   "Полтавська область",
		"myk":       "Миколаївська область",
		"krop":      "Кіровоградська область",
		"krym":      "Автономна Республіка Крим",
	}
}

// RegionCodes — упорядоченные коды регионов.
func RegionCodes() []string {
	codes := make([]string, 0, len(Regions()))
	for code := range Regions() {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// IsCentralName — эвристика центрального органа (для каталога без региона).
func IsCentralName(name string) bool {
	n := strings.ToLower(name)
	centralMarkers := []string{
		"міністерств", "національне агентств", "національна поліція",
		"державна служба", "державне агентств", "офіс генерального прокурора",
		"державне бюро розслідувань", "казначейська служба", "офіс президента",
		"кабінет міністрів", "-verховна рада", "верховна рада",
		"уповноважений", "рахункова палата", "антикорупційне",
		"національне антикорупційне", "центральна",
	}
	for _, m := range centralMarkers {
		if strings.Contains(n, m) {
			return true
		}
	}
	return false
}

// Browse — выборка органов раздела с пагинацией.
// section: central|all|region:<code>; page — с нуля; perPage — размер страницы.
// Возвращает органы страницы, всего в разделе.
func (s *CatalogStore) Browse(section string, page, perPage int) ([]CatalogBody, int) {
	if perPage <= 0 {
		perPage = 8
	}
	if page < 0 {
		page = 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return nil, 0
	}

	var list []CatalogBody
	switch {
	case section == "central":
		for _, b := range s.catalog.Bodies {
			if b.Region == "" {
				list = append(list, b)
			}
		}
	case section == "all":
		list = append(list, s.catalog.Bodies...)
	case strings.HasPrefix(section, "region:"):
		code := strings.TrimPrefix(section, "region:")
		for _, b := range s.catalog.Bodies {
			if b.Region == code {
				list = append(list, b)
			}
		}
	default:
		return nil, 0
	}

	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	total := len(list)
	start := page * perPage
	if start >= total {
		return nil, total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return list[start:end], total
}

// SearchLocal — локальный поиск по каталогу (без запроса к порталу).
// Возвращает до limit совпадений (подстрока в названии, без учёта регистра).
func (s *CatalogStore) SearchLocal(query string, limit int) []CatalogBody {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return nil
	}

	var exact []CatalogBody
	var partial []CatalogBody
	qRunes := []rune(q)
	for _, b := range s.catalog.Bodies {
		n := strings.ToLower(b.Name)
		if strings.Contains(n, q) {
			if strings.HasPrefix(n, q) || len(exact) < limit {
				exact = append(exact, b)
			}
			if len(exact) >= limit {
				break
			}
			continue
		}
		// все слова запроса встречаются в названии
		words := strings.Fields(q)
		if len(words) > 1 && len(partial) < limit {
			all := true
			for _, w := range words {
				if len([]rune(w)) < 3 && len(qRunes) > 6 {
					continue // короткие служебные слова пропускаем
				}
				if !strings.Contains(n, w) {
					all = false
					break
				}
			}
			if all {
				partial = append(partial, b)
			}
		}
	}
	if len(exact) >= limit {
		return exact[:limit]
	}
	out := append(exact, partial...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// atomicWrite — запись во временный файл + rename (нет обрезанного файла).
func atomicWrite(path string, raw []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var _ = fmt.Sprintf
