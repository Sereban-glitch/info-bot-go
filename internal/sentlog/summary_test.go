package sentlog

import "testing"

func TestSummarySeparatesDeliveryResponseAndClassification(t *testing.T) {
	entries := []SentEntry{
		{Delivered: true, Status: "bounced", ReplyReceivedAt: "legacy-bounce-timestamp"},
		{Delivered: true, LastStatus: "error_message"},
		{Delivered: true, Channel: "dostup", LastStatus: "waiting_classification", ReplyReceivedAt: "answer"},
		{Delivered: true, Channel: "dostup", LastStatus: "rejected", ReplyReceivedAt: "answer"},
		{Delivered: true, AckAt: "ack"},
		{Delivered: true},
		{Delivered: true, LastStatus: "user_withdrawn"},
		{Delivered: true, LastStatus: "user_withdrawn", ReplyReceivedAt: "answer-before-withdrawal"},
		{Delivered: false, ReplyReceivedAt: "unconfirmed"},
	}
	got := Summarize(entries)
	want := Summary{Total: 8, Answered: 3, Awaiting: 2, AckOnly: 1, Bounced: 2, Withdrawn: 1, Undelivered: 1, AwaitingClassification: 1}
	if got != want {
		t.Fatalf("Summarize = %+v, want %+v", got, want)
	}
	if got.Total != got.Answered+got.Awaiting+got.Bounced+got.Withdrawn {
		t.Fatal("inconsistent population")
	}
}

func TestSummaryActualLegacyBounce(t *testing.T) {
	sl, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sl.Close()
	if err := sl.Append(SentEntry{MessageID: "synthetic", RecipientEmail: "fixture@example.invalid", Delivered: true}); err != nil {
		t.Fatal(err)
	}
	if !sl.MarkBounced("fixture@example.invalid") {
		t.Fatal("bounce not recorded")
	}
	s := Summarize(sl.ListAll())
	if s.Answered != 0 || s.Bounced != 1 || s.Awaiting != 0 {
		t.Fatalf("bounce counted as reply: %+v", s)
	}
}
