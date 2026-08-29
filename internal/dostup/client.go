// Package dostup предоставляет клиент для сайта «Доступ до правди» (dostup.org.ua)
// — украинского портала публичных информационных запросов (форк Alavetelli).
//
// Протокол полностью повторяет поведение браузера и НЕ требует JavaScript:
// все CSRF-токены сервер отдаёт предзаполненными в HTML.
//
// Основные сценарии:
//   - Login(email, password)          — вход по логину/паролю (cookie-сессия)
//   - SearchBodies(query)             — поиск распорядителя в каталоге (2145+ органов)
//   - SubmitRequest(slug, title, body)— подача запроса (двухшаговая форма)
//   - RegisterFullFlow                — регистрация нового аккаунта (см. register.go)
//
// Сессия сохраняется в JSON-файл (cookies), между запусками переиспользуется.
package dostup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// BaseURL — продакшн-адрес портала.
const BaseURL = "https://dostup.org.ua"

// UserAgent — реалистичный UA (сервер отвечает 403 на пустые/ботовые UA у некоторых страниц).
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// Ошибки пакета.
var (
	ErrNotLoggedIn     = errors.New("dostup: не выполнен вход (сессия истекла)")
	ErrRateLimited     = errors.New("dostup: сервер вернул 500 (rate limit «Забагато запитів» или баг) — повторите через 3-5 минут")
	ErrBodyNotFound    = errors.New("dostup: распорядитель не найден в каталоге")
	ErrTokenNotFound   = errors.New("dostup: CSRF-токен не найден на странице (протокол сайта изменился?)")
	ErrInvalidResponse = errors.New("dostup: неожиданный ответ сервера")
)

// Body — распорядитель информации (госорган) из каталога портала.
type Body struct {
	Slug string `json:"slug"` // часть URL: /body/<slug>
	Name string `json:"name"` // официальное название
}

// RequestInfo — краткая карточка отправленного запроса.
type RequestInfo struct {
	URL   string `json:"url"`   // публичный адрес запроса
	Slug  string `json:"slug"`  // слаг запроса
	Title string `json:"title"` // заголовок
}

// Client — HTTP-клиент портала с cookie-сессией.
type Client struct {
	BaseURL      string
	http         *http.Client
	jar          *cookiejar.Jar
	sessionFile  string // путь к JSON-файлу для персистентности cookies
	email        string // логин (email) для авто-релейина
	password     string
	lastLocation string        // Location из последнего ответа (для ручных редиректов)
	ratings      *RatingsStore // персистентные рейтинги органов (может быть nil)
}

// New создаёт клиент. sessionFile может быть пустым — тогда сессия живёт
// только в памяти. Файл создаётся автоматически при Login().
func New(sessionFile string) *Client {
	jar, _ := cookiejar.New(nil)
	c := &Client{
		BaseURL:     BaseURL,
		jar:         jar,
		sessionFile: sessionFile,
		http: &http.Client{
			Jar:     jar,
			Timeout: 60 * time.Second,
		},
	}
	c.loadSession()
	return c
}

// SetCredentials задаёт логин/пароль для автоматического релейина при истечении сессии.
func (c *Client) SetCredentials(email, password string) {
	c.email = email
	c.password = password
}

// ---------- низкоуровневый HTTP ----------

func (c *Client) do(method, path string, form url.Values) (string, int, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "uk,ru;q=0.9,en;q=0.8")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	// Не следуем редиректам автоматически: контроль над каждой страницей
	// (после логина 302 — норма; после POST /new — важно прочитать финальный ответ).
	c.http.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	c.lastLocation = resp.Header.Get("Location")
	b, err := io.ReadAll(resp.Body)
	return string(b), resp.StatusCode, err
}

func (c *Client) get(path string) (string, int, error) {
	return c.do("GET", path, nil)
}

// GetPage — публичный доступ к произвольной странице портала (HTML).
// Используется для проверки статуса запросов и отладки.
func (c *Client) GetPage(path string) (string, int, error) {
	return c.getFollow(path)
}

