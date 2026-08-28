package handlers

import (
	"strings"
	"testing"

	"info-bot-go/internal/session"
)

// Тест: письмо на портал подписывается именем пользователя,
// а не названием аккаунта «Громадський моніторинг».
func TestBuildDostupBodySignature(t *testing.T) {
	cases := []struct {
		name    string
		profile session.Profile
		wantSub string
	}{
		{"FullName как введено", session.Profile{FullName: "Іван Петренко"}, "З повагою,\nІван Петренко"},
		{"из частей профиля", session.Profile{FirstName: "Іван", LastName: "Петренко"}, "З повагою,\nПетренко Іван"},
		{"страховка без имени", session.Profile{}, "З повагою,\nГромадянин України"},
	}
	for _, tc := range cases {
		sess := &session.SessionData{Profile: tc.profile, Draft: Draft{Body: "Текст запиту."}}
		got := buildDostupBody(sess)
		if !strings.Contains(got, tc.wantSub) {
			t.Errorf("%s: подпись не найдена %q в:\n%s", tc.name, tc.wantSub, got)
		}
		if strings.Contains(got, "Громадський моніторинг") {
			t.Errorf("%s: письмо не должно подписываться аккаунтом портала", tc.name)
		}
		if !strings.Contains(got, "Текст запиту.") {
			t.Errorf("%s: тело запроса потеряно", tc.name)
		}
	}
}
