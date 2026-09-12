package web

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"info-bot-go/internal/dostup"
	"info-bot-go/internal/moderation"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
)

// createRequestReq — дані нового інформаційного запиту із застосунка.
// Логіка повторює handleSubmit бота, але без Telegram-флоу: один POST замість
// десяти кроків діалогу. Скринінг ТЗ №10 і заведення гілки — ті ж.
type createRequestReq struct {
	BodySlug      string `json:"bodySlug"`
	RecipientName string `json:"recipientName"`
	Subject       string `json:"subject"`
	Body          string `json:"body"`
	Signature     string `json:"signature"`
}

// createRequestResp — результат публікації запиту.
type createRequestResp struct {
	OK       bool   `json:"ok"`
	Slug     string `json:"slug"`
	URL      string `json:"url"`
	Deadline string `json:"deadline"`
}

// bodyItem — знайдений розпорядник порталу для /api/bodies?q=.
type bodyItem struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// followUpReq — текст уточнення (допис) до опублікованого запиту.
type followUpReq struct {
	Slug string `json:"slug"`
	Text string `json:"text"`
}

type followUpResp struct {
	OK  bool   `json:"ok"`
	URL string `json:"url"`
}

// handleCreateRequest — POST /api/requests: подання запиту на портал
// від імені користувача застосунка.
func (s *Server) handleCreateRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}
	userID := getUserID(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
		return
	}
	if s.dostup == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{OK: false, Err: "канал порталу не налаштований"})
		return
	}

	var req createRequestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "неправильний JSON"})
		return
	}
	req.BodySlug = strings.TrimSpace(req.BodySlug)
	req.RecipientName = strings.TrimSpace(req.RecipientName)
	req.Subject = strings.TrimSpace(req.Subject)
	req.Body = strings.TrimSpace(req.Body)
	req.Signature = strings.TrimSpace(req.Signature)
	if req.BodySlug == "" || req.RecipientName == "" || req.Subject == "" || req.Body == "" || req.Signature == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "заповніть усі поля"})
		return
	}
	if len([]rune(req.Subject)) > 150 {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "тема занадто довга (до 150 символів)"})
		return
	}

	var sess *session.SessionData
	if v, err := s.sessions.Get(session.SessionKey(userID)); err == nil && v != nil {
		sess = v
	}
	data := buildRequestBody(sess, req)

	// ТЗ №10: антиспровокаційний скринінг — як у боті. Гострі запити через
	// спільний акаунт порталу не йдуть: спрямовуємо в чат-бота (/new).
	if s.cfg != nil && s.cfg.ModerationEnabled {
		if v := moderation.Check(req.Subject, data, req.RecipientName); v.Hold {
			log.Printf("[WEB] moderation hold: user=%d subject=%q", userID, req.Subject)
			writeJSON(w, http.StatusForbidden, APIResponse{OK: false, Err: "цей запит потребує перевірки — надішліть його через чат-бота командою /new"})
			return
		}
	}

	if err := s.dostup.EnsureSession(); err != nil {
		log.Printf("[WEB] dostup session: %v", err)
		writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Err: "не вдалося зєднатися з порталом — спробуйте за хвилину"})
		return
	}

	info, err := s.dostup.SubmitRequest(req.BodySlug, req.Subject, data)
	if err != nil {
		switch {
		case errors.Is(err, dostup.ErrNeedsClassification):
			log.Printf("[WEB] dostup needs-classification: user=%d subject=%q err=%v", userID, req.Subject, err)
			writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Err: "портал не прийняв запит: уточніть тему"})
		case errors.Is(err, dostup.ErrRateLimited):
			writeJSON(w, http.StatusTooManyRequests, APIResponse{OK: false, Err: "портал обмежує частоту — спробуйте за 3–5 хвилин"})
		case errors.Is(err, dostup.ErrInvalidResponse):
			log.Printf("[WEB] dostup unconfirmed submit: user=%d subject=%q err=%v", userID, req.Subject, err)
			s.recordSent(userID, req, "", "")
			writeJSON(w, http.StatusOK, createRequestResp{OK: true, Slug: req.BodySlug, URL: "", Deadline: workDeadline(5)})
		default:
			log.Printf("[WEB] dostup submit error: user=%d err=%v", userID, err)
			writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Err: "портал не відповів — спробуйте ще раз"})
		}
		return
	}

	if info == nil || info.Slug == "" {
		log.Printf("[WEB] dostup empty info: user=%d", userID)
		writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Err: "портал не підтвердив запис — перевірте «Мої запити» за хвилину"})
		return
	}

	s.recordSent(userID, req, info.Slug, info.URL)
	deadline := workDeadline(5)
	if s.events != nil {
		s.events.Notify(userID, `{"type":"created"}`)
	}
	writeJSON(w, http.StatusCreated, createRequestResp{OK: true, Slug: info.Slug, URL: info.URL, Deadline: deadline})
}

