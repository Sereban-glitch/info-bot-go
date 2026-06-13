package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"info-bot-go/internal/directory"
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
		if initData == "" {
			initData = r.URL.Query().Get("init_data")
		}

		if initData != "" {
			userID, ok := ValidateInitData(initData, s.cfg.BotToken)
			if ok && userID > 0 {
				r.Header.Set("X-User-ID", fmt.Sprintf("%d", userID))
				log.Printf("[AUTH] HMAC OK: user_id=%d path=%s", userID, r.URL.Path)
				next(w, r)
				return
			}
		}

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
