package handlers

import (
	"strings"
	"testing"

	"info-bot-go/internal/moderation"
)

// Картка власнику: хто (Telegram + ID), підпис, орган, тема, канал,
// ознаки ризику і текст листа (обрізаний). Містики бути не має —
// власник має бачити все для рішення ✅/❌.
func TestBuildReviewCard(t *testing.T) {
	it := moderation.Item{
		UserID:    6919677903,
		ChatID:    6919677903,
		TGName:    "Test User",
		TGUser:    "tester",
		Signature: "Шварцнегер Арнольд Іванович",
		Channel:   moderation.ChannelDostup,
		Organ:     "Офіс Президента України",
		Title:     "Запит про військову посаду",
		Body:      "Текст листа про посаду.",
		Reasons:   []string{"військова розвідка", "військова посада"},
	}
	card := buildReviewCard(it)
	for _, want := range []string{
		"6919677903",
		"@tester",
		"Шварцнегер Арнольд Іванович",
		"Офіс Президента України",
		"військова розвідка",
		"Текст листа про посаду",
		"спільний акаунт порталу",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("у картці немає %q:\n%s", want, card)
		}
	}
}

// Довгий лист у картці обрізається (ліміт Telegram 4096), але не
// зневалідується HTML (екранування <pre>).
func TestBuildReviewCardTruncation(t *testing.T) {
	long := strings.Repeat("а", 4000)
	card := buildReviewCard(moderation.Item{Body: long, Channel: moderation.ChannelEmail})
	if len([]rune(card)) > 3800 {
		t.Errorf("картка надто довга: %d символів", len([]rune(card)))
	}
	if !strings.Contains(card, "…") {
		t.Error("обрізання не позначене «…»")
	}
	if !strings.Contains(card, "email") {
		t.Error("канал email не вказано")
	}
}

// Відмова автору: ПОВНИЙ текст листа (не обрізаний!) — право на
// самостійну подачу не має залежати від довжини листа.
func TestRejectUserTextFullBody(t *testing.T) {
	long := strings.Repeat("запит ", 600) // 4200+ символів
	it := moderation.Item{Title: "тема", Body: long}
	msg := rejectUserText(it)
	if !strings.Contains(msg, strings.TrimSuffix(strings.TrimSpace(long), "")) && len(msg) <= len(long) {
		t.Errorf("відмова має містити ПОВНИЙ текст листа (повідомлення %d, лист %d)", len(msg), len(long))
	}
	if !strings.Contains(msg, "самостійно") || !strings.Contains(msg, "dostup.org.ua") {
		t.Error("відмова без інструкції самостійної подачі")
	}
}

// Повідомлення про перевірку: без звинувачень, з поясненням і без
// слова «відхилено» (рішення ще не прийнято).
func TestHoldUserText(t *testing.T) {
	msg := holdUserText()
	for _, want := range []string{"перевірку", "воєнний час", "спільний канал"} {
		if !strings.Contains(msg, want) {
			t.Errorf("у повідомленні немає %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "не надіслано") {
		t.Error("повідомлення не має звучати як відмова — рішення ще не прийнято")
	}
}
