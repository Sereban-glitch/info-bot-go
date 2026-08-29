package dostup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient — клиент против локального тестового «портала».
func newTestClient(baseURL string) *Client {
	c := New("")
	c.BaseURL = baseURL
	return c
}

// ТЗ №5, фикс спама: SubmitRequest обязан брать адрес СОЗДАННОГО запроса,
// а не первую попавшуюся ссылку /request/... на странице ответа.
//
// Сценарий бага: страница после отправки содержит ссылку на ПРОШЛЫЙ
// запрос пользователя (например, в боковой панели) РАНЬШЕ ссылки на
// новый. Раньше бот записывал чужой адрес → две записи журнала делили
// один идентификатор → спам уведомлениями каждые 20 минут.
//
// Здесь эмулируем портал: после POST /new — страница с ловушкой,
// а «Мої запити» отдаёт список, где первым стоит НОВЫЙ запрос.
func TestSubmitRequestPicksNewestByTitle(t *testing.T) {
	var lastNewFormBody string
	mux := http.NewServeMux()

	mux.HandleFunc("/new/min-health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<input name="authenticity_token" value="t1"/><input name="info_request[public_body_id]" value="7"/>`))
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if r.FormValue("preview") == "1" {
				// Предпросмотр: страница с токеном для финального шага
				w.Write([]byte(`<input name="authenticity_token" value="t2"/>`))
				return
			}
			lastNewFormBody = r.FormValue("outgoing_message[body]")
			// Финальный ответ 200 со страницей, где ПЕРВОЙ идёт ссылка
			// на старый запрос, и только потом — на новый (ловушка
			// для наивного регэкспа «первая ссылка»).
			w.Write([]byte(`<html><body>
                          <a href="/request/old_dsa_request">Мій старий запит</a>
                          <a href="/request/moz_stats_2026">Новий</a>
                        </body></html>`))
			return
		}
		w.Write([]byte(`<input name="authenticity_token" value="t1"/>`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Главная: ссылка «Мої запити» + список свежих запросов
		w.Write([]byte(`<a href="/user/serhii/requests">Мої запити</a>`))
	})
	mux.HandleFunc("/user/serhii/requests", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`
                <div class="request_listing">
                  <a href="/request/moz_stats_2026#incoming-1">Про надання статистичної інформації серцево-судинних</a>
                  <time datetime="2026-08-29T08:37:52Z"></time>
                </div>
                <div class="request_listing">
                  <a href="/request/old_dsa_request">Перелік електронних адрес судів</a>
                  <time datetime="2026-08-27T20:35:09Z"></time>
                </div>`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetCredentials("a@b.c", "p")

	info, err := c.SubmitRequest("min-health",
		"Про надання статистичної інформації серцево-судинних захворювань",
		"Тело запроса")
	if err != nil {
		t.Fatalf("SubmitRequest: %v", err)
	}
	if lastNewFormBody == "" {
		t.Fatal("POST /new не был вызван")
	}
	if info.Slug != "moz_stats_2026" {
		t.Fatalf("slug=%q — бот снова взял чужой адрес (спам вернётся); ожидаем moz_stats_2026", info.Slug)
	}
	if !strings.HasPrefix(info.URL, srv.URL) {
		t.Fatalf("URL=%q", info.URL)
	}
}

// Если «Мої запити» недоступен, срабатывает запасной путь: адрес из
// редиректа после отправки.
func TestSubmitRequestFallsBackToRedirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/new/min-health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<input name="authenticity_token" value="t1"/><input name="info_request[public_body_id]" value="7"/>`))
	})
	mux.HandleFunc("/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if r.FormValue("preview") == "1" {
				w.Write([]byte(`<input name="authenticity_token" value="t2"/>`))
				return
			}
			w.Header().Set("Location", "/request/from_redirect_slug")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Write([]byte(`<input name="authenticity_token" value="t1"/>`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<p>нет ссылки Мої запити — сессия не активна</p>`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetCredentials("a@b.c", "p")

	info, err := c.SubmitRequest("min-health", "Тема", "Тело")
	if err != nil {
		t.Fatalf("SubmitRequest: %v", err)
	}
	if info.Slug != "from_redirect_slug" {
		t.Fatalf("slug=%q, ожидаем from_redirect_slug", info.Slug)
	}
}

// MyRequestsFull: названия с апострофами приходят как HTML-сущности —
// должны разкодироваться («здоров&#39;я» → «здоров'я»).
func TestMyRequestsFullUnescapesEntities(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/user/u/requests">Мої запити</a>`))
	})
	mux.HandleFunc("/user/u/requests", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`
                <div class="request_listing">
                  <a href="/request/moz_2026#incoming-1">Запит до Міністерства охорони здоров&#39;я</a>
                  <a href="https://dostup.org.ua/body/ministerstvo_okhorony_zdorovia_ukrainy">Міністерство охорони здоров&#39;я</a>
                  <time datetime="2026-08-29T08:37:52Z"></time>
                  <i class="icon-standalone icon_waiting_response"></i>
                </div>`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.SetCredentials("a@b.c", "p")

	reqs, err := c.MyRequestsFull()
	if err != nil {
		t.Fatalf("MyRequestsFull: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("запросов: %d, ожидаем 1", len(reqs))
	}
	if !strings.Contains(reqs[0].Title, "здоров'я") {
		t.Fatalf("Title с сущностью: %q", reqs[0].Title)
	}
	if !strings.Contains(reqs[0].BodyName, "здоров'я") {
		t.Fatalf("BodyName с сущностью: %q", reqs[0].BodyName)
	}
	if reqs[0].Status != "waiting_response" {
		t.Fatalf("Status=%q", reqs[0].Status)
	}
}
