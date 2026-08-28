package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"info-bot-go/internal/ai"
	"info-bot-go/internal/config"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
)

// Server provides HTTP API for the Telegram Mini App.
type Server struct {
	cfg      *config.Config
	sessions *session.FileStore
	sentLog  *sentlog.SentLog
	gemini   *ai.Rotator
	botToken string
}

// NewServer creates a new API server.
func NewServer(cfg *config.Config, sessions *session.FileStore, sentLog *sentlog.SentLog, gemini *ai.Rotator) *Server {
	return &Server{
		cfg:      cfg,
		sessions: sessions,
		sentLog:  sentLog,
		gemini:   gemini,
		botToken: cfg.BotToken,
	}
}

// Start begins serving the API on the given address.
func (s *Server) Start(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/profile", s.cors(s.auth(s.handleProfile)))
	mux.HandleFunc("/api/requests", s.cors(s.auth(s.handleRequests)))
	mux.HandleFunc("/api/generate-template", s.cors(s.auth(s.handleGenerateTemplate)))

	log.Printf("[API] Starting HTTP server on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("[API] Server error: %v", err)
	}
}

// cors adds CORS headers for Vercel mini-app.
func (s *Server) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

// auth validates Telegram WebApp initData.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		initData := r.Header.Get("Authorization")
		if initData == "" {
			initData = r.URL.Query().Get("init_data")
		}
		if initData == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		userID, err := s.verifyAndExtractUser(initData)
		if err != nil {
			log.Printf("[API] auth failed: %v", err)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		// Store userID in context
		q := r.URL.Query()
		q.Set("_uid", fmt.Sprintf("%d", userID))
		r.URL.RawQuery = q.Encode()

		next(w, r)
	}
}

// verifyAndExtractUser validates Telegram initData HMAC and extracts user ID.
func (s *Server) verifyAndExtractUser(initData string) (int64, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return 0, fmt.Errorf("parse init data: %w", err)
	}

	hash := values.Get("hash")
	if hash == "" {
		return 0, fmt.Errorf("no hash in init data")
	}
	values.Del("hash")

	// Sort keys and build check string
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var checkStrings []string
	for _, k := range keys {
		checkStrings = append(checkStrings, fmt.Sprintf("%s=%s", k, values.Get(k)))
	}
	checkString := strings.Join(checkStrings, "\n")

	// Compute secret key: HMAC-SHA256("WebAppData", botToken)
	secretKey := hmac.New(sha256.New, []byte("WebAppData"))
	secretKey.Write([]byte(s.botToken))

	// Compute HMAC: HMAC-SHA256(secretKey, checkString)
	h := hmac.New(sha256.New, secretKey.Sum(nil))
	h.Write([]byte(checkString))
	computedHash := hex.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(computedHash), []byte(hash)) {
		return 0, fmt.Errorf("hash mismatch: got %s, computed %s", hash, computedHash)
	}

	// Check auth_date (not older than 24 hours)
	authDateStr := values.Get("auth_date")
	if authDateStr != "" {
		var authDate int64
		fmt.Sscanf(authDateStr, "%d", &authDate)
		if time.Now().Unix()-authDate > 86400 {
			return 0, fmt.Errorf("init data expired")
		}
	}

	// Extract user ID
	userJSON := values.Get("user")
	if userJSON == "" {
		return 0, fmt.Errorf("no user in init data")
	}

	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(userJSON), &user); err != nil {
		return 0, fmt.Errorf("parse user: %w", err)
	}

	return user.ID, nil
}

// jsonResp writes a JSON response.
func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(data)
}

// handleProfile returns the user's profile data.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("_uid")
	key := session.SessionKey(0)
	fmt.Sscanf(userID, "%d", &key)
	// Reconstruct key properly
	var uid int64
	fmt.Sscanf(userID, "%d", &uid)
	key = session.SessionKey(uid)

	sess, err := s.sessions.Get(key)
	if err != nil {
		sess = session.NewSessionData()
	}

	jsonResp(w, map[string]interface{}{
		"profile": sess.Profile,
		"email":   sess.Profile.Email,
		"shared":  sess.Draft.UseSharedMailbox || sess.Profile.Email == "",
	})
}

