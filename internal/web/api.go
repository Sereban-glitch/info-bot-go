package web

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"info-bot-go/internal/directory"
	"info-bot-go/internal/dostup"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
)

// ---------------------------------------------------------------------------
// API response types
// ---------------------------------------------------------------------------

type APIResponse struct {
	OK   bool        `json:"ok"`
	Data interface{} `json:"data,omitempty"`
	Err  string      `json:"error,omitempty"`
}

type ProfileResponse struct {
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	MiddleName string `json:"middleName"`
	Email      string `json:"email"`
	Address    string `json:"address"`
	FullName   string `json:"fullName"`
	Ready      bool   `json:"ready"`
}

type RequestItem struct {
	ID              string `json:"id"`
	RecipientName   string `json:"recipientName"`
	RecipientEmail  string `json:"recipientEmail"`
	Subject         string `json:"subject"`
	Date            string `json:"date"`
	Delivered       bool   `json:"delivered"`
	Status          string `json:"status"`
	ReplyReceivedAt string `json:"replyReceivedAt"`
	DaysLeft        int    `json:"daysLeft"`
	Channel         string `json:"channel,omitempty"`
	URL             string `json:"url,omitempty"`
	LastStatus      string `json:"lastStatus,omitempty"`
	ResponseExcerpt string `json:"responseExcerpt,omitempty"`
	AckAt           string `json:"ackAt,omitempty"` // получено только авто-подтверждение — ответа по существу ещё нет
}

type StatsResponse struct {
	Total   int `json:"total"`
	Pending int `json:"pending"`
	Replied int `json:"replied"`
	Overdue int `json:"overdue"`
}

type TemplateItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type DirectoryEntry struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Category string `json:"category"`
}

type GenerateTemplateRequest struct {
	Description string `json:"description"`
}

type GenerateTemplateResponse struct {
	Subject       string   `json:"subject"`
	Body          string   `json:"body"`
	LawRefs       []LawRef `json:"lawRefs"`
	RecipientHint string   `json:"recipientHint"`
}

type LawRef struct {
	Article   string `json:"article"`
	Title     string `json:"title"`
	Relevance string `json:"relevance"`
}

// ---------------------------------------------------------------------------
// Auth middleware
// ---------------------------------------------------------------------------

// authMiddleware validates Telegram WebApp initData via HMAC-SHA256 and injects userID into context.
// Only HMAC-validated requests are allowed; no fallback to X-User-ID for security.
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		initData := r.Header.Get("X-Init-Data")
		var userID int64
		var ok bool
		if initData != "" {
			userID, ok = ValidateInitData(initData, s.cfg.BotToken)
		} else if queryData := r.URL.Query().Get("init_data"); queryData != "" {
			userID, ok = ValidateInitData(queryData, s.cfg.BotToken)
		}

		if ok && userID > 0 {
			r.Header.Set("X-User-ID", fmt.Sprintf("%d", userID))
			log.Printf("[AUTH] HMAC OK: user_id=%d path=%s", userID, r.URL.Path)
			next(w, r)
			return
		}

		// Security fix (audit #22, A1): no user_id / X-User-ID fallbacks.
		// Unsigned identifiers let anyone read any user's profile and history.
		log.Printf("[AUTH] FAILED: no valid auth: path=%s remote=%s", r.URL.Path, r.RemoteAddr)
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "missing auth"})
	}
}

// getUserID extracts the validated user ID from request headers.
func getUserID(r *http.Request) int64 {
	s := r.Header.Get("X-User-ID")
	if s == "" {
		return 0
	}
	var id int64
	fmt.Sscanf(s, "%d", &id)
	return id
}

// ---------------------------------------------------------------------------
// API handlers
// ---------------------------------------------------------------------------

// handleMe returns the user's profile and stats.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}

	userID := getUserID(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
		return
	}

	// Load session
	key := session.SessionKey(userID)
	sess, err := s.sessions.Get(key)
	if err != nil {
		sess = session.NewSessionData()
	}

	profile := ProfileResponse{
		FirstName:  sess.Profile.FirstName,
		LastName:   sess.Profile.LastName,
		MiddleName: sess.Profile.MiddleName,
		Email:      sess.Profile.Email,
		Address:    sess.Profile.PostalAddress,
		FullName:   session.ProfileDisplayName(sess.Profile),
		Ready:      session.IsProfileReady(sess.Profile),
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: profile})
}