// getFollow выполняет GET с ручным следованием редиректам (до 5),
// возвращая тело и код ФИНАЛЬНОЙ страницы.
func (c *Client) getFollow(path string) (string, int, error) {
	for i := 0; i < 6; i++ {
		body, code, err := c.do("GET", path, nil)
		if err != nil {
			return "", 0, err
		}
		if code == 301 || code == 302 || code == 303 || code == 307 || code == 308 {
			loc := c.lastLocation
			if loc == "" {
				return body, code, nil
			}
			if strings.HasPrefix(loc, "/") {
				path = loc
			} else if strings.HasPrefix(loc, c.BaseURL) {
				path = strings.TrimPrefix(loc, c.BaseURL)
			} else {
				return body, code, nil // внешний редирект — не следуем
			}
			continue
		}
		return body, code, nil
	}
	return "", 0, fmt.Errorf("dostup: слишком много редиректов")
}

func (c *Client) post(path string, form url.Values) (string, int, error) {
	return c.do("POST", path, form)
}

// ---------- сессия ----------

type sessionFileData struct {
	Cookies []*http.Cookie `json:"cookies"`
	Email   string         `json:"email,omitempty"`
}

func (c *Client) saveSession() {
	if c.sessionFile == "" {
		return
	}
	u, _ := url.Parse(c.BaseURL)
	data := sessionFileData{Cookies: c.jar.Cookies(u), Email: c.email}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(c.sessionFile, b, 0600)
}

func (c *Client) loadSession() {
	if c.sessionFile == "" {
		return
	}
	b, err := os.ReadFile(c.sessionFile)
	if err != nil {
		return
	}
	var data sessionFileData
	if json.Unmarshal(b, &data) != nil {
		return
	}
	u, _ := url.Parse(c.BaseURL)
	if len(data.Cookies) > 0 {
		c.jar.SetCookies(u, data.Cookies)
	}
	if data.Email != "" && c.email == "" {
		c.email = data.Email
	}
}

// IsLoggedIn проверяет живость сессии (на главной есть ссылка выхода).
func (c *Client) IsLoggedIn() bool {
	body, code, err := c.get("/")
	if err != nil || code != 200 {
		return false
	}
	return strings.Contains(body, "sign_out") || strings.Contains(body, "Мої запити")
}

// EnsureSession гарантирует рабочую сессию: при истечении — релейин
// (требуются SetCredentials или предыдущий Login с тем же файлом сессии).
func (c *Client) EnsureSession() error {
	if c.IsLoggedIn() {
		return nil
	}
	if c.email == "" || c.password == "" {
		return ErrNotLoggedIn
	}
	return c.Login(c.email, c.password)
}

// ---------- вход ----------

var reSigninToken = regexp.MustCompile(`name="token" id="signin_token" value="([^"]+)"`)

// Login выполняет вход по email+паролю. Работает без браузера:
// сервер кладёт signin_token прямо в HTML формы.
func (c *Client) Login(email, password string) error {
	// Уже залогинены действующей сессией? (сервер 302-редиректит
	// со страницы входа, если пользователь уже вошёл.)
	if c.IsLoggedIn() {
		c.email, c.password = email, password
		c.saveSession()
		return nil
	}

	page, code, err := c.get("/profile/sign_in")
	if err != nil {
		return fmt.Errorf("dostup: страница входа недоступна: %w", err)
	}
	if code != 200 {
		return fmt.Errorf("dostup: страница входа: HTTP %d", code)
	}
	m := reSigninToken.FindStringSubmatch(page)
	if m == nil {
		return ErrTokenNotFound
	}
	form := url.Values{
		"authenticity_token":    {""},
		"user_signin[email]":    {email},
		"user_signin[password]": {password},
		"remember_me":           {"1"},
		"token":                 {m[1]},
		"modal":                 {""},
		"commit":                {"Увійти"},
	}
	body, code, err := c.post("/profile/sign_in", form)
	if err != nil {
		return err
	}
	// Неудача: 200 с формой и errorExplanation.
	if code == 200 && strings.Contains(body, "errorExplanation") {
		return fmt.Errorf("dostup: неверный email или пароль")
	}
	// Успех: 302/303 или уже 200; проверяем фактическую сессию на главной.
	if c.IsLoggedIn() {
		c.email, c.password = email, password
		c.saveSession()
		return nil
	}
	return fmt.Errorf("dostup: вход не удался (HTTP %d)", code)
}

