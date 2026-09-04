package web

import (
        "encoding/json"
        "fmt"
        "log"
        "math"
        "net/http"
        "sort"
        "strconv"
        "strings"
        "time"

        "info-bot-go/internal/ai"
        "info-bot-go/internal/directory"
        "info-bot-go/internal/dostup"
        "info-bot-go/internal/sentlog"
        "info-bot-go/internal/session"
        "info-bot-go/internal/stars"
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

// AnalyzeRequest — запрос розбора ответа органа из мини-приложения.
type AnalyzeRequest struct {
        Organ   string `json:"organ"`
        Subject string `json:"subject"`
        Text    string `json:"text"`
}

// StarsStatusResponse — состояние монетизации и баланс пользователя.
type StarsStatusResponse struct {
        Enabled     bool `json:"enabled"`
        Price       int  `json:"price"`       // Stars за пакет
        Pack        int  `json:"pack"`        // кредитов в пакете
        FreeCredits int  `json:"freeCredits"` // стартовый бонус
        Balance     int  `json:"balance"`     // текущий баланс (0 при выключенной)
}

// StarsInvoiceResponse — ссылка на оплату пакета.
type StarsInvoiceResponse struct {
        Enabled bool   `json:"enabled"`
        Link    string `json:"link,omitempty"`
        Price   int    `json:"price"`
        Pack    int    `json:"pack"`
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

// handleDeleteMyData — ТЗ №5: полное удаление персональных данных
// пользователя (ЗУ №2297-IV). Требует подпись Telegram (authMiddleware
// стоит перед этим обработчиком в маршруте). Удаление идёт под
// сессионной блокировкой — даже если пользователь одновременно пишет
// боту, данные не «перетрутся» и частичного удаления не случится.
func (s *Server) handleDeleteMyData(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
                return
        }

        userID := getUserID(r)
        if userID == 0 {
                writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
                return
        }

        key := session.SessionKey(userID)
        s.sessions.LockSession(key)
        defer s.sessions.UnlockSession(key)

        removed := 0
        if s.sentLog != nil {
                n, err := s.sentLog.DeleteByUser(userID)
                if err != nil {
                        log.Printf("[WEB] delete-my-data: журнал, user=%d: %v", userID, err)
                        writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Err: "failed to delete history"})
                        return
                }
                removed = n
        }
        s.sessions.Delete(key)

        log.Printf("[WEB] delete-my-data: user=%d видалено (записів: %d)", userID, removed)
        writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]interface{}{
                "deleted":        true,
                "removedEntries": removed,
        }})
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

// ---------------------------------------------------------------------------
// AI-разбор ответа органа (ТЗ №6: вкладка «Розбір» в мини-приложении)
// ---------------------------------------------------------------------------

// handleAnalyze — POST /api/analyze: AI-разбор ответа органа.
// Тело: {organ?, subject?, text}. Ответ — вердикт ai.RefusalAnalysis.
//
// Защита (по слоям):
//  1. IP-лимит anl (6/мин, middleware — до подписи);
//  2. подпись Telegram (authMiddleware);
//  3. монетизация: при STARS_ENABLED списывается 1 кредит (при сбое
//     модели возвращается), при выключенной — часовой лимит 6/час
//     (как в боте; защита AI-квоты).
func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
                return
        }

        userID := getUserID(r)
        if userID == 0 {
                writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
                return
        }

        var req AnalyzeRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "invalid request body"})
                return
        }
        req.Text = strings.TrimSpace(req.Text)
        if len(req.Text) < 40 {
                writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "text too short"})
                return
        }
        // Рун-безопасное усечение: байтовая нарезка ломала бы кириллицу.
        req.Text = ai.TruncateReplyText(req.Text)

        // AI не настроен — сразу честный 503, кредиты не трогаем.
        if s.gemini == nil {
                writeJSON(w, http.StatusServiceUnavailable, APIResponse{OK: false, Err: "AI not configured"})
                return
        }

        // Монетизация или часовой лимит бесплатных разборов.
        spendCredit := s.cfg.StarsEnabled && s.stars != nil
        if spendCredit {
                s.stars.EnsureWelcome(userID, s.cfg.StarsFreeCredits)
                if !s.stars.Spend(userID, 1) {
                        writeJSON(w, http.StatusPaymentRequired, APIResponse{OK: false, Err: "no credits"})
                        return
                }
        } else if s.analyzeUsers != nil && !s.analyzeUsers.Allow("u:"+strconv.FormatInt(userID, 10)) {
                writeJSON(w, http.StatusTooManyRequests, APIResponse{OK: false, Err: "hourly limit"})
                return
        }

        analysis, err := s.gemini.AnalyzeRefusal(
                strings.TrimSpace(req.Organ),
                strings.TrimSpace(req.Subject),
                req.Text,
                nil, // фото — только через бота (мини-апп шлёт текст)
        )
        if err != nil {
                if spendCredit {
                        _ = s.stars.Add(userID, 1) // сбой модели — кредит возвращаем
                }
                log.Printf("[WEB] analyze error for user %d: %v", userID, err)
                writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Err: "AI analysis failed"})
                return
        }

        log.Printf("[WEB] analyze user=%d organ=%q type=%s next=%s",
                userID, req.Organ, analysis.Type, analysis.NextStep)
        writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: analysis})
}

