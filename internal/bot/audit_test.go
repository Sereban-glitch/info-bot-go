package bot

import (
	"strings"
	"testing"

	tb "gopkg.in/telebot.v3"
)

func updWithText(text string) tb.Update {
	return tb.Update{Message: &tb.Message{Text: text}}
}

// Инлайн-кнопка: уникальный идентификатор остаётся, payload отбрасывается.
func TestAuditAction_CallbackStripsPayload(t *testing.T) {
	u := tb.Update{Callback: &tb.Callback{Data: "\fdonate|user_42"}}
	if got := auditAction(u); got != "btn:donate" {
		t.Fatalf("btn:want %q, got %q", "btn:donate", got)
	}
}

// Инлайн-кнопка без payload и без префикса тоже распознаётся.
func TestAuditAction_CallbackRaw(t *testing.T) {
	u := tb.Update{Callback: &tb.Callback{Data: "pr_tone_sharp"}}
	if got := auditAction(u); got != "btn:pr_tone_sharp" {
		t.Fatalf("btn raw: want %q, got %q", "btn:pr_tone_sharp", got)
	}
}

// Команда: аргументы и суффикс @botname отрезаются.
func TestAuditAction_Command(t *testing.T) {
	for in, want := range map[string]string{
		"/start":                   "cmd:/start",
		"/find Івано-Франківськ":    "cmd:/find",
		"/my@Infozaputbot":         "cmd:/my",
		"  /new  ":                 "cmd:/new",
	} {
		if got := auditAction(updWithText(in)); got != want {
			t.Fatalf("cmd %q: want %q, got %q", in, want, got)
		}
	}
}

// Известная кнопка меню логируется с подписью.
func TestAuditAction_MenuButton(t *testing.T) {
	got := auditAction(updWithText("📚 Шаблони"))
	if got != "menu:📚 Шаблони" {
		t.Fatalf("menu: want %q, got %q", "menu:📚 Шаблони", got)
	}
}

// Приватность: произвольный текст НЕ попадает в журнал — только msg:text.
func TestAuditAction_FreeTextNotLogged(t *testing.T) {
	secret := "Прошу надати інформацію про мій будинок на вулиці Секретній, 7"
	got := auditAction(updWithText(secret))
	if got != "msg:text" {
		t.Fatalf("free text must collapse to msg:text, got %q", got)
	}
	if strings.Contains(got, "Секрет") {
		t.Fatal("содержимое сообщения не должно попадать в действие")
	}
	// Короткий произвольный текст тоже не должен попадать целиком.
	got = auditAction(updWithText("привіт"))
	if got != "msg:text" {
		t.Fatalf("short free text must collapse to msg:text, got %q", got)
	}
}

// Медиа: голос/фото/документ — факт без содержимого.
func TestAuditAction_Media(t *testing.T) {
	u := tb.Update{Message: &tb.Message{Voice: &tb.Voice{MIME: "audio/ogg"}}}
	if got := auditAction(u); got != "msg:voice" {
		t.Fatalf("voice: got %q", got)
	}
	u = tb.Update{Message: &tb.Message{Photo: &tb.Photo{Caption: "секрет"}}}
	if got := auditAction(u); got != "msg:photo" {
		t.Fatalf("photo: got %q", got)
	}
	u = tb.Update{Message: &tb.Message{Document: &tb.Document{FileName: "x.pdf"}}}
	if got := auditAction(u); got != "msg:document" {
		t.Fatalf("document: got %q", got)
	}
}

// Пустой/служебный апдейт — не логировать.
func TestAuditAction_NoAction(t *testing.T) {
	if got := auditAction(tb.Update{}); got != "" {
		t.Fatalf("empty update: want \"\", got %q", got)
	}
	u := tb.Update{Message: &tb.Message{Caption: "только подпись без текста"}}
	if got := auditAction(u); got != "" {
		t.Fatalf("caption-only update: want \"\", got %q", got)
	}
}

// Длинный мусорный идентификатор кнопки обрезается — защита лога.
func TestAuditAction_CallbackLongData(t *testing.T) {
	long := strings.Repeat("a", 100)
	u := tb.Update{Callback: &tb.Callback{Data: long}}
	got := auditAction(u)
	if len(got) != len("btn:")+40 {
		t.Fatalf("long data must be cut to 40, got len=%d", len(got))
	}
}
