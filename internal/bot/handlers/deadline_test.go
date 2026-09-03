package handlers

// Тесты дневного дайджеста сроков (просьба владельца 04.09.2026: не
// спамить напоминаниями — одно сгруппированное сообщение в сутки).
// До фикса: «последний день» и «просрочено» отправлялись КАЖДЫЙ ЧАС.

import (
	"strings"
	"testing"
	"time"

	"info-bot-go/internal/sentlog"
)

func TestDeadlineDigestDue(t *testing.T) {
	kyiv := kyivLoc()
	// Пятница 04.09.2026 (31.08.2026 — понедельник).
	fri := func(h, m int) time.Time { return time.Date(2026, 9, 4, h, m, 0, 0, kyiv) }

	t.Run("ночь — рано", func(t *testing.T) {
		if deadlineDigestDue(time.Time{}, fri(8, 59), kyiv) {
			t.Fatal("до 09:00 отправлять нельзя")
		}
	})
	t.Run("09:00 — можно", func(t *testing.T) {
		if !deadlineDigestDue(time.Time{}, fri(9, 0), kyiv) {
			t.Fatal("с 09:00 отправлять можно")
		}
	})
	t.Run("21:59 — можно, 22:00 — поздно", func(t *testing.T) {
		if !deadlineDigestDue(time.Time{}, fri(21, 59), kyiv) {
			t.Fatal("до 22:00 отправлять можно")
		}
		if deadlineDigestDue(time.Time{}, fri(22, 0), kyiv) {
			t.Fatal("после 22:00 отправлять нельзя")
		}
	})
	t.Run("уже отправляли сегодня — молчим", func(t *testing.T) {
		sent := fri(10, 0)
		if deadlineDigestDue(sent, fri(15, 0), kyiv) {
			t.Fatal("второй дайджест в те же сутки запрещён")
		}
	})
	t.Run("отправляли вчера — можно снова", func(t *testing.T) {
		sent := time.Date(2026, 9, 3, 10, 0, 0, 0, kyiv)
		if !deadlineDigestDue(sent, fri(15, 0), kyiv) {
			t.Fatal("на следующие сутки дайджест разрешён")
		}
	})
}

func TestCollectDeadlineItems(t *testing.T) {
	kyiv := kyivLoc()
	// Понедельник 07.09.2026 12:00 Киева.
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, kyiv)

	entries := []sentlog.SentEntry{
		// Просрочка: отправлен 20.08 → дедлайн ~27.08 → 🔴
		{MessageID: "m1", ChatID: 100, UserID: 100, RecipientName: "ДСА", Date: "2026-08-20T12:00:00+03:00"},
		// Последний день: отправлен 01.09 18:00 → дедлайн вт 08.09 18:00 (30ч) → 🟠
		{MessageID: "m2", ChatID: 100, UserID: 100, RecipientName: "МОЗ", Date: "2026-09-01T18:00:00+03:00"},
		// Уже отвечено — не участвует
		{MessageID: "m3", ChatID: 100, UserID: 100, RecipientName: "НПУ", Date: "2026-08-20T12:00:00+03:00", Status: "replied"},
		// Недоставлено — не участвует
		{MessageID: "m4", ChatID: 100, UserID: 100, RecipientName: "X", Date: "2026-08-20T12:00:00+03:00", Status: "bounced"},
		// Просрочку уже отправляли (ГЛАВНЫЙ ФИКС старого спама) — не участвует
		{MessageID: "m5", ChatID: 100, UserID: 100, RecipientName: "Y", Date: "2026-08-20T12:00:00+03:00", Status: "expired"},
		// ChatID не указан → группируется по UserID
		{MessageID: "m6", UserID: 200, RecipientName: "Офіс Президента", Date: "2026-09-01T18:00:00+03:00"},
	}

	pending := collectDeadlineItems(entries, now)

	items := pending[100]
	if len(items) != 2 {
		t.Fatalf("у пользователя 100 должно быть 2 пункта (🔴+🟠), есть %d", len(items))
	}
	red, orange := items[0], items[1]
	if !red.expired || !strings.Contains(red.line, "🔴") || !strings.Contains(red.line, "ДСА") {
		t.Fatalf("первый пункт должен быть просрочкой ДСА: %+v", red)
	}
	if orange.expired || !strings.Contains(orange.line, "🟠") || !strings.Contains(orange.line, "МОЗ") {
		t.Fatalf("второй пункт должен быть последним днём МОЗ: %+v", orange)
	}
	if _, ok := pending[200]; !ok || len(pending[200]) != 1 {
		t.Fatalf("fallback на UserID не сработал: %+v", pending)
	}
	for id := range pending {
		if id != 100 && id != 200 {
			t.Fatalf("лишний пользователь в выборке: %d", id)
		}
	}
}

func TestGroupOverdueThreads(t *testing.T) {
	kyiv := kyivLoc()
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, kyiv)
	old := "2026-08-20T12:00:00+03:00" // просрочен давно

	entries := []sentlog.SentEntry{
		{MessageID: "dostup:slugA", UserID: 7, Channel: "dostup", URL: "https://dostup.org.ua/request/slugA", Date: old},
		{MessageID: "dostup:slugB", UserID: 7, Channel: "dostup", URL: "https://dostup.org.ua/request/slugB", Date: old},
		{MessageID: "dostup:slugC", UserID: 9, Channel: "dostup", URL: "https://dostup.org.ua/request/slugC", Date: "2026-09-05T12:00:00+03:00"},
		{MessageID: "email:plain", UserID: 9, Channel: "email", Date: old},
	}
	threads := map[int64][]FollowUpThread{
		7: {
			{Slug: "slugA", Organ: "ДСА", Subject: "Перелік", URL: "u"},
			// slugB напоминали 3 часа назад → пользователь 7 сегодня молчит
			{Slug: "slugB", Organ: "МОЗ", Subject: "Статистика", URL: "u", LastRemindAt: now.Add(-3 * time.Hour).Format(time.RFC3339)},
		},
		9: {
			// дедлайн slugC ещё не прошёл (05.09+5 рд) — не группируется
			{Slug: "slugC", Organ: "АП", Subject: "Посада", URL: "u"},
		},
	}

	grouped, reminded := groupOverdueThreads(entries, func(uid int64) []FollowUpThread { return threads[uid] }, now)

	if !reminded[7] {
		t.Fatal("пользователь 7 получал напоминание за последние 24 часа — должен молчать")
	}
	if len(grouped[7]) != 2 {
		t.Fatalf("у пользователя 7 обе просроченные гилки попадают в группу: %d", len(grouped[7]))
	}
	if len(grouped[9]) != 0 {
		t.Fatalf("у пользователя 9 дедлайн не прошёл и канал не тот: %d", len(grouped[9]))
	}
	if len(reminded) != 1 {
		t.Fatalf("лишние флаги «напоминали»: %+v", reminded)
	}
}