// ---------------------------------------------------------------------------
// Монетизация Telegram Stars (каркас, выключена по умолчанию)
// ---------------------------------------------------------------------------

// handleStarsStatus — GET /api/stars/status: состояние монетизации и баланс.
// При выключенной монетизации отдаёт enabled=false (фронтенд прячет оплату).
func (s *Server) handleStarsStatus(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
                return
        }
        userID := getUserID(r)
        if userID == 0 {
                writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
                return
        }
        resp := StarsStatusResponse{
                Enabled:     s.cfg.StarsEnabled && s.stars != nil,
                Price:       s.cfg.StarsAnalyzePrice,
                Pack:        s.cfg.StarsAnalyzePack,
                FreeCredits: s.cfg.StarsFreeCredits,
        }
        if resp.Enabled {
                s.stars.EnsureWelcome(userID, s.cfg.StarsFreeCredits)
                resp.Balance = s.stars.Balance(userID)
        }
        writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: resp})
}

// handleStarsInvoice — POST /api/stars/invoice: ссылка на оплату пакета.
// При выключенной монетизации — 403 с enabled=false (кнопок не должно быть).
func (s *Server) handleStarsInvoice(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
                return
        }
        userID := getUserID(r)
        if userID == 0 {
                writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
                return
        }
        if !s.cfg.StarsEnabled || s.stars == nil || s.starsClient == nil {
                writeJSON(w, http.StatusForbidden, APIResponse{OK: false, Err: "payments disabled"})
                return
        }
        link, err := s.starsClient.CreateInvoiceLink(
                fmt.Sprintf("Пакет: %d AI-розборів", s.cfg.StarsAnalyzePack),
                "Розбір відповідей органів: тип, законність за ЗУ №2939-VI, порушені статті та готовий документ.",
                stars.BuildPayload(userID, s.cfg.StarsAnalyzePack),
                s.cfg.StarsAnalyzePrice,
        )
        if err != nil {
                log.Printf("[WEB] stars invoice user=%d: %v", userID, err)
                writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Err: "invoice failed"})
                return
        }
        writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: StarsInvoiceResponse{
                Enabled: true,
                Link:    link,
                Price:   s.cfg.StarsAnalyzePrice,
                Pack:    s.cfg.StarsAnalyzePack,
        }})
}

// ---------------------------------------------------------------------------
// Публичная аналитика по темам запросов (ТЗ №9, фаза 3.11 плана развития)
// ---------------------------------------------------------------------------

// AnalyticsTopicRow — агрегат по одной теме.
type AnalyticsTopicRow struct {
        ID       string  `json:"id"`
        Emoji    string  `json:"emoji"`
        Title    string  `json:"title"`
        Requests int     `json:"requests"`
        Answered int     `json:"answered"`
        Share    float64 `json:"share"` // доля от общего числа запросов, %
}

// AnalyticsOrganRow — самый запрашиваемый орган (агрегат, без ПД).
type AnalyticsOrganRow struct {
        Name     string `json:"name"`
        Requests int    `json:"requests"`
        Answered int    `json:"answered"`
}

// AnalyticsMonthRow — запросы за месяц (тренд).
type AnalyticsMonthRow struct {
        Ym       string `json:"ym"` // «2026-08»
        Requests int    `json:"requests"`
}

// AnalyticsResponse — ответ GET /api/analytics. Только агрегаты:
// ни имён, ни идентификаторов пользователей, ни текстов запросов —
// темы и счётчики. Эндпоинт публичный, как и /api/rating.
type AnalyticsResponse struct {
        Total     int                 `json:"total"`
        Answered  int                 `json:"answered"`
        Awaiting  int                 `json:"awaiting"`
        AckOnly   int                 `json:"ackOnly"` // только авто-подтверждение, ответа ещё нет
        ByTopic   []AnalyticsTopicRow `json:"byTopic"`
        TopOrgans []AnalyticsOrganRow `json:"topOrgans"`
        ByMonth   []AnalyticsMonthRow `json:"byMonth"`
        FetchedAt string              `json:"fetchedAt,omitempty"`
}