// handleRequests returns the user's sent requests.
func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}

	userID := getUserID(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
		return
	}

	entries := s.sentLog.ListByUser(userID)
	items := make([]RequestItem, 0, len(entries))

	for _, e := range entries {
		daysLeft := calcDaysLeft(e.Date)
		status := "pending"
		if e.Status == "bounced" {
			status = "bounced"
		} else if e.ReplyReceivedAt != "" {
			status = "replied"
		} else if daysLeft < 0 {
			status = "overdue"
		}

		items = append(items, RequestItem{
			ID:              e.MessageID,
			RecipientName:   e.RecipientName,
			RecipientEmail:  e.RecipientEmail,
			Subject:         e.Subject,
			Date:            e.Date,
			Delivered:       e.Delivered,
			Status:          status,
			ReplyReceivedAt: e.ReplyReceivedAt,
			DaysLeft:        daysLeft,
			Channel:         e.Channel,
			URL:             e.URL,
			LastStatus:      e.LastStatus,
			ResponseExcerpt: e.ResponseExcerpt,
			AckAt:           e.AckAt,
		})
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: items})
}

// handleTemplates returns all available templates.
func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}

	items := make([]TemplateItem, 0, len(builtInTemplates))
	for _, t := range builtInTemplates {
		items = append(items, TemplateItem{
			ID:      t.ID,
			Title:   t.Title,
			Subject: t.Subject,
			Body:    t.Body,
		})
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: items})
}

// handleDirectory returns the directory of government bodies.
func (s *Server) handleDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}

	query := r.URL.Query().Get("q")
	var entries []directory.Recipient

	if query != "" && s.directory != nil {
		entries = s.directory.Search(query)
	} else if s.directory != nil {
		entries = s.directory.AllRecipients()
	}

	items := make([]DirectoryEntry, 0, len(entries))
	for _, e := range entries {
		items = append(items, DirectoryEntry{
			ID:       e.ID,
			Name:     e.Name,
			Email:    e.Email,
			Category: e.Category,
		})
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: items})
}

// handleStats returns global and per-user stats.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}

	userID := getUserID(r)

	var entries []sentlog.SentEntry
	if userID > 0 {
		entries = s.sentLog.ListByUser(userID)
	} else {
		entries = s.sentLog.ListAll()
	}

	stats := StatsResponse{}
	for _, e := range entries {
		stats.Total++
		daysLeft := calcDaysLeft(e.Date)
		if e.ReplyReceivedAt != "" {
			stats.Replied++
		} else if e.Status == "bounced" {
			// Count bounced as pending for stats
			stats.Pending++
		} else if daysLeft < 0 {
			stats.Overdue++
		} else {
			stats.Pending++
		}
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: stats})
}

// handleGenerateTemplate generates a FOI request template from a short description using AI.
func (s *Server) handleGenerateTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}

	userID := getUserID(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
		return
	}

	if s.gemini == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{OK: false, Err: "AI not configured"})
		return
	}

	var req GenerateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "invalid request body"})
		return
	}

	if strings.TrimSpace(req.Description) == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "description is required"})
		return
	}

	subject, body, lawRefs, recipientHint, err := s.gemini.GenerateFromDescription(req.Description)
	if err != nil {
		log.Printf("[WEB] generate-template error for user %d: %v", userID, err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Err: "AI generation failed"})
		return
	}

	// Convert []map[string]string to []LawRef
	refItems := make([]LawRef, 0, len(lawRefs))
	for _, lr := range lawRefs {
		refItems = append(refItems, LawRef{
			Article:   lr["article"],
			Title:     lr["title"],
			Relevance: lr["relevance"],
		})
	}

	result := GenerateTemplateResponse{
		Subject:       subject,
		Body:          body,
		LawRefs:       refItems,
		RecipientHint: recipientHint,
	}

	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: result})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// calcDaysLeft calculates working days remaining from a date string.
func calcDaysLeft(dateStr string) int {
	if dateStr == "" {
		return 0
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		// Try RFC3339
		t, err = time.Parse(time.RFC3339, dateStr)
		if err != nil {
			return 0
		}
	}
	deadline := addWorkingDays(t, 5)
	now := time.Now()
	days := int(deadline.Sub(now).Hours() / 24)
	return days
}

// addWorkingDays adds n working (business) days to the given date.
func addWorkingDays(start time.Time, n int) time.Time {
	d := start
	added := 0
	for added < n {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			added++
		}
	}
	return d
}

