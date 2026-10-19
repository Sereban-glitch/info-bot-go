package handlers

import (
	"fmt"
	tb "gopkg.in/telebot.v3"
	"info-bot-go/internal/config"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/stats"
	"strings"
	"testing"
)

func TestAuditStatsActualHandlerConsistentPopulation(t *testing.T) {
	sl, err := sentlog.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sl.Close()
	for _, e := range []sentlog.SentEntry{
		{MessageID: "answer", UserID: 8, Delivered: true, Channel: "dostup", LastStatus: "waiting_classification", ReplyReceivedAt: "answer"},
		{MessageID: "bounce", UserID: 8, Delivered: true, Status: "bounced", ReplyReceivedAt: "legacy"},
		{MessageID: "draft", UserID: 8, Delivered: false},
		{MessageID: "ack", UserID: 8, Delivered: true, AckAt: "ack"},
		{MessageID: "other-user", UserID: 9, Delivered: true},
	} {
		if err := sl.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	gs, err := stats.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gs.SyncCounts(999, 999)
	bot, err := tb.NewBot(tb.Settings{Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	NewStatsModule(&Deps{Bot: bot, Cfg: &config.Config{AdminID: 7}, SentLog: sl, Stats: gs}).Register()
	for _, id := range []int64{7, 8} {
		c := &auditHelpContext{user: tb.User{ID: id}}
		if err := bot.Trigger("/stats", c); err != nil {
			t.Fatal(err)
		}
		if c.calls != 1 {
			t.Fatalf("Send calls=%d", c.calls)
		}
		total, awaiting := 3, 1
		if id == 7 {
			total, awaiting = 4, 2
		}
		for _, want := range []string{fmt.Sprintf("Надіслано (за журналом): %d", total), "Відповідей по суті: 1", fmt.Sprintf("Очікують відповіді по суті: %d", awaiting), "Помилок доставки: 1", "Без підтвердження надсилання: 1", "За журналом ще не класифіковано: 1"} {
			if !strings.Contains(c.text, want) {
				t.Errorf("user=%d missing %q: %s", id, want, c.text)
			}
		}
		if strings.Contains(c.text, "999") {
			t.Error("stale counters leaked")
		}
		if id == 7 && !strings.Contains(c.text, "Інтервал синхронізації: 20 хв") {
			t.Error("effective interval missing")
		}
	}
}
