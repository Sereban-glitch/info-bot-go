package dostup

// Публичный поиск запросов на портале «Доступ до правди»
// (для мини-приложения: «Запити спільноти» — проверьте, не спрашивал ли
// кто-то уже то же самое; ответ может быть уже опубликован).
//
// URL: /search/<query>/all — HTML со списком request_listing.

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// PublicRequest — карточка публичного запроса портала.
type PublicRequest struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	BodyName string `json:"bodyName,omitempty"`
	Status   string `json:"status,omitempty"` // successful|waiting_response|overdue|rejected|...
	Date     string `json:"date,omitempty"`   // YYYY-MM-DD
	URL      string `json:"url"`
}

var (
	reSearchListing = regexp.MustCompile(`<div class="request_listing">`)
	reSearchTitle   = regexp.MustCompile(`<a href="/request/([a-z0-9_]+)(?:#[^"]*)?"[^>]*>(.*?)</a>`)
	reSearchBodyTo  = regexp.MustCompile(`(?s)відіслано до\s*<a[^>]*>(.*?)</a>`)
	reSearchBodyFrm = regexp.MustCompile(`(?si)відповідь від розпорядника\s*<a[^>]*>(.*?)</a>`)
	reSearchDate    = regexp.MustCompile(`<time datetime="([0-9\-]{10})`)
	reSearchStatus  = regexp.MustCompile(`icon-standalone icon_([a-z_]+)`)
)

// SearchURL — публичная ссылка поиска на портале.
func SearchURL(query string) string {
	return BaseURL + "/search/" + url.PathEscape(strings.TrimSpace(query)) + "/all"
}

// SearchRequests ищет публичные запросы по подстроке (без авторизации).
func (c *Client) SearchRequests(query string) ([]PublicRequest, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("dostup: пустой поисковый запрос")
	}
	page, code, err := c.get("/search/" + url.PathEscape(query) + "/all")
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: поиск: HTTP %d", code)
	}
	if isRateLimited(page) {
		return nil, ErrRateLimited
	}
	return parseSearchResults(page), nil
}

// parseSearchResults — разбор страницы поиска.
func parseSearchResults(html string) []PublicRequest {
	var out []PublicRequest
	for _, block := range splitListings(html) {
		mt := reSearchTitle.FindStringSubmatch(block)
		if mt == nil {
			continue
		}
		pr := PublicRequest{
			Slug:  mt[1],
			Title: htmlUnescape(strings.TrimSpace(stripTags(mt[2]))),
			URL:   BaseURL + "/request/" + mt[1],
		}
		if mb := reSearchBodyTo.FindStringSubmatch(block); mb != nil {
			pr.BodyName = htmlUnescape(strings.TrimSpace(mb[1]))
		} else if mb := reSearchBodyFrm.FindStringSubmatch(block); mb != nil {
			pr.BodyName = htmlUnescape(strings.TrimSpace(mb[1]))
		}
		if md := reSearchDate.FindStringSubmatch(block); md != nil {
			pr.Date = md[1]
		}
		if ms := reSearchStatus.FindStringSubmatch(block); ms != nil {
			pr.Status = strings.TrimSuffix(ms[1], "_response")
			pr.Status = strings.TrimSuffix(pr.Status, "_classification")
		}
		if pr.Title != "" {
			out = append(out, pr)
		}
	}
	return out
}

// stripTags убирает HTML-теги.
func stripTags(s string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
}

// splitListings режет страницу поиска на блоки request_listing
// (блоки содержат вложенные div — границы определяем по началам блоков).
func splitListings(html string) []string {
	locs := reSearchListing.FindAllStringIndex(html, -1)
	if len(locs) == 0 {
		return nil
	}
	var out []string
	for i, loc := range locs {
		end := len(html)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out = append(out, html[loc[0]:end])
	}
	return out
}