// Built-in templates data (mirrors handlers/templates.go)
var builtInTemplates = []struct {
	ID      string
	Title   string
	Subject string
	Body    string
}{
	{"shelters", "🛡️ Стан укриттів", "Технічний стан та фінансування укриттів", "Прошу надати копію акту останньої перевірки технічного стану укриття за адресою [вказати адресу] та інформацію про суму коштів, виділених на його утримання у 2024-2026 роках."},
	{"energy_res", "💡 Енергонезалежність", "Закупівля генераторів та палива", "Прошу надати перелік закупівель генераторів та систем накопичення енергії вашим органом за останній рік із зазначенням вартості одиниці товару та місця їх експлуатації."},
	{"blackouts", "🔌 Справедливі відключення", "Підстави внесення об'єктів до критичної інфраструктури", "На якій підставі об'єкт за адресою [вказати адресу] внесено до переліку критичної інфраструктури, що не підлягає відключенням? Прошу надати копію відповідного рішення."},
	{"medicine", "🏥 Медицина та ВВК", "Фінансування та доступність ВВК/лікарні", "Скільки бюджетних коштів було виділено на закупівлю медикаментів для [назва лікарні] за останній рік? Прошу надати звіт про використання цих коштів та стан черги на проходження ВВК."},
	{"education", "🎒 Безпека у школах", "Стан укриттів у навчальних закладах", "Чи відповідає укриття закладу [номер/назва] нормам ДСНС? Прошу надати копію акту готовності закладу до навчального року та інформацію про облік благодійних внесків батьків."},
	{"vpo", "🤝 Допомога ВПО", "Розподіл гуманітарної допомоги для ВПО", "Прошу надати інформацію про обсяги фінансової та гуманітарної допомоги, отриманої вашим органом для потреб ВПО за останній квартал, та перелік програм, на які ці ресурси спрямовані."},
	{"recovery", "🏚️ єВідновлення", "Статус виплат за пошкоджене майно", "Прошу надати статистику щодо кількості поданих заяв та фактично виплачених компенсацій за програмою єВідновлення у [назва району] за поточний рік, а також причини відмов."},
	{"police", "🚔 Ефективність поліції", "Статистика розкриття злочинів у районі", "Прошу надати статистику щодо кількості зареєстрованих та переданих до суду проваджень за ст. [номер статті] ККУ протягом останніх 12 місяців на території [назва району]."},
	{"tcc", "👮‍♂️ Скарги на ТЦК", "Результати перевірок діяльності ТЦК", "Прошу надати інформацію про кількість зареєстрованих скарг на дії представників ТЦК та СП у регіоні за останній квартал та результати проведених службових перевірок за цими фактами."},
	{"salaries", "👔 Зарплати посадовців", "Виплати керівному складу органу", "Прошу надати помісячну деталізацію виплат (оклад, премії, надбавки) керівнику органу та його заступникам за поточний рік. Інформація є публічною згідно ст. 6 Закону № 2939-VI."},
	{"budget", "💰 Витрати на ремонти", "Використання бюджету на ремонтні роботи", "Прошу надати перелік договорів на ремонтні роботи, укладених вашим органом за останні 6 місяців, разом із актами виконаних робіт та копіями платіжних доручень."},
}

// ---------------------------------------------------------------------------
// Рейтинги органов и поиск по публичным запросам портала
// ---------------------------------------------------------------------------

// BodyStatsResponse — рейтинг органа для мини-приложения.
type BodyStatsResponse struct {
	Available  bool `json:"available"`
	Requests   int  `json:"requests"`
	Overdue    int  `json:"overdue"`
	OverduePct int  `json:"overduePct"`
	Successful int  `json:"successful"`
}

// RatingItem — строка публичного рейтинга органов (только агрегаты, без ПД).
type RatingItem struct {
	Rank             int     `json:"rank"`
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	Region           string  `json:"region,omitempty"`
	Index            int     `json:"index"` // «індекс відкритості» 0..100
	Badge            string  `json:"badge"` // 🟢 / 🟡 / 🔴
	Requests         int     `json:"requests"`
	Successful       int     `json:"successful"`
	Overdue          int     `json:"overdue"`
	OverduePct       int     `json:"overduePct"`
	AvgResponseHours float64 `json:"avgResponseHours,omitempty"` // наши данные, часов
	ResponseSample   int     `json:"responseSample,omitempty"`   // сколько наших ответов учтено
	PortalURL        string  `json:"portalUrl"`
}

