package handlers

import (
	"testing"

	"info-bot-go/internal/session"
)

// Регресія бага з продакшну (31.08): кнопка «Пропустити» на кроці
// «2️⃣ Введіть ваше прізвище» питала те саме поле нескінченно — бот
// обирав «перше порожнє поле», а пропущене поле як було, так і
// лишалося порожнім.

func TestNextProfileStepSequence(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"profile:firstName", "profile:lastName"},
		{"profile:lastName", "profile:middleName"},
		{"profile:middleName", "profile:postalAddress"},
		{"profile:postalAddress", "profile:email"},
		{"profile:email", ""},     // останній крок → фінальний екран
		{"profile:signature", ""}, // не поле послідовності
		{"idle", ""},
		{"new:pick_category", ""},
	}
	for _, tc := range cases {
		if got := nextProfileStep(tc.in); got != tc.want {
			t.Errorf("nextProfileStep(%q) = %q, хочеться %q", tc.in, got, tc.want)
		}
	}
}

func TestNextProfileStepNeverLoops(t *testing.T) {
	// Головна умова фікса: після пропуску бот НІКОЛИ не повертається
	// до того самого кроку (саме це і було нескінченним циклом кнопки).
	for _, s := range profileFieldSteps {
		if next := nextProfileStep(s); next == s {
			t.Errorf("nextProfileStep(%q) == %q — кнопка «Пропустити» зациклиться", s, next)
		}
	}
}

func TestIsProfileFieldStep(t *testing.T) {
	for _, s := range profileFieldSteps {
		if !isProfileFieldStep(s) {
			t.Errorf("isProfileFieldStep(%q) = false, хочеться true", s)
		}
	}
	for _, s := range []string{"profile:signature", "idle", "new:ask_subject", ""} {
		if isProfileFieldStep(s) {
			t.Errorf("isProfileFieldStep(%q) = true, хочеться false (не поле послідовності)", s)
		}
	}
}

func TestProfileFieldEmpty(t *testing.T) {
	p := session.Profile{
		FirstName: "Сергій",
		LastName:  "Воробей",
		Email:     "a@b.ua",
	}
	cases := []struct {
		step string
		want bool
	}{
		{"profile:firstName", false},
		{"profile:lastName", false},
		{"profile:middleName", true},
		{"profile:postalAddress", true},
		{"profile:email", false},
		{"profile:signature", false}, // не поле
	}
	for _, tc := range cases {
		if got := profileFieldEmpty(p, tc.step); got != tc.want {
			t.Errorf("profileFieldEmpty(%+v, %q) = %v, хочеться %v", p, tc.step, got, tc.want)
		}
	}
}

func TestApplySkip(t *testing.T) {
	sess := &session.SessionData{}
	sess.Profile = session.Profile{
		FirstName:     "Сергій",
		LastName:      "Воробей",
		MiddleName:    "Іванович",
		PostalAddress: "м. Запоріжжя",
		Email:         "a@b.ua",
	}
	sess.Draft.UseSharedMailbox = false

	applySkip(sess, "profile:lastName")
	if sess.Profile.LastName != "" || sess.Profile.FirstName != "Сергій" {
		t.Errorf("пропуск прізвища має очистити ЛИШЕ LastName: %+v", sess.Profile)
	}

	applySkip(sess, "profile:middleName")
	if sess.Profile.MiddleName != "" {
		t.Errorf("пропуск по-батькові має очистити MiddleName")
	}

	applySkip(sess, "profile:postalAddress")
	if sess.Profile.PostalAddress != "" {
		t.Errorf("пропуск адреси має очистити PostalAddress")
	}

	// Пропуск email вмикає спільну скриньку (відповідь — на публічній сторінці).
	applySkip(sess, "profile:email")
	if sess.Profile.Email != "" {
		t.Errorf("пропуск email має очистити Email")
	}
	if !sess.Draft.UseSharedMailbox {
		t.Errorf("пропуск email має вмикати UseSharedMailbox")
	}

	// Ім'я пропустити не можна: applySkip не чіпає firstName навіть якщо
	// хтось викликатиме з цим кроком (кнопки там немає, але захистимося).
	saved := sess.Profile.FirstName
	applySkip(sess, "profile:firstName")
	if sess.Profile.FirstName != saved {
		t.Errorf("firstName не має очищуватися пропуском: %q", sess.Profile.FirstName)
	}
}

