package handlers

// Тесты ТЗ №8 (блок E): тижневий дайджест, «Що нового» вернувшимся,
// демо-розбір (константы учебного примера).

import (
	"strings"
	"testing"
	"time"

	"info-bot-go/internal/dostup"
)

func TestDigestDue(t *testing.T) {
	kyiv, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		kyiv = kyivLoc()
	}
	// Понедельник 31.08.2026, 09:30 Киева
	mon := time.Date(2026, 8, 31, 9, 30, 0, 0, kyiv)
	if mon.Weekday() != time.Monday {
		t.Fatalf("тест сломан: 31.08.2026 не понедельник (%v)", mon.Weekday())
	}

	t.Run("понедельник после часа — пора", func(t *testing.T) {
		if !digestDue(time.Time{}, mon, kyiv, 9) {
			t.Fatal("понедельник 09:30 при пустой истории должен отправляться")
		}
	})
	t.Run("до часа — рано", func(t *testing.T) {
		early := time.Date(2026, 8, 31, 7, 0, 0, 0, kyiv)
		if digestDue(time.Time{}, early, kyiv, 9) {
			t.Fatal("до 09:00 отправлять нельзя")
		}
	})
	t.Run("не понедельник — нет", func(t *testing.T) {
		tue := time.Date(2026, 9, 1, 10, 0, 0, 0, kyiv)
		if digestDue(time.Time{}, tue, kyiv, 9) {
			t.Fatal("во вторник отправлять нельзя")
		}
	})
	t.Run("уже отправляли в этот понедельник — нет", func(t *testing.T) {
		sentAt := time.Date(2026, 8, 31, 9, 5, 0, 0, kyiv)
		if digestDue(sentAt, mon, kyiv, 9) {
			t.Fatal("повторная отправка в тот же понедельник запрещена")
		}
	})
	t.Run("отправляли неделю назад (прошлый понедельник) — пора", func(t *testing.T) {
		sentAt := time.Date(2026, 8, 24, 9, 5, 0, 0, kyiv)
		if !digestDue(sentAt, mon, kyiv, 9) {
			t.Fatal("новый понедельник после прошлонедельной отправки должен отправиться")
		}
	})
	t.Run("рестарт бота не дублирует отправку", func(t *testing.T) {
		// процесс перезапустили через час после отправки
		sentAt := time.Date(2026, 8, 31, 9, 5, 0, 0, kyiv)
		restart := time.Date(2026, 8, 31, 10, 30, 0, 0, kyiv)
		if digestDue(sentAt, restart, kyiv, 9) {
			t.Fatal("рестарт в тот же понедельник не должен дублировать дайджест")
		}
	})
}

func TestBuildDigestText(t *testing.T) {
	cur := digestSnapshot{Users: 42, Requests: 120, Replies: 61, Analyze: 89}
	prev := digestSnapshot{Users: 39, Requests: 115, Replies: 59, Analyze: 82}
	best := []dostup.LeaderRow{
		{Rank: 1, Name: "Ужгородська міська рада", Index: 50, Requests: 12, OverduePct: 0},
	}
	worst := []dostup.LeaderRow{
		{Rank: 1, Name: "Якийсь орган", Index: 5, Requests: 8, OverduePct: 75},
	}
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	asOf := now.Add(-2 * time.Hour)

	text := buildDigestText(cur, prev, best, worst, 2145, asOf, now)

	for _, want := range []string{
		"Тижневий дайджест",
		"Нових користувачів: <b>+3</b> (разом 42)",
		"Запитів надіслано: <b>+5</b> (разом 120)",
		"AI-розборів: <b>+7</b> (разом 89)",
		"Ужгородська міська рада — індекс 50",
		"Антирейтинг",
		"Готовий пост для каналу",
		"Оцінено 2145 органів",
		"@Infozaputbot",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("дайджест не содержит %q", want)
		}
	}

	t.Run("дельты не уходят в минус при коррекции счётчиков", func(t *testing.T) {
		bigger := digestSnapshot{Users: 42, Requests: 100, Replies: 50, Analyze: 80}
		text := buildDigestText(bigger, cur, nil, nil, 0, time.Time{}, now)
		if strings.Contains(text, "+-") || strings.Contains(text, ":-5") {
			t.Fatalf("отрицательная дельта просочилась в текст: %s", text)
		}
	})

	t.Run("без рейтинга — без раздела топов", func(t *testing.T) {
		text := buildDigestText(cur, prev, nil, nil, 0, time.Time{}, now)
		if strings.Contains(text, "Топ-5") || strings.Contains(text, "пост для каналу") {
			t.Fatal("пустой рейтинг не должен рисоваться")
		}
	})
}

func TestShouldShowWhatsNew(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	twoDaysAgo := now.Add(-48 * time.Hour)
	monthAgo := now.Add(-30 * 24 * time.Hour)

	if shouldShowWhatsNew(time.Time{}, time.Time{}, now) {
		t.Fatal("новичку «Що нового» не показываем")
	}
	if shouldShowWhatsNew(twoDaysAgo, time.Time{}, now) {
		t.Fatal("ушёл на 2 дня — не показываем")
	}
	if !shouldShowWhatsNew(monthAgo, time.Time{}, now) {
		t.Fatal("вернулся после 30 дней — показываем")
	}
	if !shouldShowWhatsNew(monthAgo, monthAgo, now) {
		t.Fatal("первый возврат после долгого отсутствия — показываем")
	}
	recentShown := now.Add(-3 * 24 * time.Hour)
	if shouldShowWhatsNew(monthAgo, recentShown, now) {
		t.Fatal("недавно показывали — не спамим")
	}
}

func TestDemoConstants(t *testing.T) {
	// Учебный пример должен быть полноценным: и орган, и тема, и текст.
	if len(strings.TrimSpace(demoOrgan)) < 5 {
		t.Error("demoOrgan слишком короткий")
	}
	if len(strings.TrimSpace(demoSubject)) < 5 {
		t.Error("demoSubject слишком короткий")
	}
	if len(strings.TrimSpace(demoReply)) < 200 {
		t.Errorf("demoReply подозрительно короткий (%d симв.)", len(demoReply))
	}
	// Демо-ответ обязан содержать юридически значимые маркеры кейса.
	for _, want := range []string{"Оберіг", "Міністерством юстиції", "Про звернення громадян"} {
		if !strings.Contains(demoReply, want) {
			t.Errorf("demoReply не содержит %q", want)
		}
	}
}
