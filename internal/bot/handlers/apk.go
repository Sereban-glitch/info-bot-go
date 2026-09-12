package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	tb "gopkg.in/telebot.v3"
)

// apkReleaseURL — «latest»-ссылка GitHub Release на Android-приложение
// (обёртка мини-застосунку, собирається у .github/workflows/build-apk.yml).
const (
	apkReleaseURL = "https://github.com/Sereban-glitch/info-bot-go/releases/latest/download/smart-zapyt.apk"
	apkFileName   = "smart-zapyt.apk"
)

// ApkModule handles /apk — Android-застосунок (обгортка міні-застосунку).
type ApkModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewApkModule(deps *Deps) *ApkModule {
	return &ApkModule{deps: deps, bot: deps.Bot}
}

func (m *ApkModule) Name() string { return "apk" }

func (m *ApkModule) Register() {
	m.bot.Handle("/apk", safeHandler("apk", m.handleApk))
	m.bot.Handle("📲 Встановити застосунок", safeHandler("apk_btn", m.handleApk))
}

func (m *ApkModule) handleApk(c tb.Context) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(apkReleaseURL)
	if err != nil {
		log.Printf("[APK] download error: %v", err)
		return m.sendFallback(c)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[APK] release недоступний: HTTP %d", resp.StatusCode)
		return m.sendFallback(c)
	}

	_ = c.Send("📲 Завантажую застосунок...")

	doc := &tb.Document{
		File:     tb.FromReader(resp.Body),
		FileName: apkFileName,
	}
	if _, err := m.bot.Send(tb.ChatID(c.Sender().ID), doc); err != nil {
		log.Printf("[APK] send error: %v", err)
		return m.sendFallback(c)
	}

	return c.Send("✅ Файл вище — застосунок \u00abСмарт Запит\u00bb.\n\n1\ufe0f⃣ Відкрийте файл smart-zapyt.apk\n2\ufe0f⃣ Дозвольте встановлення з невідомих джерел (система сама запитає)\n3\ufe0f⃣ Відкрийте застосунок і увійдіть кодом з бота: команда /login\n\niOS не підтримується \u2014 користуйтесь міні-застосунком \U0001f6aa Прозоро.")
}

func (m *ApkModule) sendFallback(c tb.Context) error {
	return c.Send(fmt.Sprintf("📲 Android-застосунок «Смарт Запит»:\n%s\n\nСкачайте та встановіть файл .apk (дозвольте встановлення з невідомих джерел).", apkReleaseURL))
}