// TestJourneyOnlyFirstName — сценарій власника (лист 31.08): користувач
// хоче зберігати в профілі ЛИШЕ ім'я, без прізвища. Проганяємо весь флоу
// логіки: ім'я → пропуск прізвища → пропуск по-батькові → пропуск адреси →
// пропуск email → фінальний екран (showProfile заповнює підпис з частин).
func TestJourneyOnlyFirstName(t *testing.T) {
	sess := &session.SessionData{}

	// 1️⃣ Введіть ваше ім'я → «Сергій»
	sess.Step = "profile:firstName"
	sess.Profile.FirstName = "Сергій"
	next := nextProfileStep(sess.Step)
	if next != "profile:lastName" {
		t.Fatalf("після імені очікувався крок прізвища, отримано %q", next)
	}

	// 2️⃣..5️⃣ користувач усе пропускає — і кожен пропуск рухає вперед
	remaining := []string{
		"profile:lastName",
		"profile:middleName",
		"profile:postalAddress",
		"profile:email",
	}
	for _, step := range remaining {
		sess.Step = step
		applySkip(sess, step)
		sess.Step = nextProfileStep(step)
		if isProfileFieldStep(sess.Step) && sess.Step == step {
			t.Fatalf("пропуск %q залишив курсор на тому ж кроці — цикл!", step)
		}
	}
	if sess.Step != "" {
		t.Fatalf("після останнього пропуску флоу має завершитися, крок = %q", sess.Step)
	}

	// Фінальний екран: showProfile заповнює підпис з частин, якщо пусто.
	if sess.Profile.FullName == "" {
		sess.Profile.FullName = session.ProfileDisplayName(sess.Profile)
	}
	if got := sess.Profile.FullName; got != "Сергій" {
		t.Errorf("підпис має бути лише ім'я «Сергій», отримано %q", got)
	}
	if !session.IsProfileReady(sess.Profile) {
		t.Errorf("профіль лише з ім'ям має вважатися готовим (FullName заповнено)")
	}
	if got := session.SignatureName(sess.Profile); got != "Сергій" {
		t.Errorf("листи мають підписуватися «Сергій», отримано %q", got)
	}
}

// TestJourneySkipLastNameThenAnswer — пропуск не має ламати решту флоу:
// після пропуску прізвища користувач Усе ще може ввести по-батькові.
func TestJourneySkipLastNameThenAnswer(t *testing.T) {
	sess := &session.SessionData{}
	sess.Profile.FirstName = "Сергій"

	// пропустили прізвище
	sess.Step = "profile:lastName"
	applySkip(sess, "profile:lastName")
	sess.Step = nextProfileStep("profile:lastName")
	if sess.Step != "profile:middleName" {
		t.Fatalf("після пропуску прізвища очікувалося питання по-батькові, отримано %q", sess.Step)
	}

	// відповіли на по-батькові
	sess.Profile.MiddleName = "Іванович"
	sess.Step = nextProfileStep(sess.Step)
	if sess.Step != "profile:postalAddress" {
		t.Fatalf("після по-батькові очікувалася адреса, отримано %q", sess.Step)
	}
	if sess.Profile.LastName != "" {
		t.Errorf("прізвище має лишатися порожнім після пропуску")
	}

	// підпис з частин — без прізвища, але з по-батькові
	sess.Profile.FullName = session.ProfileDisplayName(sess.Profile)
	if got := sess.Profile.FullName; got != "Сергій Іванович" {
		t.Errorf("очікувався підпис «Сергій Іванович», отримано %q", got)
	}
}