// RatingResponse — ответ /api/rating.
type RatingResponse struct {
	Items     []RatingItem `json:"items"`
	Total     int          `json:"total"`   // органов в рейтинге (≥5 запитів)
	Covered   int          `json:"covered"` // по скольким органам есть данные
	Catalog   int          `json:"catalog"` // всего органов в каталоге
	FetchedAt string       `json:"fetchedAt,omitempty"`
}

// handleRating — GET /api/rating?sort=best|worst&q=&offset=&limit=:
// публичный лидерборд открытости (агрегированные счётчики публичных органов,
// ноль персональных данных). Поэтому эндпоинт НЕ обёрнут authMiddleware —
// страница рейтинга работает и в обычном браузере, без Telegram.
func (s *Server) handleRating(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}
	if s.ratings == nil || s.catalog == nil {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: RatingResponse{Items: []RatingItem{}}})
		return
	}
	sortParam := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	if sortParam != "worst" {
		sortParam = "best"
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	cat := s.catalog.Get()
	if cat == nil {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: RatingResponse{Items: []RatingItem{}}})
		return
	}
	rows, total := s.ratings.Leaderboard(cat.Bodies, dostup.LeaderOptions{
		Sort: sortParam, Query: q, Offset: offset, Limit: limit,
	})

	// Среднее время ответа — наши собственные данные (портал не отдаёт).
	// Показываем только для органов с ≥ 2 ответами: одна цифра — не статистика.
	timings := map[string]sentlog.BodyTiming{}
	if s.sentLog != nil {
		for name, t := range s.sentLog.AvgResponseHoursByBody() {
			if t.Count >= 2 {
				timings[name] = t
			}
		}
	}

	items := make([]RatingItem, 0, len(rows))
	for _, row := range rows {
		item := RatingItem{
			Rank:       row.Rank,
			Slug:       row.Slug,
			Name:       row.Name,
			Region:     row.Region,
			Index:      row.Index,
			Badge:      dostup.RatingBadge(row.Index),
			Requests:   row.Requests,
			Successful: row.Successful,
			Overdue:    row.Overdue,
			OverduePct: row.OverduePct,
			PortalURL:  dostup.BaseURL + "/body/" + row.Slug,
		}
		if t, ok := timings[strings.ToLower(row.Name)]; ok {
			item.AvgResponseHours = math.Round(t.Hours*10) / 10
			item.ResponseSample = t.Count
		}
		items = append(items, item)
	}

	resp := RatingResponse{
		Items:   items,
		Total:   total,
		Covered: s.ratings.Count(),
		Catalog: len(cat.Bodies),
	}
	if latest := s.ratings.LatestFetch(); !latest.IsZero() {
		resp.FetchedAt = latest.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: resp})
}

// handleBodyStats — GET /api/body-stats?name=<орган>: рейтинг ответов органа.
func (s *Server) handleBodyStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "name is required"})
		return
	}
	if s.dostup == nil || s.catalog == nil {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: BodyStatsResponse{Available: false}})
		return
	}

	// Слаг органа: привязка → каталог → локальный поиск
	slug := ""
	if b, ok := s.catalog.LookupBinding(name); ok {
		slug = b.Slug
	} else if hits := s.catalog.SearchLocal(name, 1); len(hits) > 0 {
		slug = hits[0].Slug
	}
	if slug == "" {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: BodyStatsResponse{Available: false}})
		return
	}

	st, err := s.dostup.BodyStatsCached(slug, true)
	if err != nil || st == nil {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: BodyStatsResponse{Available: false}})
		return
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: BodyStatsResponse{
		Available:  true,
		Requests:   st.Requests,
		Overdue:    st.Overdue,
		OverduePct: st.OverduePct(),
		Successful: st.Successful,
	}})
}

// handleSearchRequests — GET /api/search-requests?q=<запит>: публичный поиск.
func (s *Server) handleSearchRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "q is required"})
		return
	}
	searchURL := dostup.SearchURL(q)
	if s.dostup == nil {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]interface{}{
			"items":     []dostup.PublicRequest{},
			"searchURL": searchURL,
		}})
		return
	}
	items, err := s.dostup.SearchRequests(q)
	if err != nil {
		log.Printf("[WEB] search-requests %q: %v", q, err)
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]interface{}{
			"items":     []dostup.PublicRequest{},
			"searchURL": searchURL,
		}})
		return
	}
	if len(items) > 10 {
		items = items[:10]
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]interface{}{
		"items":     items,
		"searchURL": searchURL,
	}})
}
