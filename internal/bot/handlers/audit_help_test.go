package handlers

import (
	tb "gopkg.in/telebot.v3"
	"info-bot-go/internal/config"
	"strings"
	"testing"
)

type auditHelpContext struct {
	tb.Context
	user  tb.User
	text  string
	calls int
}

func (c *auditHelpContext) Sender() *tb.User { return &c.user }
func (c *auditHelpContext) Send(what interface{}, _ ...interface{}) error {
	c.calls++
	c.text = what.(string)
	return nil
}
func TestAuditHelpActualHandlerOwnerBoundary(t *testing.T) {
	bot, err := tb.NewBot(tb.Settings{Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	NewHelpModule(&Deps{Bot: bot, Cfg: &config.Config{AdminID: 7}}).Register()
	for _, tc := range []struct {
		name  string
		id    int64
		owner bool
	}{{"owner", 7, true}, {"regular-user", 8, false}} {
		t.Run(tc.name, func(t *testing.T) {
			c := &auditHelpContext{user: tb.User{ID: tc.id}}
			if err := bot.Trigger("/help", c); err != nil {
				t.Fatal(err)
			}
			if c.calls != 1 {
				t.Fatalf("Send calls=%d", c.calls)
			}
			if strings.Contains(c.text, "*Для власника:*") {
				t.Fatal("literal Markdown in plain-text help")
			}
			for _, command := range []string{"/sync", "/backup", "Для власника:"} {
				if strings.Contains(c.text, command) != tc.owner {
					t.Errorf("owner-only %q visibility incorrect", command)
				}
			}
		})
	}
}