// ---------- поиск распорядителей ----------

var (
	reBodyLink = regexp.MustCompile(`(?s)href="/body/([a-z0-9_]+)"[^>]*>(.*?)</a>`)
	reTag      = regexp.MustCompile(`<[^>]+>`)
)

// SearchBodies ищет распорядителей по названию (регистр не важен).
// Протокол: GET /body/list/all?public_body_query=<текст>.
func (c *Client) SearchBodies(query string) ([]Body, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	path := "/body/list/all?public_body_query=" + url.QueryEscape(q)
	page, code, err := c.get(path)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: поиск: HTTP %d", code)
	}
	var out []Body
	seen := map[string]bool{}
	for _, m := range reBodyLink.FindAllStringSubmatch(page, -1) {
		slug, name := m[1], strings.TrimSpace(reTag.ReplaceAllString(m[2], ""))
		if slug == "" || name == "" || strings.HasPrefix(slug, "list") || seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, Body{Slug: slug, Name: name})
		if len(out) >= 30 { // разумный предел для клавиатуры Telegram
			break
		}
	}
	return out, nil
}

// ---------- подача запроса ----------

var (
	// Порядок атрибутов name/value на портале непостоянен (value может идти
	// как до, так и после name), поэтому ищем весь тег input, затем значение в нём.
	reTokenInput  = regexp.MustCompile(`<input[^>]*name="authenticity_token"[^>]*>`)
	reBodyIDInput = regexp.MustCompile(`<input[^>]*name="info_request\[public_body_id\]"[^>]*>`)
	reInputValue  = regexp.MustCompile(`value="([^"]*)"`)
	reRequestLink = regexp.MustCompile(`href="(/request/[^"#]+)"`)
)

// isRateLimited распознаёт анти-спам страницу портала «Забагато запитів».
// Портал отдаёт её с HTTP 200 (а иногда 500) — проверять нужно содержимое.
func isRateLimited(page string) bool {
	return strings.Contains(page, "Забагато запитів")
}

