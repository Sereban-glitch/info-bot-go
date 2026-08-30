package handlers

import (
	"strings"
	"testing"
)

// Регресія: багрепорт губився, якщо в імені/юзернеймі/тексті були
// Markdown-символи (реальний випадок 30.08.2026: «can't parse entities»,
// адмін не отримав звіт, а користувачу показали «Дякуємо!»).

func TestBuildBugReportAdminHTMLEscapesEverything(t *testing.T) {
	cases := []struct {
		name      string
		username  string
		firstName string
		lastName  string
		report    string
	}{
		{"underscores", "shoferv_v", "Віктор", "Тест_Іваненко", "кнопка *не* працює"},
		{"markdown_mix", "user[name]", "*Арнольд*", "Шварцнегер", "помилка `тут` [посилання](https://x)"},
		{"html_injection", "<b>admin</b>", "Іван", "Петренко", "<script>alert(1)</script>"},
		{"ampersand", "a&b", "Олена", "Ковальчук", "Q&A & more"},
		{"plain", "normal_user", "Сергій", "Воробей", "Все працює добре"},
		{"empty", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildBugReportAdminHTML(tc.username, 42, tc.firstName, tc.lastName, tc.report)

			// Жодного «сирого» кутового дужного HTML з даних користувача
			if strings.Contains(got, "<script>") || strings.Contains(got, "<b>admin</b>") {
				t.Errorf("HTML не екрановано: %q", got)
			}
			// Валідні теги розмітки картки лишаються
			if !strings.Contains(got, "<b>Звіт про помилку</b>") {
				t.Errorf("заголовок картки загублено: %q", got)
			}
			if !strings.Contains(got, "ID: 42") {
				t.Errorf("ID користувача загублено: %q", got)
			}
			// ampersand екранується
			if strings.Contains(got, "a&b") && tc.username == "a&b" {
				t.Errorf("амперсанд не екрановано: %q", got)
			}
		})
	}
}

func TestBuildBugReportAdminHTMLStructure(t *testing.T) {
	got := buildBugReportAdminHTML("shoferv", 5172631906, "Віктор", "", "Не працює кнопка пропуску")
	for _, want := range []string{
		"@shoferv",
		"5172631906",
		"Віктор",
		"Не працює кнопка пропуску",
		"📝",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("у картці немає %q: %q", want, got)
		}
	}
}

func TestBuildBugReportAdminHTMLEmptyName(t *testing.T) {
	// Порожнє ім'я → «—», порожній юзернейм → «—» (без «@»)
	got := buildBugReportAdminHTML("", 1, "", "", "текст")
	if !strings.Contains(got, "👤 Ім'я: —") {
		t.Errorf("порожнє ім'я має показуватися як «—»: %q", got)
	}
	if strings.Contains(got, "@—") {
		t.Errorf("порожній юзернейм не має мати «@»: %q", got)
	}
	if !strings.Contains(got, "👤 Від: —") {
		t.Errorf("порожній юзернейм має показуватися як «—»: %q", got)
	}
}
