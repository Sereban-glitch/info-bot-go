package handlers_test

import (
	"info-bot-go/internal/dostup"
	"testing"
)

// These tests invoke production functions, not Telegram or live portal services.
func TestAuditResponseArrivalIsNotSuccessRating(t *testing.T) {
	for _, status := range []string{"waiting_classification", "rejected", "not_held", "successful", "partially_successful"} {
		t.Run(status, func(t *testing.T) {
			if !dostup.ResponseArrived(status) {
				t.Fatalf("ResponseArrived(%q) = false", status)
			}
		})
	}
	for _, status := range []string{"waiting_response", "error_message", "unknown"} {
		if dostup.ResponseArrived(status) {
			t.Errorf("ResponseArrived(%q) = true", status)
		}
	}
	idx, rated := dostup.OpennessIndex(&dostup.BodyStats{Requests: 4, Successful: 4})
	if rated || idx != 0 {
		t.Fatalf("four successful requests should be unrated: (%d,%v)", idx, rated)
	}
	idx, rated = dostup.OpennessIndex(&dostup.BodyStats{Requests: 5, Successful: 0})
	if !rated || idx != 0 {
		t.Fatalf("five requests with zero successes should be rated zero: (%d,%v)", idx, rated)
	}
}
func TestAuditRefusalAndAcknowledgementAreDistinct(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       dostup.ReplyKind
	}{
		{"refusal", "У наданні запитуваної інформації відмовлено на підставі статті 22.", dostup.ReplySubstantive},
		{"acknowledgement", "Ваш запит зареєстровано. Буде розглянуто у встановлений термін.", dostup.ReplyAcknowledgement},
		{"empty", "", dostup.ReplyUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dostup.ClassifyReply(tc.text); got != tc.want {
				t.Fatalf("ClassifyReply=%v,want %v", got, tc.want)
			}
		})
	}
}
func TestAuditRefusalLabel(t *testing.T) {
	if got := dostup.StatusLabel("rejected"); got != "❌ У задоволенні відмовлено" {
		t.Fatalf("StatusLabel(rejected)=%q", got)
	}
}

// Synthetic arithmetic only: this does not prove how a live portal feed counts
// waiting_classification, incoming attachments, or requester assessments.
func TestAuditOpennessIndexDenominatorArithmetic(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		requests, successful, want int
	}{
		{"ten-of-ten", 10, 10, 100}, {"ten-of-eleven", 11, 10, 91},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, rated := dostup.OpennessIndex(&dostup.BodyStats{Requests: tc.requests, Successful: tc.successful})
			if !rated || got != tc.want {
				t.Fatalf("OpennessIndex=(%d,%v),want (%d,true)", got, rated, tc.want)
			}
		})
	}
}