// SubmitRequest подаёт информационный запрос в два шага (обязательный preview):
//
//	Шаг 1: GET  /new/<slug>                    — authenticity_token + public_body_id
//	Шаг 2: POST /new  (preview=1, commit=...)   — страница предпросмотра, НОВЫЙ токен
//	Шаг 3: POST /new  (preview=0, submit=...)   — публикация, ответ содержит /request/<slug>
//
// Возвращает публичный URL запроса.
func (c *Client) SubmitRequest(bodySlug, title, text string) (*RequestInfo, error) {
	if err := c.EnsureSession(); err != nil {
		return nil, err
	}

	// Шаг 1: страница формы
	page, code, err := c.get("/new/" + bodySlug)
	if err != nil {
		return nil, err
	}
	if code == 404 {
		return nil, ErrBodyNotFound
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: форма запроса: HTTP %d", code)
	}
	if isRateLimited(page) {
		return nil, ErrRateLimited
	}
	mt := reTokenInput.FindString(page)
	mb := reBodyIDInput.FindString(page)
	if mt == "" || mb == "" {
		return nil, ErrTokenNotFound
	}
	mv := reInputValue.FindStringSubmatch(mt)
	mvid := reInputValue.FindStringSubmatch(mb)
	if mv == nil || mvid == nil || mvid[1] == "" {
		return nil, ErrTokenNotFound
	}
	token1, bodyID := mv[1], mvid[1]

	// Шаг 2: preview
	previewForm := url.Values{
		"authenticity_token":           {token1},
		"info_request[title]":          {title},
		"outgoing_message[body]":       {text},
		"info_request[public_body_id]": {bodyID},
		"submitted_new_request":        {"1"},
		"preview":                      {"1"},
		"commit":                       {"Наступний крок: попередній перегляд"},
	}
	previewPage, code, err := c.post("/new", previewForm)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: предпросмотр: HTTP %d", code)
	}
	if isRateLimited(previewPage) {
		return nil, ErrRateLimited
	}
	mt2 := reTokenInput.FindString(previewPage)
	if mt2 == "" {
		return nil, fmt.Errorf("dostup: предпросмотр не прошёл валидацию (проверьте длину темы/текста)")
	}
	mv2 := reInputValue.FindStringSubmatch(mt2)
	if mv2 == nil || mv2[1] == "" {
		return nil, ErrTokenNotFound
	}
	token2 := mv2[1]

	// Шаг 3: финальная отправка
	sendForm := url.Values{
		"authenticity_token":           {token2},
		"info_request[title]":          {title},
		"outgoing_message[body]":       {text},
		"info_request[public_body_id]": {bodyID},
		"info_request[tag_string]":     {""},
		"submitted_new_request":        {"1"},
		"preview":                      {"0"},
		"submit":                       {"Відправити та опублікувати"},
	}
	sendPage, code, err := c.post("/new", sendForm)
	if err != nil {
		return nil, err
	}

	// 500 или страница «Забагато запитів»: известная особенность сайта —
	// rate limit; запрос обычно НЕ создаётся, повтор через 3-5 минут проходит.
	if code == 500 || isRateLimited(sendPage) {
		return nil, ErrRateLimited
	}

	// Ищем ссылку на созданный запрос
	if m := reRequestLink.FindStringSubmatch(sendPage); m != nil {
		return &RequestInfo{
			URL:   c.BaseURL + m[1],
			Slug:  strings.TrimPrefix(m[1], "/request/"),
			Title: title,
		}, nil
	}

	// Иногда после успешного создания бывает 302 на страницу запроса.
	if code == 302 || code == 303 {
		if reqs, err := c.MyRequests(); err == nil && len(reqs) > 0 {
			prefix := title
			if len(prefix) > 30 {
				prefix = prefix[:30]
			}
			for _, r := range reqs {
				if strings.Contains(strings.ToLower(r.Title), strings.ToLower(prefix)) {
					return &r, nil
				}
			}
			// Вернём самый свежий как наиболее вероятный результат
			r := reqs[0]
			return &r, nil
		}
	}
	return nil, ErrInvalidResponse
}

// ---------- мои запросы ----------

var reUserRequest = regexp.MustCompile(`(?s)href="(/request/[^"#]+)"[^>]*>(.*?)</a>`)

// reMyRequestsLink — ссылка «Мої запити» на главной: /user/<ім'я>/requests
var reMyRequestsLink = regexp.MustCompile(`href="(/user/[^"]+/requests)"[^>]*>\s*Мої запити`)

// MyRequests возвращает список запросов текущего пользователя (свежие сверху).
func (c *Client) MyRequests() ([]RequestInfo, error) {
	full, err := c.MyRequestsFull()
	if err != nil {
		return nil, err
	}
	out := make([]RequestInfo, 0, len(full))
	for _, r := range full {
		out = append(out, r.RequestInfo)
	}
	return out, nil
}

// PortalRequest — карточка запроса со страницы «Мої запити» портала.
type PortalRequest struct {
	RequestInfo        // URL, Slug, Title
	BodyName    string // название распорядителя
	BodySlug    string // слаг распорядителя на портале
	Date        string // ISO-datetime создания/получения
	Status      string // машинный статус (waiting_response, successful, ...)
	HasResponse bool   // орган уже прислал ответ (ссылка с #incoming-)
}

var (
	reListingLink = regexp.MustCompile(`(?s)href="(/request/[^"#]+)(#[^"]*)?"[^>]*>(.*?)</a>`)
	reListingBody = regexp.MustCompile(`(?s)href="https?://[^"]*/body/([a-z0-9_]+)"[^>]*>(.*?)</a>`)
	reListingTime = regexp.MustCompile(`<time[^>]*datetime="([^"]+)"`)
	reListingIcon = regexp.MustCompile(`icon-standalone icon_([a-z_]+)`)
)