// handleRequests returns the user's sent requests with statuses.
func (s *Server) handleRequests(w http.ResponseWriter, r *http.Request) {
	var uid int64
	fmt.Sscanf(r.URL.Query().Get("_uid"), "%d", &uid)

	allEntries := s.sentLog.ListByUser(uid)

	type RequestCard struct {
		RecipientName   string `json:"recipientName"`
		RecipientEmail  string `json:"recipientEmail"`
		Subject         string `json:"subject"`
		Date            string `json:"date"`
		Status          string `json:"status"`
		DaysLeft        int    `json:"daysLeft,omitempty"`
		DeadlineDate    string `json:"deadlineDate,omitempty"`
		ReplyReceivedAt string `json:"replyReceivedAt,omitempty"`
	}

	cards := make([]RequestCard, 0, len(allEntries))
	for _, e := range allEntries {
		card := RequestCard{
			RecipientName:   e.RecipientName,
			RecipientEmail:  e.RecipientEmail,
			Subject:         e.Subject,
			Date:            e.Date,
			Status:          e.Status,
			ReplyReceivedAt: e.ReplyReceivedAt,
		}

		if e.Status == "" && e.ReplyReceivedAt == "" {
			card.Status = "pending"
			deadline := calcWorkingDaysDeadline(e.Date)
			remaining := time.Until(deadline)
			card.DaysLeft = int(remaining.Hours() / 24)
			card.DeadlineDate = deadline.Format("02.01.2006")
			if remaining <= 0 {
				card.Status = "expired"
				card.DaysLeft = 0
			}
		} else if e.Status == "replied" {
			card.Status = "replied"
		} else if e.Status == "bounced" {
			card.Status = "bounced"
		}

		cards = append(cards, card)
	}

	// Stats
	stats := map[string]int{
		"total":   len(cards),
		"replied": 0,
		"pending": 0,
		"expired": 0,
		"bounced": 0,
	}
	for _, c := range cards {
		stats[c.Status]++
	}

	jsonResp(w, map[string]interface{}{
		"requests": cards,
		"stats":    stats,
	})
}

// handleGenerateTemplate generates a FOI request template from a short description using AI.
func (s *Server) handleGenerateTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Description string `json:"description"`
		OrganName   string `json:"organName,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if req.Description == "" {
		http.Error(w, `{"error":"description is required"}`, http.StatusBadRequest)
		return
	}

	if s.gemini == nil || !s.gemini.Available() {
		// Fallback: generate a basic template without AI
		template := s.generateFallbackTemplate(req.Description, req.OrganName)
		jsonResp(w, map[string]interface{}{
			"template": template,
			"aiUsed":   false,
		})
		return
	}

	template, err := s.generateAITemplate(req.Description, req.OrganName)
	if err != nil {
		log.Printf("[API] AI template generation error: %v", err)
		template := s.generateFallbackTemplate(req.Description, req.OrganName)
		jsonResp(w, map[string]interface{}{
			"template": template,
			"aiUsed":   false,
		})
		return
	}

	jsonResp(w, map[string]interface{}{
		"template": template,
		"aiUsed":   true,
	})
}

// generateAITemplate uses Gemini to create a legally precise FOI request.
func (s *Server) generateAITemplate(description, organName string) (string, error) {
	prompt := fmt.Sprintf(`Ти — юрист-експерт з Закону України "Про доступ до публічної інформації" (№ 2939-VI). 
Згенеруй офіційний запит на публічну інформацію за таким описом:

Опис: %s
Орган: %s

Вимоги до запиту:
1. Обов'язково посилайся на конкретні статті Закону № 2939-VI:
   - ст. 1 (право на інформацію)
   - ст. 13 (спосіб подання запиту)
   - ст. 19 (електронний запит не потребує підпису)
   - ст. 20 (строк розгляду — 5 робочих днів)
   - ст. 22 (перенаправлення належному розпоряднику)
2. Формулюй чітко, юридично грамотно, без зайвих емоцій
3. Вкажи, що відповідь має бути надана в електронному вигляді
4. Якщо орган не вказано — залиши placeholder [НАЗВА ОРГАНУ]
5. Тільки текст запиту, без пояснень та коментарів

Формат: текст запиту українською мовою`, description, organName)

	result, err := s.gemini.GenerateText(prompt)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(result), nil
}

// generateFallbackTemplate creates a basic template when AI is unavailable.
func (s *Server) generateFallbackTemplate(description, organName string) string {
	organ := organName
	if organ == "" {
		organ = "[НАЗВА ОРГАНУ]"
	}

	now := time.Now().Format("02.01.2006")

	return fmt.Sprintf(`Відповідно до статті 1 Закону України «Про доступ до публічної інформації» (№ 2939-VI), кожен має право на доступ до публічної інформації.

Відповідно до статті 13 названого Закону, прошу надати наступну публічну інформацію:

%s

Відповідь прошу надати у строк, встановлений статтею 20 Закону (не пізніше п'яти робочих днів з дня отримання запиту), в електронному вигляді.

Відповідно до частини 2 статті 19 Закону, цей запит надсилається в електронній формі та не потребує власноручного підпису.

У разі, якщо Ви не володієте запитуваною інформацією, на підставі статті 22 Закону прошу направити цей запит належному розпоряднику з одночасним повідомленням про це запитувача.

%s`, description, now)
}

// calcWorkingDaysDeadline adds 5 working days to the send date.
func calcWorkingDaysDeadline(dateStr string) time.Time {
	t, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		t, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			return time.Now().Add(7 * 24 * time.Hour)
		}
	}
	daysAdded := 0
	current := t
	for daysAdded < 5 {
		current = current.AddDate(0, 0, 1)
		weekday := current.Weekday()
		if weekday != time.Saturday && weekday != time.Sunday {
			daysAdded++
		}
	}
	return current
}
