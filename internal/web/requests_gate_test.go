package web

import (
	"encoding/json"
	"info-bot-go/internal/dostup"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCreateRequestClassificationGateHonestNoPublish(t *testing.T) {
	var posts atomic.Int32
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
			http.Error(w, "unexpected write", 500)
			return
		}
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<a href="/sign_out">Synthetic logged-in session</a>`))
		case "/new/synthetic":
			w.Write([]byte(`First, did your other requests succeed?`))
		default:
			t.Errorf("unexpected local path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer portal.Close()
	s := newTestServer()
	s.dostup = dostup.New("")
	s.dostup.BaseURL = portal.URL
	var err error
	s.sessions, err = session.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.sessions.Close()
	s.sentLog, err = sentlog.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.sentLog.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/requests", strings.NewReader(`{"bodySlug":"synthetic","recipientName":"Fixture authority","subject":"Fixture subject","body":"Fixture request","signature":"Fixture"}`))
	req.Header.Set("X-Init-Data", signInitData(t, 100, s.cfg.BotToken))
	rec := httptest.NewRecorder()
	s.authMiddleware(s.handleCreateRequest)(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK || !strings.Contains(resp.Err, "класифікувати попередні відповіді") || !strings.Contains(resp.Err, "новий запит не надіслано") || strings.Contains(resp.Err, "уточніть тему") {
		t.Fatalf("misleading gate response: %+v", resp)
	}
	if posts.Load() != 0 || len(s.sentLog.ListAll()) != 0 {
		t.Fatal("blocked request must not be published or recorded as sent")
	}
}