// MyRequestsFull возвращает запросы пользователя портала с датами и статусами.
// Источник истины: страница «Мої запити» (нужна активная сессия).
func (c *Client) MyRequestsFull() ([]PortalRequest, error) {
	if err := c.EnsureSession(); err != nil {
		return nil, err
	}
	home, code, err := c.get("/")
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: главная: HTTP %d", code)
	}
	m := reMyRequestsLink.FindStringSubmatch(home)
	if m == nil {
		return nil, ErrNotLoggedIn // нет ссылки — сессия не активна
	}
	page, code, err := c.getFollow(m[1])
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: мои запросы: HTTP %d", code)
	}

	var out []PortalRequest
	seen := map[string]bool{}
	// Блоки request_listing содержат вложенные div — режем страницу
	// по открывающему тегу и берём достаточное окно для разбора.
	chunks := strings.Split(page, `<div class="request_listing">`)
	for _, blkRaw := range chunks[1:] {
		blk := blkRaw
		if len(blk) > 4000 {
			blk = blk[:4000]
		}
		ml := reListingLink.FindStringSubmatch(blk)
		if ml == nil {
			continue
		}
		slug := strings.TrimPrefix(ml[1], "/request/")
		if slug == "" || strings.HasPrefix(slug, "new") || seen[slug] {
			continue
		}
		seen[slug] = true
		pr := PortalRequest{
			RequestInfo: RequestInfo{
				URL:   c.BaseURL + ml[1],
				Slug:  slug,
				Title: strings.TrimSpace(reTag.ReplaceAllString(ml[3], "")),
			},
			HasResponse: ml[2] != "", // ссылка «#incoming-N» — есть ответ
		}
		if mb := reListingBody.FindStringSubmatch(blk); mb != nil {
			pr.BodySlug = mb[1]
			pr.BodyName = strings.TrimSpace(reTag.ReplaceAllString(mb[2], ""))
		}
		if mt := reListingTime.FindStringSubmatch(blk); mt != nil {
			pr.Date = mt[1]
		}
		if mi := reListingIcon.FindStringSubmatch(blk); mi != nil {
			pr.Status = mi[1]
		}
		out = append(out, pr)
		if len(out) >= 50 {
			break
		}
	}
	return out, nil
}

// ---------- статус публичной страницы запроса ----------

// RequestStatus — состояние запроса, парсится из ПУБЛИЧНОЙ страницы
// (авторизация не нужна — страницу может открыть любой по ссылке).
type RequestStatus struct {
	Slug            string `json:"slug"`
	Status          string `json:"status"`             // машинный статус портала
	Deadline        string `json:"deadline,omitempty"` // ожидаемая дата ответа (ISO)
	HasResponse     bool   `json:"hasResponse"`        // есть входящее сообщение от органа
	ResponseFrom    string `json:"responseFrom,omitempty"`
	ResponseExcerpt string `json:"responseExcerpt,omitempty"` // первые ~400 символов ответа
	LastIncomingID  string `json:"lastIncomingId,omitempty"`  // id последнего входящего (дедупликация уведомлений)
	IncomingCount   int    `json:"incomingCount"`             // сколько входящих сообщений всего
}

var (
	reStatusDiv   = regexp.MustCompile(`(?s)id="request_status"[^>]*class="([^"]*)"`)
	reStatusTime  = regexp.MustCompile(`(?s)id="request_status".{0,999}?<time datetime="([^"]+)"`)
	reIncoming    = regexp.MustCompile(`id="incoming-(\d+)"`)
	reCorrAuthor  = regexp.MustCompile(`(?s)class="correspondence__header__author[^"]*"[^>]*>(.*?)</span>`)
	reCorrText    = regexp.MustCompile(`(?s)class="correspondence_text"[^>]*>(.*?)<p class="event_actions"`)
	reIncomingHdr = regexp.MustCompile(`(?s)id="incoming-\d+"[^>]*>(.{0,400}?)<div[^>]*class="correspondence_text`)
)

