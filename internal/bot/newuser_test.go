package bot

import (
	"strings"
	"testing"

	tb "gopkg.in/telebot.v3"
)

// shouldNotifyNewUser: обычный новый пользователь в личке → уведомляем.
func TestShouldNotifyNewUser_PrivateStranger(t *testing.T) {
	if !shouldNotifyNewUser(111, 745130167, string(tb.ChatPrivate)) {
		t.Fatal("обычный новый пользователь в личном чате должен уведомлять владельца")
	}
}

// shouldNotifyNewUser: сам владелец → не уведомляем.
func TestShouldNotifyNewUser_AdminHimself(t *testing.T) {
	if shouldNotifyNewUser(745130167, 745130167, string(tb.ChatPrivate)) {
		t.Fatal("владелец не должен получать уведомление о самом себе")
	}
}

// shouldNotifyNewUser: группа/канал → не уведомляем (любой участник
// группы выглядит «новым», хотя бот не начинали).
func TestShouldNotifyNewUser_Group(t *testing.T) {
	if shouldNotifyNewUser(111, 745130167, string(tb.ChatGroup)) {
		t.Fatal("сообщение в группе не должно уведомлять владельца")
	}
	if shouldNotifyNewUser(111, 745130167, string(tb.ChatSuperGroup)) {
		t.Fatal("сообщение в супергруппе не должно уведомлять владельца")
	}
}

// shouldNotifyNewUser: админ не настроен или нулевой отправитель → нет.
func TestShouldNotifyNewUser_NoAdminOrZero(t *testing.T) {
	if shouldNotifyNewUser(111, 0, string(tb.ChatPrivate)) {
		t.Fatal("без настроенного владельца уведомлять некому")
	}
	if shouldNotifyNewUser(0, 745130167, string(tb.ChatPrivate)) {
		t.Fatal("нулевой отправитель — не пользователь")
	}
}

// shouldNotifyNewUser: неизвестный тип чата (пустой) → уведомляем,
// в частном боте это нормальный личный апдейт (callback и т.п.).
func TestShouldNotifyNewUser_EmptyChatType(t *testing.T) {
	if !shouldNotifyNewUser(111, 745130167, "") {
		t.Fatal("пустой тип чата следует считать личным апдейтом")
	}
}

// newUserNotifyText: имя + username + счётчик.
func TestNewUserNotifyText_Full(t *testing.T) {
	s := tb.User{ID: 42, FirstName: "Олена", LastName: "Петренко", Username: "olena_p"}
	text := newUserNotifyText(s, 5)
	for _, want := range []string{"Олена Петренко", "@olena_p", "ID: 42", "Всього користувачів: 5"} {
		if !strings.Contains(text, want) {
			t.Fatalf("текст уведомления должен содержать %q, получено: %s", want, text)
		}
	}
}

// newUserNotifyText: без имени и без username — не падаем, показываем заглушки.
func TestNewUserNotifyText_Empty(t *testing.T) {
	s := tb.User{ID: 7}
	text := newUserNotifyText(s, 0)
	if !strings.Contains(text, "без імені") {
		t.Fatalf("без имени должна быть заглушка «без імені», получено: %s", text)
	}
	if !strings.Contains(text, "немає") {
		t.Fatalf("без username должна быть заглушка «немає», получено: %s", text)
	}
	if strings.Contains(text, "Всього користувачів") {
		t.Fatalf("при нулевом счётчике строку «Всього користувачів» не показываем: %s", text)
	}
}