// recordSent пише відправку в журнал і заводить гілку — те саме,
// що робить handleSubmit бота після публікації.
func (s *Server) recordSent(userID int64, req createRequestReq, slug, url string) {
	now := time.Now()
	msgID := "dostup:" + slug
	if s.sentLog != nil {
		_ = s.sentLog.Append(sentlog.SentEntry{
			MessageID:      msgID,
			UserID:         userID,
			RecipientName:  req.RecipientName,
			RecipientEmail: "dostup.org.ua",
			Subject:        req.Subject,
			Date:           now.Format(time.RFC3339),
			Channel:        "dostup",
			URL:            url,
			DostupBody:     req.RecipientName,
			Delivered:      true,
		})
	}
	if slug != "" && s.followUps != nil {
		s.followUps.Upsert(userID, session.FollowUpThread{
			Slug:    slug,
			Subject: req.Subject,
			Organ:   req.RecipientName,
			URL:     url,
		})
	}
}

// handleFollowUp — POST /api/followups: допис до запиту (аналог FollowUp у боті).
func (s *Server) handleFollowUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}
	userID := getUserID(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
		return
	}
	if s.dostup == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{OK: false, Err: "канал порталу не налаштований"})
		return
	}

	var req followUpReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "неправильний JSON"})
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	req.Text = strings.TrimSpace(req.Text)
	if req.Slug == "" || req.Text == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "слаг і текст — обовязкові"})
		return
	}

	if err := s.dostup.EnsureSession(); err != nil {
		log.Printf("[WEB] followup session: %v", err)
		writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Err: "не вдалося зєднатися з порталом — спробуйте за хвилину"})
		return
	}

	link, err := s.dostup.SubmitFollowUp(req.Slug, req.Text)
	if err != nil {
		if errors.Is(err, dostup.ErrRateLimited) {
			writeJSON(w, http.StatusTooManyRequests, APIResponse{OK: false, Err: "портал обмежує частоту — спробуйте за 3–5 хвилин"})
			return
		}
		log.Printf("[WEB] followup error: user=%d slug=%s err=%v", userID, req.Slug, err)
		writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Err: "портал не відповів на допис — спробуйте ще раз"})
		return
	}

	if s.followUps != nil {
		s.followUps.MarkFollowUpSent(userID, req.Slug, time.Now().Format(time.RFC3339))
	}
	if s.events != nil {
		s.events.Notify(userID, `{"type":"followup"}`)
	}
	writeJSON(w, http.StatusOK, followUpResp{OK: true, URL: link})
}

// handleBodies — GET /api/bodies?q=: пошук органа на порталі. Повертає slug+name.
func (s *Server) handleBodies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}
	userID := getUserID(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
		return
	}
	if s.dostup == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{OK: false, Err: "канал порталу не налаштований"})
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: []bodyItem{}})
		return
	}

	if err := s.dostup.EnsureSession(); err != nil {
		log.Printf("[WEB] bodies session: %v", err)
		writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Err: "не вдалося зєднатися з порталом"})
		return
	}

	bodies, err := s.dostup.SearchBodies(q)
	if err != nil {
		log.Printf("[WEB] bodies search: %v", err)
		writeJSON(w, http.StatusBadGateway, APIResponse{OK: false, Err: "пошук не відповів"})
		return
	}

	items := make([]bodyItem, 0, len(bodies))
	for _, b := range bodies {
		items = append(items, bodyItem{Slug: b.Slug, Name: b.Name})
	}
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: items})
}

// buildRequestBody збирає текст запиту — репліка buildDostupBody бота.
func buildRequestBody(sess *session.SessionData, req createRequestReq) string {
	var b strings.Builder
	b.WriteString("На підставі статей 1, 13, 19, 20 Закону України «Про доступ до публічної інформації» від 13 січня 2011 року № 2939-VI, які надають право звертатись із запитами до розпорядників інформації, прошу надати наступну інформацію.\n\n")
	b.WriteString(req.Body)
	b.WriteString("\n\nЗ повагою,\n")
	b.WriteString(req.Signature)
	if sess != nil {
		if sess.Profile.Email != "" && strings.Contains(sess.Profile.Email, "@") {
			b.WriteString("\nВідповідь прошу надіслати електронною поштою: " + sess.Profile.Email)
		}
		if sess.Profile.PostalAddress != "" {
			b.WriteString("\nПоштова адреса: " + sess.Profile.PostalAddress)
		}
	}
	b.WriteString("\n" + time.Now().Format("02.01.2006"))
	return b.String()
}

// workDeadline обчислює дату дедлайна через n робочих днів.
func workDeadline(n int) string {
	d := time.Now()
	added := 0
	for added < n {
		d = d.AddDate(0, 0, 1)
		switch d.Weekday() {
		case time.Saturday, time.Sunday:
		default:
			added++
		}
	}
	return d.Format("02.01.2006")
}