// StatusLabel возвращает человекочитаемую метку статуса (укр) с эмодзи.
func StatusLabel(status string) string {
	labels := map[string]string{
		"waiting_response":       "⏳ Очікується відповідь",
		"waiting_classification": "📬 Відповідь отримана (обробляється)",
		"waiting_clarification":  "❓ Потрібне уточнення запиту",
		"successful":             "✅ Успішна відповідь",
		"partially_successful":   "🟡 Частково успішна відповідь",
		"rejected":               "❌ У відовленні відмовлено",
		"not_held":               "📭 Орган інформацією не володіє",
		"gone_postal":            "📮 Відповідь надіслана поштою",
		"internal_review":        "🔁 Внутрішній розгляд",
		"error_message":          "⚠️ Помилка доставки запиту",
		"requires_admin":         "🛡️ Потребує рішення адміністрації",
		"user_withdrawn":         "🚫 Запит відкликано",
		"attention_requested":    "🚨 Запит потребує уваги",
	}
	if l, ok := labels[status]; ok {
		return l
	}
	return "❔ " + status
}

// ResponseArrived — статус, означающий наличие ответа органа.
func ResponseArrived(status string) bool {
	switch status {
	case "waiting_classification", "successful", "partially_successful",
		"rejected", "not_held", "gone_postal", "internal_review":
		return true
	}
	return false
}

// GetRequestStatus парсит публичную страницу запроса (без авторизации):
// статус, дедлайн и, если есть, — текст ответа органа.
func (c *Client) GetRequestStatus(slug string) (*RequestStatus, error) {
	page, code, err := c.getFollow("/request/" + slug)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: страница запроса: HTTP %d", code)
	}
	st := &RequestStatus{Slug: slug}
	if m := reStatusDiv.FindStringSubmatch(page); m != nil {
		// class="request-status-message request-status-message--<status>"
		if i := strings.Index(m[1], "--"); i >= 0 {
			st.Status = strings.TrimSpace(m[1][i+2:])
		}
	}
	if m := reStatusTime.FindStringSubmatch(page); m != nil {
		st.Deadline = m[1]
	}
	// входящие сообщения (ответы органа)
	incoming := reIncoming.FindAllStringSubmatchIndex(page, -1)
	if len(incoming) > 0 {
		last := incoming[len(incoming)-1]
		st.HasResponse = true
		st.IncomingCount = len(incoming)
		// полный матч — `id="incoming-N"`; вырезаем N
		full := page[last[0]:last[1]]
		if i := strings.Index(full, "incoming-"); i >= 0 {
			st.LastIncomingID = strings.TrimSuffix(full[i+len("incoming-"):], `"`)
		}
		// ищем отправителя и тело письма после последнего incoming-якоря
		tail := page[last[0]:]
		if m := reCorrAuthor.FindStringSubmatch(tail); m != nil {
			from := strings.Join(strings.Fields(htmlUnescape(reTag.ReplaceAllString(m[1], " "))), " ")
			if i := strings.Index(from, ","); i > 0 {
				from = from[:i]
			}
			if len(from) > 80 {
				from = from[:80]
			}
			st.ResponseFrom = from
		} else if m := reIncomingHdr.FindStringSubmatch(tail); m != nil {
			plain := reTag.ReplaceAllString(m[1], " ")
			plain = strings.Join(strings.Fields(plain), " ")
			st.ResponseFrom = strings.TrimSpace(plain[:min(80, len(plain))])
		}
		if m := reCorrText.FindStringSubmatch(tail); m != nil {
			text := htmlUnescape(reTag.ReplaceAllString(m[1], " "))
			text = strings.Join(strings.Fields(text), " ")
			if len(text) > 400 {
				text = text[:400] + "…"
			}
			st.ResponseExcerpt = text
		}
	}
	return st, nil
}

// htmlUnescape заменяет базовые HTML-сущности.
func htmlUnescape(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
		"&#39;", "'", "&nbsp;", " ", "&mdash;", "—", "&laquo;", "«", "&raquo;", "»",
	)
	return replacer.Replace(s)
}

// min для Go 1.19 (встроенный min появился только в 1.21).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
