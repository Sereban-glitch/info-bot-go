package dostup

import "testing"

// Тесты классификатора тем (ТЗ №9). Главные кейсы — три реальных запроса
// с прода плюс типовые коллизии стемов.
func TestClassifyTopic(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string // ID темы
	}{
		// Реальные запросы с прода (28–30.08.2026)
		{
			name: "ДСА: перелік електронних адрес судів (суд б'є «дані»)",
			text: "Перелік електронних адрес судів для звернень громадян Державна судова адміністрація України",
			want: "justice",
		},
		{
			name: "МОЗ: статистика серцево-судинних захворювань (здоров'я б'є «суд» у слові судинних)",
			text: "Про надання статистичної інформації щодо серцево-судинних захворювань за перше півріччя Міністерство охорони здоров'я",
			want: "health",
		},
		{
			name: "Офіс Президента: військова посада",
			text: "Запит про надання інформації щодо військової посади Буданова К.О. Офіс Президента України",
			want: "defense",
		},
		// Коллизии стемов
		{
			name: "«освітлення вулиць» — інфраструктура, а не освіта",
			text: "Організація освітлення вулиць у селищі",
			want: "infra",
		},
		{
			name: "«заклад освіти» — освіта",
			text: "Фінансування закладу освіти",
			want: "education",
		},
		{
			name: "«електронні адреси органів» — дані, а не інфраструктура",
			text: "Перелік електронних адрес органів влади",
			want: "data",
		},
		{
			name: "«впорядкування землі» — не соціальна (ВПО)",
			text: "Впорядкування земельної ділянки",
			want: "land",
		},
		{
			name: "«посаду» не містить «суд»",
			text: "Про призначення на посаду директора",
			want: "gov",
		},
		// Типовые темы
		{"тцк і мобілізація", "Облік громадян у ТЦК та мобілізація", "defense"},
		{"пенсії", "Про перерахунок пенсій", "social"},
		{"доріг", "Стан доріг обласного значення", "infra"},
		{"бюджет", "Виконання бюджету громади за 2025 рік", "economy"},
		{"еко", "Вивезення сміття та звалища", "ecology"},
		{"реєстр", "Виписка з реєстру власників", "data"},
		{"порожній текст", "", "other"},
		{
			name: "невідома тема",
			text: "Про проведення святкового заходу",
			want: "other",
		},
	}
	for _, tc := range cases {
		if got := ClassifyTopic(tc.text); got.ID != tc.want {
			t.Errorf("%s: ClassifyTopic=%q (%s), want %q", tc.name, got.ID, got.Title, tc.want)
		}
	}
}

func TestTopicByID(t *testing.T) {
	if TopicByID("health").Title != "Охорона здоров'я" {
		t.Errorf("TopicByID(health)=%q", TopicByID("health").Title)
	}
	if TopicByID("nope").ID != "other" {
		t.Errorf("TopicByID(nope)=%q, want other", TopicByID("nope").ID)
	}
}

func TestTopicsOrder(t *testing.T) {
	ts := Topics()
	if len(ts) < 10 {
		t.Fatalf("слишком мало тем: %d", len(ts))
	}
	// Специфичные темы обязаны идти раньше генеральных —
	// это фундамент приоритетного матчинга.
	idx := func(id string) int {
		for i, t := range ts {
			if t.ID == id {
				return i
			}
		}
		return -1
	}
	if idx("health") >= idx("justice") {
		t.Error("health должен идти раньше justice («серцево-судинні»)")
	}
	if idx("justice") >= idx("data") {
		t.Error("justice должен идти раньше data («адрес судів»)")
	}
	if idx("data") >= idx("gov") {
		t.Error("data должен идти раньше gov")
	}
}
