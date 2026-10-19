package dostup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Synthetic local HTTP fixtures exercise the actual parser and classifier.
// No portal, Telegram, SMTP, or AI service is contacted.
func TestAuditGetRequestStatusLettersBeforeClassification(t *testing.T) {
	for _, tc := range []struct {
		name, status, body string
		want               ReplyKind
	}{
		{"awaiting-with-answer", "waiting_classification", "Надаємо перелік документів за вашим запитом.", ReplySubstantive},
		{"awaiting-with-ack", "waiting_classification", "Ваш запит зареєстровано. Буде розглянуто у встановлений термін.", ReplyAcknowledgement},
		{"refusal-with-answer", "rejected", "У наданні інформації відмовлено на підставі статті 22.", ReplySubstantive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/request/synthetic" {
					t.Errorf("unexpected path: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				fmt.Fprintf(w, `<div id="request_status" class="request-status-message request-status-message--%s"></div><div id="incoming-42"><span class="correspondence__header__author">Synthetic authority</span><div class="correspondence_text"><p>%s</p><p class="event_actions">actions</p></div></div>`, tc.status, tc.body)
			}))
			defer srv.Close()
			c := newTestClient(srv.URL)
			st, err := c.GetRequestStatus("synthetic")
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != tc.status || !st.HasResponse || st.IncomingCount != 1 || st.LastIncomingID != "42" {
				t.Fatalf("wrong parsed status: %+v", st)
			}
			if !ResponseArrived(st.Status) {
				t.Fatalf("response lost for %s", st.Status)
			}
			if got := ClassifyReply(st.ResponseExcerpt); got != tc.want {
				t.Fatalf("ClassifyReply(%q) = %v, want %v", st.ResponseExcerpt, got, tc.want)
			}
		})
	}
}
