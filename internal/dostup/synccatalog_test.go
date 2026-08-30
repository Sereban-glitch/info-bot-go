package dostup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// catalogPage — HTML-страница каталога со ссылками на органы.
func catalogPage(slugs ...[2]string) string {
	var b strings.Builder
	b.WriteString("<html><body><div class=\"body_list\">")
	for _, pair := range slugs {
		fmt.Fprintf(&b, "<a href=\"/body/%s\">%s</a>", pair[0], pair[1])
	}
	b.WriteString("</div></body></html>")
	return b.String()
}

// newPortalServer — мок портала с НОВОЙ (после редизайна) структурой адрес:
//   - /body/list/<region>?page=N — региональные листинги (работают);
//   - /body?page=N               — общий список всех органов (новый адрес);
//   - /body/list/?page=N и /body/list?page=N — мёртвые адресы (404).
//
// Возвращает сервер и журнал запрошенных путей (для проверки, что мёртвые
// адресы не запрашиваются вовсе).
func newPortalServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string

	regional := map[string]map[int]string{
		// Миколаївська область: одна страница, два органа.
		"myk": {
			1: catalogPage(
				[2]string{"miska_rada_mikolayeva", "Миколаївська міська рада"},
				[2]string{"obldierzhadministratsiia", "Миколаївська обласна державна адміністрація"},
			),
		},
		// Київ: две страницы (2 + 1 орган) — проверка пагинации регионов.
		"kyiv": {
			1: catalogPage(
				[2]string{"kyivska_miska_rada", "Київська міська рада"},
				[2]string{"kievsk_ga", "Головне управління поліції в Києві"},
			),
			2: catalogPage(
				[2]string{"kyiv_obl_rada", "Київська обласна рада"},
			),
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path+fmt.Sprintf("?page=%s", r.URL.Query().Get("page")))
		mu.Unlock()

		switch {
		case r.URL.Path == "/body": // общий список
			switch r.URL.Query().Get("page") {
			case "1", "":
				// все 5 региональных органов + 2 центральных (новых).
				w.Write([]byte(catalogPage(
					[2]string{"miska_rada_mikolayeva", "Миколаївська міська рада"},
					[2]string{"obldierzhadministratsiia", "Миколаївська обласна державна адміністрація"},
					[2]string{"kyivska_miska_rada", "Київська міська рада"},
					[2]string{"kievsk_ga", "Головне управління поліції в Києві"},
					[2]string{"kyiv_obl_rada", "Київська обласна рада"},
					[2]string{"ministerstvo_ohorony_zdorovia", "Міністерство охорони здоров'я України"},
					[2]string{"derzhavna_sudova_administratsiia", "Державна судова адміністрація України"},
				)))
			default: // страницы дальше первой — пустые
				w.Write([]byte(catalogPage()))
			}
		case strings.HasPrefix(r.URL.Path, "/body/list/"):
			region := strings.TrimPrefix(r.URL.Path, "/body/list/")
			pages, ok := regional[region]
			if !ok { // регион без органов — пустая страница
				w.Write([]byte(catalogPage()))
				return
			}
			page := r.URL.Query().Get("page")
			if page == "" {
				page = "1"
			}
			if html, ok := pages[atoiDefault(page, 1)]; ok {
				w.Write([]byte(html))
				return
			}
			w.Write([]byte(catalogPage())) // дальше региональных страниц нет
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	return server, &paths
}

func atoiDefault(s string, def int) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	if n == 0 {
		return def
	}
	return n
}

func TestSyncCatalog_NewPortalLayout(t *testing.T) {
	server, paths := newPortalServer(t)
	defer server.Close()

	oldDelay := catalogPageDelay
	catalogPageDelay = 0
	defer func() { catalogPageDelay = oldDelay }()

	c := New("")
	c.BaseURL = server.URL

	cat, err := c.SyncCatalog()
	if err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	// 7 уникальных органов: 2 (myk) + 3 (kyiv) + 2 центральных.
	if len(cat.Bodies) != 7 {
		t.Fatalf("органов = %d, want 7: %+v", len(cat.Bodies), cat.Bodies)
	}

	bySlug := map[string]CatalogBody{}
	for _, b := range cat.Bodies {
		if _, dup := bySlug[b.Slug]; dup {
			t.Errorf("дубликат слага %q", b.Slug)
		}
		bySlug[b.Slug] = b
	}

	// Регионы присвоены фазой 1 и не перетёрты общим списком фазы 2.
	cases := []struct{ slug, wantRegion string }{
		{"miska_rada_mikolayeva", "myk"},
		{"obldierzhadministratsiia", "myk"},
		{"kyivska_miska_rada", "kyiv"},
		{"kievsk_ga", "kyiv"},
		{"kyiv_obl_rada", "kyiv"},
		{"ministerstvo_ohorony_zdorovia", ""},    // центральные
		{"derzhavna_sudova_administratsiia", ""}, // центральные
	}
	for _, tc := range cases {
		b, ok := bySlug[tc.slug]
		if !ok {
			t.Errorf("орган %q отсутствует в каталоге", tc.slug)
			continue
		}
		if b.Region != tc.wantRegion {
			t.Errorf("%q: регион = %q, want %q", tc.slug, b.Region, tc.wantRegion)
		}
	}

	// Названия распарсены (не пустые).
	if n := bySlug["miska_rada_mikolayeva"].Name; n != "Миколаївська міська рада" {
		t.Errorf("название = %q", n)
	}

	// Мёртвые адресы старого центрального раздела не запрашиваются вовсе.
	for _, p := range *paths {
		if p == "/body/list/?page=1" || p == "/body/list?page=1" {
			t.Errorf("обращение к мёртвому адресу %q", p)
		}
	}
	// Новый адрес общего списка используется.
	sawGeneral := false
	for _, p := range *paths {
		if p == "/body?page=1" {
			sawGeneral = true
		}
	}
	if !sawGeneral {
		t.Errorf("общий список /body?page=1 не запрашивался; paths=%v", *paths)
	}
}

func TestSyncCatalog_GeneralList404(t *testing.T) {
	// Портал отдаёт 404 на общий список — ошибка должна быть понятной
	// и содержать пометку «общая» (не молчаливый провал).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/body" {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/body/list/") {
			w.Write([]byte(catalogPage())) // регионы пустые
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	oldDelay := catalogPageDelay
	catalogPageDelay = 0
	defer func() { catalogPageDelay = oldDelay }()

	c := New("")
	c.BaseURL = server.URL

	_, err := c.SyncCatalog()
	if err == nil {
		t.Fatal("ожидалась ошибка при 404 на общем списке")
	}
	if !strings.Contains(err.Error(), "общая") {
		t.Errorf("ошибка без пометки «общая»: %v", err)
	}
}
