package dostup

// Рейтинги органов порталу («запросов / ответов по существу / просрочено»).
// Источник — фид органа: https://dostup.org.ua/body/<slug>/feed.json →
// public_body.info { requests_count, requests_successful_count, ... }.
//
// Кэш в памяти с TTL: каталог из ~2150 органов не дёргает портал чаще,
// чем раз в час на орган; бейджи рейтинга показываются в карточках
// запросов (веб-дашборд) и как предупреждение при выборе распорядителя.

import (
        "encoding/json"
        "fmt"
        "regexp"
        "strings"
        "sync"
        "time"
)

// BodyStats — статистика ответов органа на портале.
type BodyStats struct {
        Requests       int `json:"requests_count"`
        Successful     int `json:"requests_successful_count"`
        NotHeld        int `json:"requests_not_held_count"`
        Overdue        int `json:"requests_overdue_count"`
        Classified     int `json:"requests_visible_classified_count,omitempty"`
}

// bodyStatsEntry — элемент кэша рейтингов.
type bodyStatsEntry struct {
        stats    BodyStats
        fetched  time.Time
}

const bodyStatsTTL = time.Hour

var (
        bodyStatsMu    sync.Mutex
        bodyStatsCache = map[string]bodyStatsEntry{}
)

// BodyStatsCached — рейтинг органа по слагу с кэшем (TTL 1 час).
// online=false — только кэш, без запроса к порталу.
func (c *Client) BodyStatsCached(slug string, online bool) (*BodyStats, error) {
        if slug == "" {
                return nil, fmt.Errorf("dostup: пустой слаг для статистики")
        }
        bodyStatsMu.Lock()
        if e, ok := bodyStatsCache[slug]; ok && time.Since(e.fetched) < bodyStatsTTL {
                st := e.stats
                bodyStatsMu.Unlock()
                return &st, nil
        }
        bodyStatsMu.Unlock()
        if !online {
                return nil, fmt.Errorf("dostup: нет кэшированной статистики")
        }

        // Фид органа: { ..., "info": { requests_count, ... } }
        page, code, err := c.get("/body/" + slug + "/feed.json")
        if err != nil {
                return nil, err
        }
        if code != 200 {
                return nil, fmt.Errorf("dostup: feed органа: HTTP %d", code)
        }
        var parsed struct {
                PublicBody struct {
                        Info *BodyStats `json:"info"`
                } `json:"public_body"`
                Info *BodyStats `json:"info"` // фид отдаёт info на верхнем уровне
        }
        if err := json.Unmarshal([]byte(page), &parsed); err != nil {
                return nil, fmt.Errorf("dostup: в feed органа нет статистики: %w", err)
        }
        st := parsed.Info
        if st == nil {
                st = parsed.PublicBody.Info
        }
        if st == nil {
                return nil, fmt.Errorf("dostup: в feed органа нет статистики")
        }

        bodyStatsMu.Lock()
        bodyStatsCache[slug] = bodyStatsEntry{stats: *st, fetched: time.Now()}
        bodyStatsMu.Unlock()
        return st, nil
}

// OverduePct — доля просроченных ответов (0..100).
func (s *BodyStats) OverduePct() int {
        if s == nil || s.Requests == 0 {
                return 0
        }
        return s.Overdue * 100 / s.Requests
}

// SyncCatalog выкачивает полный каталог портала постранично.
// Секции: "" (все без региона — центральные), коды регионов портала.
// Возвращает собранный каталог; между страницами — пауза для rate-limit.
func (c *Client) SyncCatalog() (*Catalog, error) {
        cat := &Catalog{}
        seen := map[string]bool{}

        sections := append([]string{""}, RegionCodes()...)
        for _, region := range sections {
                for page := 1; page <= 60; page++ {
                        path := "/body/list/" + region + fmt.Sprintf("?page=%d", page)
                        html, code, err := c.get(path)
                        if err != nil {
                                return nil, fmt.Errorf("dostup: страница каталога %s p%d: %w", region, page, err)
                        }
                        if code != 200 {
                                return nil, fmt.Errorf("dostup: страница каталога %s p%d: HTTP %d", region, page, code)
                        }
                        if isRateLimited(html) {
                                return nil, ErrRateLimited
                        }
                        bodies := parseCatalogPage(html, region)
                        if len(bodies) == 0 {
                                break // раздел закончился
                        }
                        for _, b := range bodies {
                                if !seen[b.Slug] {
                                        seen[b.Slug] = true
                                        cat.Bodies = append(cat.Bodies, b)
                                }
                        }
                        time.Sleep(700 * time.Millisecond) // вежливость к rate-limit портала
                }
        }
        return cat, nil
}

// parseCatalogPage — извлекает органы со страницы каталога.
func parseCatalogPage(html, region string) []CatalogBody {
        var out []CatalogBody
        re := bodyLinkRe()
        for _, m := range re.FindAllStringSubmatch(html, -1) {
                slug, name := m[1], htmlUnescape(strings.TrimSpace(m[2]))
                if slug == "" || name == "" || len(name) < 3 {
                        continue
                }
                out = append(out, CatalogBody{Slug: slug, Name: name, Region: region})
        }
        return out
}

// bodyLinkRe — ссылки на страницы органов вида <a href="/body/<slug>">Название</a>.
func bodyLinkRe() *regexp.Regexp {
        return regexp.MustCompile(`<a href="/body/([a-z0-9_]+)"[^>]*>([^<]{3,300})</a>`)
}
