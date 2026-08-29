package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"info-bot-go/internal/session"
)

// Тест-контракт дисклеймера публичности портала (фича B1, ТЗ «Прозрачность
// по умолчанию»). Текст содержит юридически значимые обещания пользователю:
// что публикуется открыто, что маскирует портал, предупреждение о чутливих
// даних и переваги бота перед порталом. Если копирайт меняют так, что одно
// из обещаний пропадает — тест падает и требует осознанной правки.
func TestDisclosureTextContract(t *testing.T) {
	txt := dostupDisclosureText()

	mustContain := []string{
		// публичность — на первом месте
		"публічний портал",
		"Що буде відкрито для всіх",
		// что именно открыто
		"текст вашого запиту та відповідь органу",
		"ваш підпис — ім'я та прізвище",
		// что маскирует портал (проверено на живой странице запроса)
		"Що портал маскує автоматично",
		"[ email address ]",
		// предупреждение о чувствительных данных
		"дату народження",
		"не для чутливих даних",
		// позиционирование: бот — автоматизация процедуры
		"автоматизую офіційну процедуру",
		"2939-VI",
		// переваги бота перед порталом
		"повідомлю в чат, коли орган відповість",
		"нагадаю, якщо строк",
		"штучним інтелектом",
		"голосом",
	}
	for _, want := range mustContain {
		if !strings.Contains(txt, want) {
			t.Errorf("дисклеймер потерял обязательный фрагмент %q", want)
		}
	}

	// Текст уходит в Telegram как HTML — парность тегов <b> обязательна.
	if strings.Count(txt, "<b>") != strings.Count(txt, "</b>") {
		t.Errorf("непарные <b>-теги в дисклеймере: %d открывающих, %d закрывающих",
			strings.Count(txt, "<b>"), strings.Count(txt, "</b>"))
	}
	if len(txt) > 4000 {
		t.Errorf("дисклеймер длиннее лимита Telegram-сообщения: %d символов", len(txt))
	}
}

// Флаг показа дисклеймера персистентен: переживает JSON-сериализацию сессии
// и по умолчанию выключен у новых пользователей.
func TestDisclosureFlagRoundTrip(t *testing.T) {
	// Новый пользователь — дисклеймер ещё не показан.
	fresh := session.NewSessionData()
	if fresh.DostupDisclosureShown {
		t.Error("у новой сессии флаг дисклеймера должен быть false")
	}

	// Пользователь акцептовал дисклеймер — флаг должен сохраниться в JSON.
	fresh.DostupDisclosureShown = true
	raw, err := json.Marshal(fresh)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var loaded session.SessionData
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !loaded.DostupDisclosureShown {
		t.Error("флаг дисклеймера потерян при JSON round-trip — экран покажется повторно")
	}

	// Старые файлы сессий (без поля) не должны ломать загрузку.
	legacy := []byte(`{"step":"idle","profile":{"firstName":"Іван"},"draft":{}}`)
	var old session.SessionData
	if err := json.Unmarshal(legacy, &old); err != nil {
		t.Fatalf("legacy session unmarshal: %v", err)
	}
	if old.DostupDisclosureShown {
		t.Error("в legacy-сессии без поля флаг должен читаться как false")
	}
}
