package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"info-bot-go/internal/sentlog"
)

// ТЗ №9: публичная аналитика по темам — агрегаты без персональных данных.
func TestAnalyticsAggregates(t *testing.T) {
	dir := t.TempDir()
	sl, err := sentlog.New(dir)
	if err != nil {
		t.Fatalf("sentlog: %v", err)
	}
	defer sl.Close()

	// Три продовых запроса + один email-канал + один недоставленный черновик.
	// Недоставленный считаться НЕ должен.
	_ = sl.Append(sentlog.SentEntry{
		MessageID: "dostup:dsa", UserID: 1, Delivered: true, Channel: "dostup",
		Subject:    "Перелік електронних адрес судів для звернень громадян",
		DostupBody: "Державна судова адміністрація України", Date: "2026-08-27T20:35:09Z",
		AckAt: "2026-08-27T21:00:00Z", // только авто-подтверждение
	})
	_ = sl.Append(sentlog.SentEntry{
		MessageID: "dostup:moz", UserID: 2, Delivered: true, Channel: "dostup",
		Subject:    "Про надання статистичної інформації щодо серцево-судинних захворювань за перше півріччя",
		DostupBody: "Міністерство охорони здоров&#39;я", Date: "2026-08-29T11:37:52Z",
		ReplyReceivedAt: "2026-09-02T10:00:00Z", // отвечен
	})
	_ = sl.Append(sentlog.SentEntry{
		MessageID: "dostup:op", UserID: 3, Delivered: true, Channel: "dostup",
		Subject:    "Запит про надання інформації щодо військової посади Буданова К.О.",
		DostupBody: "Офіс Президента України", Date: "2026-08-30T16:44:35Z",
	})
	_ = sl.Append(sentlog.SentEntry{
		MessageID: "email:1", UserID: 1, Delivered: true,
		Subject:       "Стан доріг обласного значення",
		RecipientName: "Облдержадміністрація", Date: "2026-07-15T09:00:00Z",
	})
	_ = sl.Append(sentlog.SentEntry{
		MessageID: "draft:1", UserID: 1, Delivered: false,
		Subject: "Чернетка, яка не должна считаться",
	})

	s := newTestServer()
	s.sentLog = sl

	req := httptest.NewRequest(http.MethodGet, "/api/analytics", nil)
	rec := httptest.NewRecorder()
	s.handleAnalytics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/analytics: %d (body: %s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		OK   bool              `json:"ok"`
		Data AnalyticsResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !resp.OK {
		t.Fatal("ok=false")
	}
	d := resp.Data

	// Итоги: 4 доставленных (3 dostup + 1 email), 1 отвечен, 3 ждут, 1 только ack
	if d.Total != 4 {
		t.Errorf("Total=%d, want 4", d.Total)
	}
	if d.Answered != 1 {
		t.Errorf("Answered=%d, want 1", d.Answered)
	}
	if d.Awaiting != 3 {
		t.Errorf("Awaiting=%d, want 3", d.Awaiting)
	}
	if d.AckOnly != 1 {
		t.Errorf("AckOnly=%d, want 1", d.AckOnly)
	}

	// Темы: судова (1), здоров'я (1), оборона (1), інфраструктура (1)
	byID := map[string]AnalyticsTopicRow{}
	for _, row := range d.ByTopic {
		byID[row.ID] = row
	}
	for _, id := range []string{"justice", "health", "defense", "infra"} {
		if row, ok := byID[id]; !ok {
			t.Errorf("тема %s отсутствует: %+v", id, d.ByTopic)
		} else if row.Requests != 1 {
			t.Errorf("тема %s: requests=%d, want 1", id, row.Requests)
		} else if row.Share != 25.0 {
			t.Errorf("тема %s: share=%v, want 25", id, row.Share)
		}
	}
	if row, ok := byID["health"]; ok && row.Answered != 1 {
		t.Errorf("health answered=%d, want 1", row.Answered)
	}

	// Топ органов: 4 разных органа по 1 запросу; «здоров&#39;я» должен
	// анэскейпнуться в «здоров'я» и не отличаться от чистого написания
	if len(d.TopOrgans) != 4 {
		t.Fatalf("TopOrgans=%d, want 4: %+v", len(d.TopOrgans), d.TopOrgans)
	}
	found := false
	for _, o := range d.TopOrgans {
		if o.Name == "міністерство охорони здоров'я" {
			found = true
		}
	}
	if !found {
		t.Errorf("анэскейп органа не сработал: %+v", d.TopOrgans)
	}

	// Месяцы: 2026-07 (1), 2026-08 (3), по возрастанию
	if len(d.ByMonth) != 2 || d.ByMonth[0].Ym != "2026-07" || d.ByMonth[0].Requests != 1 || d.ByMonth[1].Requests != 3 {
		t.Errorf("ByMonth=%+v", d.ByMonth)
	}

	// Ни одного персонального идентификатора в JSON
	raw := rec.Body.String()
	for _, banned := range []string{"userId", "chatId", "messageId", "recipientEmail"} {
		if strings.Contains(raw, banned) {
			t.Errorf("в публичной аналитике утечка поля %q", banned)
		}
	}
}

// Пустой журнал — валидный пустой ответ, не 500.
func TestAnalyticsEmpty(t *testing.T) {
	dir := t.TempDir()
	sl, _ := sentlog.New(dir)
	defer sl.Close()

	s := newTestServer()
	s.sentLog = sl
	req := httptest.NewRequest(http.MethodGet, "/api/analytics", nil)
	rec := httptest.NewRecorder()
	s.handleAnalytics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("пустой журнал: %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK   bool              `json:"ok"`
		Data AnalyticsResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if resp.Data.Total != 0 || resp.Data.ByTopic == nil {
		t.Errorf("пустой ответ некорректен: %+v", resp.Data)
	}
}

// Сервер без журнала (sentLog=nil) — тоже не падает.
func TestAnalyticsNoSentLog(t *testing.T) {
	s := newTestServer() // без sentLog
	req := httptest.NewRequest(http.MethodGet, "/api/analytics", nil)
	rec := httptest.NewRecorder()
	s.handleAnalytics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("без журнала: %d", rec.Code)
	}
}
