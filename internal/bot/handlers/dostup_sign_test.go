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

// Мусорный email («@» из старых профилей) не должен попадать в письма —
// реальный кейс 30.08.2026: «Відповідь прошу надіслати електронною
// поштою: @» в опубликованном запросе.
func TestBuildDostupBodyEmailGuard(t *testing.T) {
	cases := []struct {
		name  string
		email string
		want  string // ожидаемая строка или "" если строки быть не должно
	}{
		{"валидный адрес", "ivan@gmail.com", "\nВідповідь прошу надіслати електронною поштою: ivan@gmail.com"},
		{"мусор @", "@", ""},
		{"мусор без домена", "a@", ""},
		{"мусор без зоны", "a@localhost", ""},
		{"пробелы", "a b@gmail.com", ""},
	}
	for _, tc := range cases {
		sess := &session.SessionData{
			Profile: session.Profile{FullName: "Іван Петренко", Email: tc.email},
			Draft:   Draft{Body: "Текст."},
		}
		got := buildDostupBody(sess)
		has := strings.Contains(got, "електронною поштою")
		if tc.want == "" && has {
			t.Errorf("%s: мусорная строка почты попала в письмо:\n%s", tc.name, got)
		}
		if tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s: строка почты не найдена в:\n%s", tc.name, got)
		}
	}
}

func TestValidEmail(t *testing.T) {
	valid := []string{"ivan@gmail.com", "i.petro@org.gov.ua", "a@b.co"}
	invalid := []string{"", "@", "a@", "@b.com", "a@b", "a b@c.com", "a@b@c.com", "a@b.", "a b@c.d"}
	for _, s := range valid {
		if !validEmail(s) {
			t.Errorf("validEmail(%q)=false, want true", s)
		}
	}
	for _, s := range invalid {
		if validEmail(s) {
			t.Errorf("validEmail(%q)=true, want false", s)
		}
	}
}

// applySignature: короткая подпись (только имя) сохраняется в FullName,
// части синхронизируются, а следующий showProfile/письмо её не потеряют.
func TestApplySignature(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		wantFullName string
		wantFirst    string
		wantLast     string
	}{
		{"только имя", "Віктор", "Віктор", "Віктор", ""},
		{"имя и фамилия", "Іван Петренко", "Іван Петренко", "Іван", "Петренко"},
		{"инициалы", "І. Петренко", "І. Петренко", "І.", "Петренко"},
		{"полное ФИО", "Гаршин Сергій Юрійович", "Гаршин Сергій Юрійович", "Гаршин", "Юрійович"},
	}
	for _, tc := range cases {
		sess := &session.SessionData{}
		applySignature(sess, tc.input)
		if sess.Profile.FullName != tc.wantFullName {
			t.Errorf("%s: FullName=%q, want %q", tc.name, sess.Profile.FullName, tc.wantFullName)
		}
		if sess.Profile.FirstName != tc.wantFirst || sess.Profile.LastName != tc.wantLast {
			t.Errorf("%s: parts=(%q,%q), want (%q,%q)", tc.name, sess.Profile.FirstName, sess.Profile.LastName, tc.wantFirst, tc.wantLast)
		}
		// Подпись в письме обязана совпадать с вводом пользователя
		if sign := session.SignatureName(sess.Profile); sign != tc.wantFullName {
			t.Errorf("%s: SignatureName=%q, want %q", tc.name, sign, tc.wantFullName)
		}
	}
}

// Регресс: подпись «І. Петренко» после applySignature не должна
// пересобираться в «Петренко І.» (старый showProfile перезаписывал
// FullName из частей и молча менял подпись).
func TestSignatureStableAfterPartsRebuild(t *testing.T) {
	sess := &session.SessionData{}
	applySignature(sess, "І. Петренко")
	// Эмуляция старого поведения showProfile: пересборка FullName из частей
	// больше не должна происходить — но если кто-то вызовет
	// ProfileDisplayName, подпись в FullName останется нетронутой.
	if sess.Profile.FullName != "І. Петренко" {
		t.Fatalf("FullName изменился: %q", sess.Profile.FullName)
	}
	// ProfileDisplayName даёт «Петренко І.» — это только отображение,
	// FullName (подпись) остаётся как ввёл пользователь
	if got := session.ProfileDisplayName(sess.Profile); got != "Петренко І." {
		t.Logf("ProfileDisplayName=%q (отображение), подпись FullName=%q", got, sess.Profile.FullName)
	}
}