// handleAnalytics — GET /api/analytics: агрегированная аналитика запросов
// по темам (ТЗ №9). Публичный: персональных данных нет, только счётчики.
func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
                return
        }
        resp := AnalyticsResponse{ByTopic: []AnalyticsTopicRow{}, TopOrgans: []AnalyticsOrganRow{}, ByMonth: []AnalyticsMonthRow{}}
        if s.sentLog != nil {
                entries := s.sentLog.ListAll()

                topicCount := map[string]int{}
                topicAnswered := map[string]int{}
                organCount := map[string]int{}
                organAnswered := map[string]int{}
                monthCount := map[string]int{}
                monthOrder := []string{}

                for _, e := range entries {
                        // Пропускаем черновики/недоставленные: считаем только реально ушедшие
                        if !e.Delivered {
                                continue
                        }
                        answered := e.ReplyReceivedAt != ""
                        resp.Total++
                        if answered {
                                resp.Answered++
                        } else if e.AckAt != "" {
                                resp.AckOnly++
                        }

                        // Тема: предмет + орган дают достаточно контекста для классификации
                        organ := htmlUnescapeString(e.DostupBody)
                        if organ == "" {
                                organ = htmlUnescapeString(e.RecipientName)
                        }
                        topic := dostup.ClassifyTopic(e.Subject + " " + organ)
                        topicCount[topic.ID]++
                        if answered {
                                topicAnswered[topic.ID]++
                        }

                        if name := strings.TrimSpace(organ); name != "" {
                                key := strings.ToLower(name)
                                organCount[key]++
                                if answered {
                                        organAnswered[key]++
                                }
                        }

                        // Месяц: первые 7 символов даты («2026-08»), любой формат ISO
                        if d := e.Date; len(d) >= 7 {
                                ym := d[:7]
                                if _, seen := monthCount[ym]; !seen {
                                        monthOrder = append(monthOrder, ym)
                                }
                                monthCount[ym]++
                        }
                }
                resp.Awaiting = resp.Total - resp.Answered

                // Темы: все известные + встретившиеся, сортировка по убыванию запросов
                for _, topic := range dostup.Topics() {
                        n := topicCount[topic.ID]
                        if n == 0 {
                                continue
                        }
                        share := 0.0
                        if resp.Total > 0 {
                                share = math.Round(float64(n)*1000/float64(resp.Total)) / 10
                        }
                        resp.ByTopic = append(resp.ByTopic, AnalyticsTopicRow{
                                ID: topic.ID, Emoji: topic.Emoji, Title: topic.Title,
                                Requests: n, Answered: topicAnswered[topic.ID], Share: share,
                        })
                }

                // Топ органов: сортировка по числу запросов, берём 5
                type organRow struct {
                        name               string
                        requests, answered int
                }
                var organs []organRow
                for key, n := range organCount {
                        organs = append(organs, organRow{name: key, requests: n, answered: organAnswered[key]})
                }
                for i := 1; i < len(organs); i++ {
                        for j := i; j > 0 && organs[j].requests > organs[j-1].requests; j-- {
                                organs[j], organs[j-1] = organs[j-1], organs[j]
                        }
                }
                for i, o := range organs {
                        if i >= 5 {
                                break
                        }
                        resp.TopOrgans = append(resp.TopOrgans, AnalyticsOrganRow{Name: o.name, Requests: o.requests, Answered: o.answered})
                }

                // Месяцы: последние 6, по возрастанию (для мини-графика)
                sort.Strings(monthOrder)
                if len(monthOrder) > 6 {
                        monthOrder = monthOrder[len(monthOrder)-6:]
                }
                for _, ym := range monthOrder {
                        resp.ByMonth = append(resp.ByMonth, AnalyticsMonthRow{Ym: ym, Requests: monthCount[ym]})
                }
        }
        resp.FetchedAt = time.Now().UTC().Format(time.RFC3339)
        writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: resp})
}

// htmlUnescapeString — безопасный анэскейп HTML-сущностей в названиях органов
// (старые записи журнала могли сохранить «здоров&#39;я» вместо «здоров'я»).
func htmlUnescapeString(s string) string {
        if !strings.Contains(s, "&") {
                return s
        }
        repl := strings.NewReplacer(
                "&#39;", "'", "&quot;", `"`, "&amp;", "&", "&lt;", "<", "&gt;", ">",
                "&#x27;", "'", "&nbsp;", " ",
        )
        return repl.Replace(s)
}
